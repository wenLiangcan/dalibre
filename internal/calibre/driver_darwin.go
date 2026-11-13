//go:build darwin

package calibre

// macOS's SQLite implements the filesystem operations needed by Finder
// volumes, including SMB/AFP byte-range locks via fsctl. modernc's Darwin
// libc exits the process for fsctl. Use the system SQLite on macOS through
// purego, retaining native locking and the project's cgo-free builds.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const libraryDriver = "dalibre-macos-sqlite"

var native struct {
	once       sync.Once
	err        error
	open       func(string, *uintptr, int32, uintptr) int32
	close      func(uintptr) int32
	errmsg     func(uintptr) string
	prepare    func(uintptr, *byte, int32, *uintptr, *uintptr) int32
	finalize   func(uintptr) int32
	reset      func(uintptr) int32
	clear      func(uintptr) int32
	step       func(uintptr) int32
	count      func(uintptr) int32
	name       func(uintptr, int32) string
	kind       func(uintptr, int32) int32
	integer    func(uintptr, int32) int64
	real       func(uintptr, int32) float64
	text       func(uintptr, int32) unsafe.Pointer
	blob       func(uintptr, int32) unsafe.Pointer
	bytes      func(uintptr, int32) int32
	params     func(uintptr) int32
	bindNull   func(uintptr, int32) int32
	bindInt    func(uintptr, int32, int64) int32
	bindReal   func(uintptr, int32, float64) int32
	bindText   func(uintptr, int32, *byte, int32, uintptr) int32
	bindBlob   func(uintptr, int32, *byte, int32, uintptr) int32
	lastID     func(uintptr) int64
	changes    func(uintptr) int32
	busy       func(uintptr, int32) int32
	interrupt  func(uintptr)
	autocommit func(uintptr) int32
	createFunc func(uintptr, string, int32, int32, uintptr, uintptr, uintptr, uintptr, uintptr) int32
	userData   func(uintptr) uintptr
	valueType  func(uintptr) int32
	valueInt   func(uintptr) int64
	valueReal  func(uintptr) float64
	valueText  func(uintptr) unsafe.Pointer
	valueBlob  func(uintptr) unsafe.Pointer
	valueBytes func(uintptr) int32
	resultNull func(uintptr)
	resultInt  func(uintptr, int64)
	resultText func(uintptr, *byte, int32, uintptr)
	aggContext func(uintptr, int32) uintptr
}

func init() { sql.Register(libraryDriver, nativeDriver{}) }

func loadNative() error {
	native.once.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libsqlite3.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			native.err = fmt.Errorf("load macOS SQLite: %w", err)
			return
		}
		purego.RegisterLibFunc(&native.open, lib, "sqlite3_open_v2")
		purego.RegisterLibFunc(&native.close, lib, "sqlite3_close_v2")
		purego.RegisterLibFunc(&native.errmsg, lib, "sqlite3_errmsg")
		purego.RegisterLibFunc(&native.prepare, lib, "sqlite3_prepare_v2")
		purego.RegisterLibFunc(&native.finalize, lib, "sqlite3_finalize")
		purego.RegisterLibFunc(&native.reset, lib, "sqlite3_reset")
		purego.RegisterLibFunc(&native.clear, lib, "sqlite3_clear_bindings")
		purego.RegisterLibFunc(&native.step, lib, "sqlite3_step")
		purego.RegisterLibFunc(&native.count, lib, "sqlite3_column_count")
		purego.RegisterLibFunc(&native.name, lib, "sqlite3_column_name")
		purego.RegisterLibFunc(&native.kind, lib, "sqlite3_column_type")
		purego.RegisterLibFunc(&native.integer, lib, "sqlite3_column_int64")
		purego.RegisterLibFunc(&native.real, lib, "sqlite3_column_double")
		purego.RegisterLibFunc(&native.text, lib, "sqlite3_column_text")
		purego.RegisterLibFunc(&native.blob, lib, "sqlite3_column_blob")
		purego.RegisterLibFunc(&native.bytes, lib, "sqlite3_column_bytes")
		purego.RegisterLibFunc(&native.params, lib, "sqlite3_bind_parameter_count")
		purego.RegisterLibFunc(&native.bindNull, lib, "sqlite3_bind_null")
		purego.RegisterLibFunc(&native.bindInt, lib, "sqlite3_bind_int64")
		purego.RegisterLibFunc(&native.bindReal, lib, "sqlite3_bind_double")
		purego.RegisterLibFunc(&native.bindText, lib, "sqlite3_bind_text")
		purego.RegisterLibFunc(&native.bindBlob, lib, "sqlite3_bind_blob")
		purego.RegisterLibFunc(&native.lastID, lib, "sqlite3_last_insert_rowid")
		purego.RegisterLibFunc(&native.changes, lib, "sqlite3_changes")
		purego.RegisterLibFunc(&native.busy, lib, "sqlite3_busy_timeout")
		purego.RegisterLibFunc(&native.interrupt, lib, "sqlite3_interrupt")
		purego.RegisterLibFunc(&native.autocommit, lib, "sqlite3_get_autocommit")
		purego.RegisterLibFunc(&native.createFunc, lib, "sqlite3_create_function_v2")
		purego.RegisterLibFunc(&native.userData, lib, "sqlite3_user_data")
		purego.RegisterLibFunc(&native.valueType, lib, "sqlite3_value_type")
		purego.RegisterLibFunc(&native.valueInt, lib, "sqlite3_value_int64")
		purego.RegisterLibFunc(&native.valueReal, lib, "sqlite3_value_double")
		purego.RegisterLibFunc(&native.valueText, lib, "sqlite3_value_text")
		purego.RegisterLibFunc(&native.valueBlob, lib, "sqlite3_value_blob")
		purego.RegisterLibFunc(&native.valueBytes, lib, "sqlite3_value_bytes")
		purego.RegisterLibFunc(&native.resultNull, lib, "sqlite3_result_null")
		purego.RegisterLibFunc(&native.resultInt, lib, "sqlite3_result_int64")
		purego.RegisterLibFunc(&native.resultText, lib, "sqlite3_result_text")
		purego.RegisterLibFunc(&native.aggContext, lib, "sqlite3_aggregate_context")
		initNativeFunctions()
	})
	return native.err
}

type nativeDriver struct{}
type nativeConn struct{ db uintptr }
type nativeStmt struct {
	conn *nativeConn
	stmt uintptr
}
type nativeRows struct {
	stmt    *nativeStmt
	own     bool
	columns []string
	ctx     context.Context
	stop    func()
}
type nativeTx struct{ conn *nativeConn }
type nativeResult struct{ id, changes int64 }

func (r nativeResult) LastInsertId() (int64, error) { return r.id, nil }
func (r nativeResult) RowsAffected() (int64, error) { return r.changes, nil }

func (nativeDriver) Open(dsn string) (driver.Conn, error) {
	if err := loadNative(); err != nil {
		return nil, err
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Del("_pragma")
	q.Del("_txlock")
	u.RawQuery = q.Encode()
	// FULLMUTEX: a pooled connection may move between goroutines/OS threads.
	c := &nativeConn{}
	if rc := native.open(u.String(), &c.db, 2|4|0x40|0x10000, 0); rc != 0 {
		err := c.error(rc)
		c.Close()
		return nil, err
	}
	if rc := native.busy(c.db, 10000); rc != 0 {
		err := c.error(rc)
		c.Close()
		return nil, err
	}
	if err := c.registerFunctions(); err != nil {
		c.Close()
		return nil, err
	}
	if _, err := c.exec("PRAGMA foreign_keys=OFF", nil); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *nativeConn) error(rc int32) error {
	return fmt.Errorf("sqlite: %s (%d)", native.errmsg(c.db), rc)
}
func (c *nativeConn) Close() error {
	if c.db == 0 {
		return nil
	}
	rc := native.close(c.db)
	if rc != 0 {
		return c.error(rc)
	}
	c.db = 0
	return nil
}
func (c *nativeConn) prepare(query string) (*nativeStmt, string, error) {
	buf := append([]byte(query), 0)
	var stmt, tail uintptr
	rc := native.prepare(c.db, &buf[0], int32(len(buf)), &stmt, &tail)
	used := int(tail - uintptr(unsafe.Pointer(&buf[0])))
	runtime.KeepAlive(buf)
	if rc != 0 {
		if stmt != 0 {
			native.finalize(stmt)
		}
		return nil, "", c.error(rc)
	}
	rest := ""
	if used > 0 && used < len(query) {
		rest = query[used:]
	}
	return &nativeStmt{conn: c, stmt: stmt}, rest, nil
}
func (c *nativeConn) Prepare(query string) (driver.Stmt, error) {
	s, rest, err := c.prepare(query)
	if err != nil {
		return nil, err
	}
	if s.stmt == 0 || strings.TrimSpace(rest) != "" {
		s.Close()
		return nil, fmt.Errorf("expected one SQL statement")
	}
	return s, nil
}
func (c *nativeConn) Begin() (driver.Tx, error) {
	if _, err := c.exec("BEGIN IMMEDIATE", nil); err != nil {
		return nil, err
	}
	return &nativeTx{c}, nil
}
func (t *nativeTx) Commit() error {
	_, err := t.conn.exec("COMMIT", nil)
	if err != nil && native.autocommit(t.conn.db) == 0 {
		// A failed COMMIT (e.g. SQLITE_BUSY) keeps the transaction and its
		// locks; database/sql won't roll back, so the pooled connection
		// would stay inside it.
		t.conn.exec("ROLLBACK", nil)
	}
	return err
}
func (t *nativeTx) Rollback() error { _, err := t.conn.exec("ROLLBACK", nil); return err }

func namedValues(args []driver.NamedValue) []driver.Value {
	values := make([]driver.Value, len(args))
	for i, a := range args {
		values[i] = a.Value
	}
	return values
}
func (c *nativeConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stop := c.watch(ctx)
	defer stop()
	result, err := c.exec(query, namedValues(args))
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return result, err
}

// Join the watcher before returning the connection to the pool so a late
// cancellation cannot interrupt the next caller's statement.
func (c *nativeConn) watch(ctx context.Context) func() {
	if ctx.Done() == nil {
		return func() {}
	}
	done, joined := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-ctx.Done():
			native.interrupt(c.db)
		case <-done:
		}
	}()
	return func() { close(done); <-joined }
}
func (c *nativeConn) exec(query string, args []driver.Value) (driver.Result, error) {
	// Schema creation contains multiple statements and triggers. Let SQLite
	// parse statement boundaries, including semicolons inside triggers.
	for strings.TrimSpace(query) != "" {
		s, rest, err := c.prepare(query)
		if err != nil {
			return nil, err
		}
		if s.stmt != 0 {
			n := min(s.NumInput(), len(args))
			if err := s.bind(args[:n]); err != nil {
				s.Close()
				return nil, err
			}
			args = args[n:]
			for {
				rc := native.step(s.stmt)
				if rc == 100 {
					continue
				}
				if rc != 101 {
					err := c.error(rc)
					s.Close()
					return nil, err
				}
				break
			}
			if err := s.Close(); err != nil {
				return nil, err
			}
		}
		query = rest
	}
	if len(args) != 0 {
		return nil, fmt.Errorf("sqlite: %d unused arguments", len(args))
	}
	return nativeResult{native.lastID(c.db), int64(native.changes(c.db))}, nil
}
func (c *nativeConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	prepared, err := c.Prepare(query)
	if err != nil {
		return nil, err
	}
	s := prepared.(*nativeStmt)
	if err := s.bind(namedValues(args)); err != nil {
		s.Close()
		return nil, err
	}
	r := s.rows(true)
	r.ctx, r.stop = ctx, c.watch(ctx)
	return r, nil
}
func (s *nativeStmt) Close() error {
	if s.stmt == 0 {
		return nil
	}
	rc := native.finalize(s.stmt)
	s.stmt = 0
	if rc != 0 {
		return s.conn.error(rc)
	}
	return nil
}
func (s *nativeStmt) NumInput() int { return int(native.params(s.stmt)) }
func (s *nativeStmt) bind(args []driver.Value) error {
	native.reset(s.stmt)
	native.clear(s.stmt)
	if len(args) != s.NumInput() {
		return fmt.Errorf("sqlite: expected %d arguments, got %d", s.NumInput(), len(args))
	}
	for i, a := range args {
		var rc int32
		n := int32(i + 1)
		switch v := a.(type) {
		case nil:
			rc = native.bindNull(s.stmt, n)
		case bool:
			var b int64
			if v {
				b = 1
			}
			rc = native.bindInt(s.stmt, n, b)
		case int64:
			rc = native.bindInt(s.stmt, n, v)
		case float64:
			rc = native.bindReal(s.stmt, n, v)
		case time.Time:
			rc = bindNativeBytes(s.stmt, n, []byte(formatTime(v)), false)
		case string:
			rc = bindNativeBytes(s.stmt, n, []byte(v), false)
		case []byte:
			if v == nil {
				rc = native.bindNull(s.stmt, n)
			} else {
				rc = bindNativeBytes(s.stmt, n, v, true)
			}
		default:
			return fmt.Errorf("sqlite: unsupported argument %T", a)
		}
		if rc != 0 {
			return s.conn.error(rc)
		}
	}
	return nil
}
func bindNativeBytes(stmt uintptr, n int32, b []byte, blob bool) int32 {
	// SQLITE_TRANSIENT makes SQLite copy the Go buffer before returning.
	size := len(b)
	if size == 0 {
		b = []byte{0}
	}
	if blob {
		return native.bindBlob(stmt, n, &b[0], int32(size), ^uintptr(0))
	}
	return native.bindText(stmt, n, &b[0], int32(size), ^uintptr(0))
}
func (s *nativeStmt) Exec(args []driver.Value) (driver.Result, error) {
	if err := s.bind(args); err != nil {
		return nil, err
	}
	for {
		rc := native.step(s.stmt)
		if rc == 100 {
			continue
		}
		if rc != 101 {
			err := s.conn.error(rc)
			native.reset(s.stmt)
			return nil, err
		}
		break
	}
	result := nativeResult{native.lastID(s.conn.db), int64(native.changes(s.conn.db))}
	native.reset(s.stmt)
	return result, nil
}
func (s *nativeStmt) Query(args []driver.Value) (driver.Rows, error) {
	if err := s.bind(args); err != nil {
		return nil, err
	}
	return s.rows(false), nil
}

func (s *nativeStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stop := s.conn.watch(ctx)
	defer stop()
	result, err := s.Exec(namedValues(args))
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return result, err
}

func (s *nativeStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.bind(namedValues(args)); err != nil {
		return nil, err
	}
	r := s.rows(false)
	r.ctx, r.stop = ctx, s.conn.watch(ctx)
	return r, nil
}
func (s *nativeStmt) rows(own bool) *nativeRows {
	names := make([]string, int(native.count(s.stmt)))
	for i := range names {
		names[i] = native.name(s.stmt, int32(i))
	}
	return &nativeRows{stmt: s, own: own, columns: names}
}
func (r *nativeRows) Columns() []string { return r.columns }
func (r *nativeRows) Close() error {
	if r.stop != nil {
		r.stop()
		r.stop = nil
	}
	if r.own {
		return r.stmt.Close()
	}
	native.reset(r.stmt.stmt)
	return nil
}
func copyNative(p unsafe.Pointer, n int32) []byte {
	if n == 0 {
		return []byte{}
	}
	return append([]byte(nil), unsafe.Slice((*byte)(p), int(n))...)
}
func (r *nativeRows) Next(dest []driver.Value) error {
	if r.ctx != nil && r.ctx.Err() != nil {
		return r.ctx.Err()
	}
	rc := native.step(r.stmt.stmt)
	if r.ctx != nil && r.ctx.Err() != nil {
		return r.ctx.Err()
	}
	if rc == 101 {
		return io.EOF
	}
	if rc != 100 {
		return r.stmt.conn.error(rc)
	}
	for i := range dest {
		col := int32(i)
		s := r.stmt.stmt
		switch native.kind(s, col) {
		case 1:
			dest[i] = native.integer(s, col)
		case 2:
			dest[i] = native.real(s, col)
		case 3:
			dest[i] = string(copyNative(native.text(s, col), native.bytes(s, col)))
		case 4:
			dest[i] = copyNative(native.blob(s, col), native.bytes(s, col))
		default:
			dest[i] = nil
		}
	}
	return nil
}

var scalarCallback, stepCallback, finalCallback uintptr
var nativeAggregates sync.Map // sqlite3_aggregate_context address -> *concatAgg

// Each function's user data is its index in sqlFuncs.
func nativeFunc(ctx uintptr) sqlFunc { return sqlFuncs[native.userData(ctx)] }

func initNativeFunctions() {
	scalarCallback = purego.NewCallback(func(ctx uintptr, n int32, argv unsafe.Pointer) {
		resultNative(ctx, nativeFunc(ctx).scalar(nativeArgs(n, argv)))
	})
	stepCallback = purego.NewCallback(func(ctx uintptr, n int32, argv unsafe.Pointer) {
		key := native.aggContext(ctx, 1)
		if key == 0 {
			return
		} // SQLite reports its allocation failure.
		agg, ok := nativeAggregates.Load(key)
		if !ok {
			agg = nativeFunc(ctx).aggregate()
			nativeAggregates.Store(key, agg)
		}
		agg.(*concatAgg).add(nativeArgs(n, argv))
	})
	finalCallback = purego.NewCallback(func(ctx uintptr) {
		agg, ok := nativeAggregates.LoadAndDelete(native.aggContext(ctx, 0))
		if !ok {
			native.resultNull(ctx)
			return
		}
		resultNative(ctx, agg.(*concatAgg).value())
	})
}
func resultNative(ctx uintptr, v driver.Value) {
	switch x := v.(type) {
	case nil:
		native.resultNull(ctx)
	case int64:
		native.resultInt(ctx, x)
	default:
		resultNativeText(ctx, asString(x))
	}
}
func resultNativeText(ctx uintptr, s string) {
	b := append([]byte(s), 0)
	native.resultText(ctx, &b[0], int32(len(s)), ^uintptr(0))
}
func nativeArgs(n int32, argv unsafe.Pointer) []driver.Value {
	args := make([]driver.Value, int(n))
	for i, p := range unsafe.Slice((*uintptr)(argv), int(n)) {
		switch native.valueType(p) {
		case 1:
			args[i] = native.valueInt(p)
		case 2:
			args[i] = native.valueReal(p)
		case 3:
			args[i] = string(copyNative(native.valueText(p), native.valueBytes(p)))
		case 4:
			args[i] = copyNative(native.valueBlob(p), native.valueBytes(p))
		}
	}
	return args
}
func (c *nativeConn) registerFunctions() error {
	for i, f := range sqlFuncs {
		flags := int32(1) // SQLITE_UTF8
		if f.deterministic {
			flags |= 0x800 // SQLITE_DETERMINISTIC
		}
		scalar, step, final := scalarCallback, uintptr(0), uintptr(0)
		if f.scalar == nil {
			scalar, step, final = 0, stepCallback, finalCallback
		}
		if rc := native.createFunc(c.db, f.name, f.nargs, flags, uintptr(i), scalar, step, final, 0); rc != 0 {
			return c.error(rc)
		}
	}
	return nil
}
