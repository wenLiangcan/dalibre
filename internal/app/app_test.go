package app

import (
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"dalibre/cmd/dalibre/locales"
	"dalibre/internal/app/theme"
	"dalibre/internal/calibre"
	"dalibre/internal/catalog"
)

func TestMain(m *testing.M) {
	if err := theme.RegisterFonts(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

type sample struct {
	title, author string
	tags          []string
	rating        int
}

var samples = []sample{
	{"The Three-Body Problem", "Liu Cixin", []string{"Science Fiction"}, 8},
	{"The Dark Forest", "Liu Cixin", []string{"Science Fiction"}, 10},
	{"Dune", "Frank Herbert", []string{"Classics", "Science Fiction"}, 10},
	{"Piranesi", "Susanna Clarke", []string{"Fantasy"}, 8},
	{"红楼梦", "曹雪芹", []string{"Classics"}, 10},
	{"Invisible Cities", "Italo Calvino", []string{"Literary"}, 6},
}

// newTestApp opens a library of sample books, running background work
// synchronously so the view sees its results at once.
func newTestApp(t *testing.T) *App {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Library")
	lib, err := calibre.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range samples {
		f := filepath.Join(t.TempDir(), "book.epub")
		os.WriteFile(f, []byte("x"), 0o644)
		if _, err := lib.AddBook(calibre.NewBook{File: f, Metadata: calibre.Metadata{
			Title: s.title, Authors: []string{s.author}, Tags: s.tags, Rating: s.rating,
			Pubdate: time.Date(2000+i, 1, 1, 0, 0, 0, 0, time.UTC), Languages: []string{"eng"},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	lib.Close()
	a := New("")
	a.run = func(fn func()) { fn() }
	a.trash = nil
	a.covers.thumbs = nil
	a.openLibrary(root)
	t.Cleanup(func() {
		if a.lib != nil {
			a.lib.Close()
		}
	})
	if a.lib == nil {
		t.Fatalf("library not open: %s", msgText(a.loadErr))
	}
	return a
}

// shot saves the tester's frame as a PNG when DALIBRE_SHOTS names a folder.
func shot(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("DALIBRE_SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, tt.Image())
}

func TestLibraryShowsBooks(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	for _, s := range samples {
		if !tt.HasText(s.title) {
			t.Errorf("grid misses %q; texts %q", s.title, tt.Texts())
		}
	}
	if !tt.HasText("All books") || !tt.HasText("Add books") {
		t.Error("toolbar missing")
	}
	shot(t, tt, "grid-light")

	tt.SetDark(true)
	tt.Frame()
	shot(t, tt, "grid-dark")
}

func TestSelectShowsDetails(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if err := tt.Click("Dune"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if b := a.current(); b == nil || b.Title != "Dune" {
		t.Fatalf("current = %v", b)
	}
	if !tt.HasText("Frank Herbert") || !tt.HasText("DETAILS") {
		t.Errorf("detail panel: %q", tt.Texts())
	}
	shot(t, tt, "detail")
}

func TestSearchAndFilter(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if err := tt.Click("Search library"); err != nil {
		t.Fatal(err)
	}
	tt.Type("liu")
	tt.Frame()
	if len(a.visible()) != 2 || tt.HasText("Piranesi") {
		t.Errorf("search shows %d books", len(a.visible()))
	}
	if !tt.HasText("“liu”") || !tt.HasText("Clear all") {
		t.Error("search chip missing")
	}
	a.query.Search = ""
	tt.Frame()
	// The sidebar's format filter.
	if err := tt.Click("EPUB"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.query.Filter.Value != "EPUB" || len(a.visible()) != len(samples) {
		t.Errorf("format filter %+v shows %d", a.query.Filter, len(a.visible()))
	}
	// The author chip's picker.
	if err := tt.Click("Author"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	shot(t, tt, "picker")
	if err := tt.Click("Liu Cixin, 2 books"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.query.Filter != (catalog.Filter{Kind: catalog.ByAuthor, Value: "Liu Cixin"}) || len(a.visible()) != 2 {
		t.Errorf("author filter %+v shows %d", a.query.Filter, len(a.visible()))
	}
	if !tt.HasText("Author: Liu Cixin") {
		t.Errorf("active chip missing: %q", tt.Texts())
	}
	shot(t, tt, "filters")
}

func TestListView(t *testing.T) {
	a := newTestApp(t)
	a.settings.ListView = true
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if !tt.HasText("Author") || !tt.HasText("Rating") || !tt.HasText("Liu Cixin") {
		t.Errorf("table: %q", tt.Texts())
	}
	if err := tt.Click("Dune"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	shot(t, tt, "list")
}

func TestEditMetadata(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	tt.Click("Piranesi")
	tt.Frame()
	a.openEditor(a.current())
	tt.Frame()
	if !tt.HasText("Edit metadata") {
		t.Fatalf("editor not open: %q", tt.Texts())
	}
	shot(t, tt, "edit")
	a.edit.title = "Piranesi (Revised)"
	a.edit.tags = append(a.edit.tags, "Favourite")
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	b := a.byID[a.edit.id]
	if b == nil || b.Title != "Piranesi (Revised)" || len(b.Tags) != 2 {
		t.Fatalf("saved book = %+v", b)
	}
	if _, err := os.Stat(a.lib.Dir(b)); err != nil {
		t.Errorf("book folder not moved: %v", err)
	}
}

func TestRemoveAsksFirst(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if err := tt.RightClick("Invisible Cities"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Remove from library"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.deleteOpen {
		t.Fatalf("no confirmation: %q", tt.Texts())
	}
	shot(t, tt, "delete")
	if err := tt.Click("Remove"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.books) != len(samples)-1 {
		t.Errorf("books = %d", len(a.books))
	}
}

func TestWelcomeWithoutLibrary(t *testing.T) {
	a := New("")
	a.run = func(fn func()) { fn() }
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	for _, s := range []string{"Open a library…", "Create a new library", "A calm library\nfor your books."} {
		if !tt.HasText(s) {
			t.Errorf("welcome lacks %q: %q", s, tt.Texts())
		}
	}
	shot(t, tt, "welcome")
}

func TestWelcomeCreateLibrary(t *testing.T) {
	a := New("")
	a.run = func(fn func()) { fn() }
	t.Cleanup(func() {
		if a.lib != nil {
			a.lib.Close()
		}
	})
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if err := tt.Click("Create a new library"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	parent := t.TempDir()
	a.welcome.create.parent = parent
	a.welcome.create.name = "Shelf"
	tt.Frame()
	if !tt.HasText(abbreviateHome(filepath.Join(parent, "Shelf"))) {
		t.Errorf("no path preview: %q", tt.Texts())
	}
	shot(t, tt, "welcome-create")
	if err := tt.Click("Create library"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.lib == nil || a.lib.Root != filepath.Join(parent, "Shelf") {
		t.Fatalf("library not created and opened: %v %s", a.lib, msgText(a.loadErr))
	}
}

func TestWelcomeFolderNotLibrary(t *testing.T) {
	a := New("")
	a.run = func(fn func()) { fn() }
	t.Cleanup(func() {
		if a.lib != nil {
			a.lib.Close()
		}
	})
	dir := filepath.Join(t.TempDir(), "Downloads")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "a.epub"), []byte("x"), 0o644)
	a.openLibrary(dir)
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if !tt.HasText("“Downloads” isn’t a calibre library") || !tt.HasText("This folder isn’t empty") {
		t.Fatalf("no sheet: %q", tt.Texts())
	}
	shot(t, tt, "welcome-notlibrary")
	if err := tt.Click("Create library here"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.lib == nil || a.lib.Root != dir {
		t.Fatalf("library not created: %s", msgText(a.loadErr))
	}
}

func TestWelcomeMissingLibrary(t *testing.T) {
	a := New("")
	a.run = func(fn func()) { fn() }
	gone := filepath.Join(t.TempDir(), "Books")
	a.settings.Recent = []string{gone}
	a.settings.Library = gone
	a.openLibrary(a.startupLibrary())
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	tt.Frame()
	if a.welcome.notLib.open {
		t.Fatal("a folder that is gone asked to make a library")
	}
	if !tt.HasText("Couldn’t open “Books”") || !tt.HasText("Not found") {
		t.Fatalf("missing library not said: %q", tt.Texts())
	}
	shot(t, tt, "welcome-missing")
	a.forgetRecent(gone)
	tt.Frame()
	if len(a.settings.Recent) != 0 || a.loadErr != nil {
		t.Errorf("recent = %v, err = %q", a.settings.Recent, msgText(a.loadErr))
	}
}

func TestSelectionFollowsBook(t *testing.T) {
	a := newTestApp(t)
	a.settings.ListView = true
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if err := tt.Click("Dune"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	// Reordering moves Dune to another row; the table and panel follow it.
	a.query.Descending = !a.query.Descending
	tt.Frame()
	if b := a.current(); b == nil || b.Title != "Dune" {
		t.Fatalf("current = %v", b)
	}
	if shown := a.visible(); a.cur < 0 || shown[a.cur].Title != "Dune" {
		t.Errorf("row %d is not Dune", a.cur)
	}
}

func TestLibrarySwitcherMenu(t *testing.T) {
	a := newTestApp(t)
	other := t.TempDir()
	a.settings.Recent = []string{a.lib.Root, other}
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if err := tt.Click("Library"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, r := range a.settings.Recent {
		if !tt.HasText(r) {
			t.Errorf("menu lacks %s: %q", r, tt.Texts())
		}
	}
	// Hovering the other library shows its reveal button.
	r, ok := tt.Find(other)
	if !ok {
		t.Fatal("no row for", other)
	}
	tt.Move(r.X+10, r.Y+r.H/2)
	tt.Frame()
	tt.Frame()
	shot(t, tt, "switcher")
	if _, ok := tt.Find(revealLabel() + ": " + filepath.Base(other)); !ok {
		t.Error("no reveal button")
	}
}

func TestLibrarySwitcherScrollsLongHistory(t *testing.T) {
	var bottom float32
	for _, count := range []int{12, 40} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			a := newTestApp(t)
			oldest := filepath.Join(t.TempDir(), "Oldest library")
			lib, err := calibre.Create(oldest)
			if err != nil {
				t.Fatal(err)
			}
			lib.Close()
			for i := 1; i < count-1; i++ {
				a.settings.Opened(filepath.Join(t.TempDir(), "Library "+strconv.Itoa(i)))
			}
			a.settings.Recent = append(a.settings.Recent, oldest)
			tt := ui.NewTester(a.View, 1440, 900)
			tt.Frame()
			if err := tt.Click(locales.CommonLibrary()); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			first, ok := tt.Find(a.settings.Recent[0])
			if !ok {
				t.Fatal("no first library in the menu")
			}
			if err := tt.Click(oldest); err == nil {
				t.Fatal("the oldest library should start outside the scroll viewport")
			}
			tt.Scroll(first.X+10, first.Y+first.H/2, 0, 10000)
			tt.Frame()
			last, ok := tt.Find(locales.MainNewLibraryMenu())
			if !ok {
				t.Fatal("no create-library action after scrolling")
			}
			edge := last.Y + last.H
			if edge-first.Y > 360 {
				t.Fatalf("menu exceeds its compact height: first row %v, bottom %v", first.Y, edge)
			}
			if bottom == 0 {
				bottom = edge
			} else if edge != bottom {
				t.Fatalf("menu grew with the history: bottom %v, previously %v", edge, bottom)
			}
			shot(t, tt, "switcher-scrolled")
			if count == 40 {
				// Keyboard navigation must reveal items outside the viewport too.
				tt.Key(0, ui.KeyEscape)
				if a.libraryMenuOpen {
					t.Fatal("Escape left the menu open")
				}
				if err := tt.Click(locales.CommonLibrary()); err != nil {
					t.Fatal(err)
				}
				tt.Key(0, ui.KeyEnd)
				tt.Key(0, ui.KeyUp)
				tt.Key(0, ui.KeyUp)
				tt.Key(0, ui.KeyEnter)
			} else if err := tt.Click(oldest); err != nil {
				t.Fatalf("oldest library is not reachable by scrolling: %v", err)
			}
			tt.Frame()
			if a.lib.Root != oldest || a.libraryMenuOpen || a.settings.Recent[0] != oldest || len(a.settings.Recent) != count {
				t.Fatalf("switching to the oldest library failed: root %q, menu open %v, history %v", a.lib.Root, a.libraryMenuOpen, a.settings.Recent)
			}
		})
	}
}

func TestShareMenu(t *testing.T) {
	if !shareSupported {
		t.Skip("no share sheet on this platform")
	}
	a := newTestApp(t)
	var shared []string
	saved := shareFiles
	t.Cleanup(func() { shareFiles = saved })
	shareFiles = func(_ *mygo.Window, paths []string) { shared = paths }
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	// One format: a plain item.
	if err := tt.RightClick("Dune"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Share…"); err != nil {
		t.Fatal(err)
	}
	if len(shared) != 1 || !strings.HasSuffix(shared[0], ".epub") {
		t.Fatalf("shared = %q", shared)
	}
	// More formats: a submenu of them.
	var dune *calibre.Book
	for _, b := range a.books {
		if b.Title == "Dune" {
			dune = b
		}
	}
	f := filepath.Join(t.TempDir(), "dune.pdf")
	os.WriteFile(f, []byte("x"), 0o644)
	if err := a.lib.AddFormat(dune.ID, f); err != nil {
		t.Fatal(err)
	}
	dune.Formats = append(dune.Formats, "PDF")
	tt.Frame()
	if err := tt.RightClick("Dune"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Share", "PDF"); err != nil {
		t.Fatal(err)
	}
	if len(shared) != 1 || !strings.HasSuffix(shared[0], ".pdf") {
		t.Fatalf("shared = %q", shared)
	}
}

func TestDetailPanelComesWithSelection(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if tt.HasText("Select a book to see its details") || a.detailShown() {
		t.Fatal("an empty detail panel shows with nothing selected")
	}
	if err := tt.Click("Piranesi"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.detailShown() || !tt.HasText("Susanna Clarke") {
		t.Fatalf("detail panel: %q", tt.Texts())
	}
	// The toolbar ends where the panel begins.
	bar, _ := tt.Find("Add books")
	panel, _ := tt.Find("More")
	if bar.X+bar.W > panel.X {
		t.Fatalf("the panel covers the toolbar: %v, %v", bar, panel)
	}
	shot(t, tt, "detail-shown")
	a.chosen.Clear()
	a.curID, a.cur = 0, -1
	tt.Frame()
	if a.detailShown() || tt.HasText("DETAILS") {
		t.Fatal("the panel stayed")
	}
	a.settings.HideDetail = true
	if a.detailShown() {
		t.Fatal("shown while turned off")
	}
}

// A small window, with a book's details open, keeps the whole toolbar
// left of the detail panel.
func TestNarrowWindowToolbarFits(t *testing.T) {
	for _, w := range []int{1040, 1170, 1440} {
		a := newTestApp(t)
		tt := ui.NewTester(a.View, w, 800)
		tt.Frame()
		if err := tt.Click("Dune"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Frame()
		r, ok := tt.Find("Add books")
		if !ok {
			t.Fatalf("width %d: no Add books button", w)
		}
		if edge := float32(w - theme.DetailWidth); r.X+r.W > edge {
			t.Errorf("width %d: Add books ends at %.0f, under the detail panel at %.0f", w, r.X+r.W, edge)
		}
		shot(t, tt, "narrow-"+strconv.Itoa(w))
	}
}

// TestDialogClearsCaptionButtons checks a dialog in a short window stays
// below the window's caption buttons, as Windows draws them at the top
// right, over where the Settings close button would grow to.
func TestDialogClearsCaptionButtons(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.View, 900, 560)
	bar := ui.TitleBar{Height: 48, Right: 138}
	tt.SetTitleBar(bar)
	a.openSettings(sectionGeneral)
	tt.Frame()
	shot(t, tt, "settings-short")
	r, ok := tt.Find("Close")
	if !ok {
		t.Fatalf("no close button; texts %q", tt.Texts())
	}
	if r.Y < bar.Height {
		t.Errorf("close button at y=%v, under the caption buttons (to y=%v)", r.Y, bar.Height)
	}
}

// The detail panel's Open: one file opens straight away; several open
// from a menu, the editor's formats first.
func TestDetailOpen(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if err := tt.Click("Dune"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	dune := a.current()
	sync := a.run
	opened := 0
	a.run = func(func()) { opened++ } // opening reaches for the file; don't
	if err := tt.Click("Open"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if opened != 1 || tt.Menu() != nil {
		t.Fatalf("one file: opened %d, menu %q", opened, tt.Menu())
	}
	shot(t, tt, "detail-open")

	a.run = sync
	f := filepath.Join(t.TempDir(), "dune.pdf")
	os.WriteFile(f, []byte("pdf"), 0o644)
	if err := a.lib.AddFormat(dune.ID, f); err != nil {
		t.Fatal(err)
	}
	dune.Formats = []string{"PDF", "EPUB"}
	delete(a.details, dune.ID)
	delete(a.detailsReq, dune.ID)
	tt.Frame()
	if err := tt.Click("Open"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := strings.Join(tt.Menu(), "|"); got != "EPUB|-|PDF" {
		t.Fatalf("menu %q", got)
	}
	shot(t, tt, "detail-open-menu")
	a.run = func(func()) { opened++ }
	if err := tt.ChooseMenuItem("PDF"); err != nil {
		t.Fatal(err)
	}
	if opened != 2 {
		t.Errorf("opened %d", opened)
	}
}

func TestOpenOrder(t *testing.T) {
	got := openOrder([]string{"PDF", "AZW3", "MOBI", "EPUB", "TXT"})
	if strings.Join(got, " ") != "EPUB TXT AZW3 PDF MOBI" {
		t.Errorf("order %q", got)
	}
}
