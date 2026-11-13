package calibre

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLibraryVolumePaths(t *testing.T) {
	names := []string{"External Disk", "Books #1 & 100% 日本語"}
	if runtime.GOOS != "windows" { // Windows filenames can't hold '?'
		names = append(names, "Books #1? & 100%")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), name)
			lib, err := Create(root)
			if err != nil {
				t.Fatal(err)
			}
			id := lib.UUID()
			lib.Close()
			for _, open := range []func(string) (*Library, error){Open, OpenReadOnly} {
				lib, err := open(root)
				if err != nil {
					t.Fatal(err)
				}
				if got := lib.UUID(); got != id || got == "" {
					t.Errorf("opened wrong database: UUID = %q, want %q", got, id)
				}
				lib.Close()
			}
		})
	}
}

// This exercises the actual mounted filesystem's locking without changing
// the user's library. The default Darwin VFS used to exit in libc.Xfsctl.
func TestMountedLibrary(t *testing.T) {
	root := os.Getenv("DALIBRE_REAL_LIBRARY")
	if root == "" {
		t.Skip("DALIBRE_REAL_LIBRARY not set")
	}
	lib, err := OpenReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	books, err := lib.Books()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Identifiers(); err != nil {
		t.Fatal(err)
	}
	for _, b := range books {
		if _, err := lib.Details(b); err != nil {
			t.Fatalf("book %d: %v", b.ID, err)
		}
	}
	t.Logf("read %d books and their details from mounted library", len(books))
}

// DALIBRE_TEST_VOLUME names a writable mounted folder. All writes stay in
// a new temporary library, never the user's existing library.
func TestMountedVolumeWrites(t *testing.T) {
	parent := os.Getenv("DALIBRE_TEST_VOLUME")
	if parent == "" {
		t.Skip("DALIBRE_TEST_VOLUME not set")
	}
	root, err := os.MkdirTemp(parent, "dalibre-volume-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	lib, err := Create(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	id, err := lib.AddBook(NewBook{File: writeTemp(t, "book.epub", "test book"), Metadata: Metadata{Title: "Volume test", Authors: []string{"Author"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.UpdateMetadata(id, Metadata{Title: "Renamed on volume", Authors: []string{"Another author"}}); err != nil {
		t.Fatal(err)
	}
	book, err := lib.Book(id)
	if err != nil || book.Title != "Renamed on volume" {
		t.Fatalf("updated book: %v, %v", book, err)
	}
	details, err := lib.Details(book)
	if err != nil || len(details.Files) != 1 {
		t.Fatalf("book files: %v, %v", details, err)
	}
	if _, err := os.Stat(details.Files[0].Path); err != nil {
		t.Fatal(err)
	}

	other, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.db.Exec(`PRAGMA busy_timeout=50`); err != nil {
		t.Fatal(err)
	}
	tx, err := lib.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if contender, err := other.db.Begin(); err == nil {
		contender.Rollback()
		t.Fatal("second writer acquired a transaction while the first holds its lock")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	tx2, err := other.db.Begin()
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	if err := tx2.Rollback(); err != nil {
		t.Fatal(err)
	}
	t.Log("create, import, rename, read, and competing writer locks succeeded")
}
