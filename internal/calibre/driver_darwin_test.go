//go:build darwin

package calibre

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNativeStatementsAndValues(t *testing.T) {
	lib := newTestLibrary(t)
	stmt, err := lib.db.Prepare(`SELECT ?, ?, ?, ?, ?, ?, ?`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	for range 2 {
		var text string
		var blob, empty []byte
		var null any
		var integer int64
		var real float64
		var boolean bool
		err := stmt.QueryRow("日本語\x00#?%", []byte{0, 1, 255}, []byte{}, nil, int64(1<<50), 1.25, true).Scan(&text, &blob, &empty, &null, &integer, &real, &boolean)
		if err != nil {
			t.Fatal(err)
		}
		if text != "日本語\x00#?%" || !bytes.Equal(blob, []byte{0, 1, 255}) || empty == nil || len(empty) != 0 || null != nil || integer != 1<<50 || real != 1.25 || !boolean {
			t.Fatalf("incorrect values: %q %v %v %v %d %g %v", text, blob, empty, null, integer, real, boolean)
		}
	}
	// A failed statement must leave the connection usable.
	if _, err := lib.db.Exec(`SELECT * FROM no_such_table`); err == nil {
		t.Fatal("invalid SQL succeeded")
	}
	if lib.UUID() == "" {
		t.Fatal("connection unusable after error")
	}
}

func TestNativeAggregates(t *testing.T) {
	lib := newTestLibrary(t)
	for _, tc := range []struct{ fn, expr, want string }{
		{"concat", "v", "b,a"},
		{"sortconcat", "k,v", "a,b"},
		{"sortconcat_bar", "k,v", "a|b"},
		{"sortconcat_amper", "k,v", "a&b"},
		{"identifiers_concat", "k,v", "2:b,1:a"},
	} {
		var got string
		err := lib.db.QueryRow(`WITH input(k,v) AS (VALUES (2,'b'),(1,'a')) SELECT ` + tc.fn + `(` + tc.expr + `) FROM input`).Scan(&got)
		if err != nil || got != tc.want {
			t.Errorf("%s = %q, %v", tc.fn, got, err)
		}
	}
	var empty any
	if err := lib.db.QueryRow(`SELECT concat(title) FROM books`).Scan(&empty); err != nil || empty != nil {
		t.Fatalf("empty aggregate: %v, %v", empty, err)
	}
	nativeAggregates.Range(func(key, value any) bool { t.Errorf("leaked aggregate %v", key); return true })
}

func TestNativeCancellation(t *testing.T) {
	lib := newTestLibrary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var result int64
	err := lib.db.QueryRowContext(ctx, `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT sum(x) FROM n`).Scan(&result)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
	if lib.UUID() == "" {
		t.Fatal("cancellation interrupted the next operation")
	}
	// The prepared-statement path must interrupt a running query too.
	stmt, err := lib.db.Prepare(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?) SELECT sum(x) FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	if err := stmt.QueryRowContext(ctx2, 1000000000).Scan(&result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("prepared cancellation: %v", err)
	}
	if err := stmt.QueryRow(3).Scan(&result); err != nil || result != 6 {
		t.Fatalf("statement unusable after cancellation: %d, %v", result, err)
	}
}

func TestNativeMultiStatementArgs(t *testing.T) {
	lib := newTestLibrary(t)
	if _, err := lib.db.Exec(`CREATE TEMP TABLE t(a, b); INSERT INTO t VALUES(?, 1); INSERT INTO t VALUES(?, ?)`, "x", "y", "z"); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := lib.db.QueryRow(`SELECT group_concat(a || b, ',') FROM t`).Scan(&got); err != nil || got != "x1,yz" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := lib.db.Exec(`SELECT 1; SELECT ?`, 1, 2); err == nil {
		t.Fatal("extra arguments accepted")
	}
}

func TestNativeReadOnly(t *testing.T) {
	original := newTestLibrary(t)
	lib, err := OpenReadOnly(original.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if _, err := lib.db.Exec(`DELETE FROM library_id`); err == nil || !strings.Contains(err.Error(), "readonly") {
		t.Fatalf("read-only write: %v", err)
	}
	if lib.UUID() == "" {
		t.Fatal("write changed read-only database")
	}
}
