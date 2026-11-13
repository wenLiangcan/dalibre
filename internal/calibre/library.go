// Package calibre reads and writes calibre libraries: a folder holding
// metadata.db and one folder per book.
package calibre

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed metadata_sqlite.sql
var schemaSQL string // calibre's resources/metadata_sqlite.sql (GPL-3.0)

// SchemaVersion is the user_version of the schema Create writes.
const SchemaVersion = 28

// Library is an open calibre library.
type Library struct {
	Root string
	db   *sql.DB
}

// ErrNotLibrary means the folder has no metadata.db.
var ErrNotLibrary = errors.New("not a calibre library (no metadata.db)")

func dsn(path string) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "foreign_keys(0)")
	q.Set("_txlock", "immediate")
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), OmitHost: true, RawQuery: q.Encode()}).String()
}

// Open opens the library in root.
func Open(root string) (*Library, error) { return open(root, false) }

// OpenReadOnly opens the library in root so that nothing can change it;
// every write fails.
func OpenReadOnly(root string) (*Library, error) { return open(root, true) }

func open(root string, readOnly bool) (*Library, error) {
	dbPath := filepath.Join(root, "metadata.db")
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotLibrary
		}
		return nil, fmt.Errorf("access library database: %w", err)
	}
	d := dsn(dbPath)
	if readOnly {
		d += "&mode=ro"
	} else {
		// Do not create an empty database if a volume disappears after Stat.
		d += "&mode=rw"
	}
	db, err := sql.Open(libraryDriver, d)
	if err != nil {
		return nil, err
	}
	// One connection: calibre's triggers rely on sqlFuncs, and
	// a single writer avoids SQLITE_BUSY between our own goroutines.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Library{Root: root, db: db}, nil
}

// Create makes an empty library in root, which must not hold one already.
func Create(root string) (*Library, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(root, "metadata.db")
	if _, err := os.Stat(dbPath); err == nil {
		return nil, fmt.Errorf("%s already exists", dbPath)
	}
	db, err := sql.Open(libraryDriver, dsn(dbPath))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		os.Remove(dbPath)
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if _, err := db.Exec(`INSERT INTO library_id(uuid) VALUES(?)`, newUUID()); err != nil {
		db.Close()
		return nil, err
	}
	return &Library{Root: root, db: db}, nil
}

// Close closes the database.
func (l *Library) Close() error { return l.db.Close() }

// UUID is the library's id, as calibre made it; "" when it has none.
func (l *Library) UUID() string {
	var id string
	l.db.QueryRow(`SELECT uuid FROM library_id LIMIT 1`).Scan(&id)
	return id
}

// UserVersion returns the schema version calibre recorded.
func (l *Library) UserVersion() (int, error) {
	var v int
	err := l.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	return v, err
}

// Book is a book with the fields the library views show.
type Book struct {
	ID           int64
	UUID         string
	Title        string
	Sort         string
	Authors      []string
	AuthorSort   string
	Series       string
	SeriesIndex  float64
	Rating       int // 0–10, calibre stores half stars as odd numbers
	Tags         []string
	Publisher    string
	Languages    []string
	Formats      []string // upper case, as "EPUB"
	Size         int64    // largest format's size in bytes
	Timestamp    time.Time
	Pubdate      time.Time // zero when undefined
	LastModified time.Time
	HasCover     bool
	Path         string // relative to the library root, with forward slashes
}

// Dir is the book's folder on disk.
func (l *Library) Dir(b *Book) string { return filepath.Join(l.Root, filepath.FromSlash(b.Path)) }

// CoverPath is where the book's cover.jpg is, whether it exists or not.
func (l *Library) CoverPath(b *Book) string { return filepath.Join(l.Dir(b), "cover.jpg") }

// FormatFile is one file of a book.
type FormatFile struct {
	Format string
	Name   string // base name without extension
	Size   int64
	Path   string // absolute
}

// Details holds what only the book's own view shows.
type Details struct {
	Comments    string
	Identifiers map[string]string // "isbn" → "9780765382030"
	Files       []FormatFile
}

var timeLayouts = []string{
	"2006-01-02 15:04:05.999999-07:00",
	"2006-01-02T15:04:05.999999-07:00",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02T15:04:05-07:00",
	"2006-01-02 15:04:05.999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			if t.Year() <= 101 { // calibre's UNDEFINED_DATE is 0101-01-01
				return time.Time{}
			}
			return t
		}
	}
	return time.Time{}
}

// formatTime writes a time as calibre does: ISO 8601 with a space, in UTC.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "0101-01-01 00:00:00+00:00"
	}
	return t.UTC().Format("2006-01-02 15:04:05.000000-07:00")
}

// Books loads every book of the library, with authors, tags, series and
// formats, in a handful of queries rather than one per book.
func (l *Library) Books() ([]*Book, error) { return l.loadBooks(0) }

// Book loads one book.
func (l *Library) Book(id int64) (*Book, error) {
	books, err := l.loadBooks(id)
	if err != nil {
		return nil, err
	}
	if len(books) == 0 {
		return nil, fmt.Errorf("book %d: %w", id, sql.ErrNoRows)
	}
	return books[0], nil
}

// loadBooks loads the book with the id, or every book when id is 0.
func (l *Library) loadBooks(id int64) ([]*Book, error) {
	where, linkWhere := "", ""
	var args []any
	if id != 0 {
		where, linkWhere, args = " WHERE id = ?", " WHERE l.book = ?", []any{id}
	}
	rows, err := l.db.Query(`SELECT id, IFNULL(uuid,''), title, IFNULL(sort,''), IFNULL(author_sort,''), series_index,
		CAST(timestamp AS TEXT), CAST(pubdate AS TEXT), CAST(last_modified AS TEXT), has_cover, path FROM books`+where, args...)
	if err != nil {
		return nil, err
	}
	var books []*Book
	byID := map[int64]*Book{}
	for rows.Next() {
		var b Book
		var ts, pd, lm sql.NullString
		var hasCover sql.NullBool
		if err := rows.Scan(&b.ID, &b.UUID, &b.Title, &b.Sort, &b.AuthorSort, &b.SeriesIndex, &ts, &pd, &lm, &hasCover, &b.Path); err != nil {
			rows.Close()
			return nil, err
		}
		b.Timestamp, b.Pubdate, b.LastModified = parseTime(ts.String), parseTime(pd.String), parseTime(lm.String)
		b.HasCover = hasCover.Bool
		books = append(books, &b)
		byID[b.ID] = &b
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	str := func(query string, set func(b *Book, v string)) error {
		rows, err := l.db.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var v string
			if err := rows.Scan(&id, &v); err != nil {
				return err
			}
			if b := byID[id]; b != nil {
				set(b, v)
			}
		}
		return rows.Err()
	}
	w := linkWhere
	steps := []struct {
		query string
		set   func(b *Book, v string)
	}{
		{`SELECT l.book, a.name FROM books_authors_link l JOIN authors a ON a.id = l.author` + w + ` ORDER BY l.id`,
			func(b *Book, v string) { b.Authors = append(b.Authors, AuthorName(v)) }},
		{`SELECT l.book, t.name FROM books_tags_link l JOIN tags t ON t.id = l.tag` + w + ` ORDER BY t.name`,
			func(b *Book, v string) { b.Tags = append(b.Tags, v) }},
		{`SELECT l.book, s.name FROM books_series_link l JOIN series s ON s.id = l.series` + w,
			func(b *Book, v string) { b.Series = v }},
		{`SELECT l.book, p.name FROM books_publishers_link l JOIN publishers p ON p.id = l.publisher` + w,
			func(b *Book, v string) { b.Publisher = v }},
		{`SELECT l.book, g.lang_code FROM books_languages_link l JOIN languages g ON g.id = l.lang_code` + w + ` ORDER BY l.item_order`,
			func(b *Book, v string) { b.Languages = append(b.Languages, v) }},
		{`SELECT l.book, CAST(r.rating AS TEXT) FROM books_ratings_link l JOIN ratings r ON r.id = l.rating` + w,
			func(b *Book, v string) { fmt.Sscan(v, &b.Rating) }},
		{`SELECT l.book, upper(l.format) || ':' || l.uncompressed_size FROM data l` + w + ` ORDER BY l.format`,
			func(b *Book, v string) {
				f, size, _ := strings.Cut(v, ":")
				b.Formats = append(b.Formats, f)
				var n int64
				fmt.Sscan(size, &n)
				if n > b.Size {
					b.Size = n
				}
			}},
	}
	for _, s := range steps {
		if err := str(s.query, s.set); err != nil {
			return nil, err
		}
	}
	return books, nil
}

// Identifiers maps every identifier in the library, as "type:value" in
// lower case ("isbn:9780765382030"), to its book.
func (l *Library) Identifiers() (map[string]int64, error) {
	rows, err := l.db.Query(`SELECT book, lower(type), lower(val) FROM identifiers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id int64
		var t, v string
		if err := rows.Scan(&id, &t, &v); err != nil {
			return nil, err
		}
		out[t+":"+v] = id
	}
	return out, rows.Err()
}

// Details loads the comments, identifiers and files of a book.
func (l *Library) Details(b *Book) (*Details, error) {
	d := &Details{Identifiers: map[string]string{}}
	err := l.db.QueryRow(`SELECT text FROM comments WHERE book = ?`, b.ID).Scan(&d.Comments)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	rows, err := l.db.Query(`SELECT type, val FROM identifiers WHERE book = ? ORDER BY type`, b.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t, v string
		if err := rows.Scan(&t, &v); err != nil {
			rows.Close()
			return nil, err
		}
		d.Identifiers[t] = v
	}
	rows.Close()
	rows, err = l.db.Query(`SELECT upper(format), name, uncompressed_size FROM data WHERE book = ? ORDER BY format`, b.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var f FormatFile
		if err := rows.Scan(&f.Format, &f.Name, &f.Size); err != nil {
			return nil, err
		}
		f.Path = filepath.Join(l.Dir(b), f.Name+"."+strings.ToLower(f.Format))
		d.Files = append(d.Files, f)
	}
	return d, rows.Err()
}
