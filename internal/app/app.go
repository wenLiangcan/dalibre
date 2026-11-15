package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"dalibre/cmd/dalibre/locales"
	"dalibre/internal/app/icons"
	"dalibre/internal/calibre"
	"dalibre/internal/catalog"
	"dalibre/internal/plugin"
	"dalibre/internal/settings"
)

// App is the state the window shows. Every widgets.Field is read and written on
// the main thread; work on disk runs in goroutines that hand their results
// back through post.
type App struct {
	win  *mygo.Window
	post func(func()) // runs fn on the main thread and rebuilds
	run  func(func()) // runs fn in the background
	// trash takes removed books' folders; nil keeps them in the library's
	// trash folder, as tests do.
	trash func(path string) error
	// open opens a library; tests open real libraries read-only.
	open func(root string) (*calibre.Library, error)

	settings *settings.Settings

	lib     *calibre.Library
	loading bool
	// loadingRoot is the library being opened; loadGen tells an open the
	// user cancelled from the current one.
	loadingRoot string
	loadGen     int
	// loadErr says why the last library didn't open, loadErrDetail what to
	// do about it; the welcome screen shows them.
	loadErr, loadErrDetail func() string
	welcome                welcomeState
	imp                    importState // adding dropped or chosen files
	books                  []*calibre.Book
	byID                   map[int64]*calibre.Book
	counts                 catalog.Counts
	version                int // bumps when books change, to recompute shown
	booksTime              time.Time

	query    catalog.Query
	shown    []*calibre.Book
	shownKey shownKey
	// curID is the book the detail panel shows. cur is its row in shown,
	// which the grid and table move; curAt and curList are what cur was last
	// set to and for which rows, so a move is told from a stale index.
	curID      int64
	cur        int
	curAt      int
	curList    []*calibre.Book
	chosen     ui.Selection[int64]
	grid       ui.GridState
	gridBounds ui.Rect // where the grid stood in the last frame
	table      ui.ListState
	tableSort  ui.SortOrder
	sectionsOn map[string]bool

	libraryMenuOpen bool

	details    map[int64]*calibre.Details
	detailsReq map[int64]bool
	covers     *coverCache

	// dialogs
	editOpen   bool
	edit       editState
	fetch      fetchState // the Edit metadata dialog's Fetch metadata step
	deleteOpen bool
	deleteIDs  []int64
	jobsOpen   bool
	// sharingOpen shows the Sharing popover, agentsOpen the AI agents one.
	sharingOpen, agentsOpen bool
	// agentsCall is the call whose arguments the AI agents popover shows.
	agentsCall int64
	// agentsWake is when the AI agents button is next due to change.
	agentsWake time.Time
	// agentIcons are the agents' own icons, decoded, by agent name.
	agentIcons map[string]ui.ImageSource
	// pickerOpen is which filter's picker is open, by kind; pickerQuery
	// narrows its list.
	pickerOpen  [catalog.ByLanguage + 1]bool
	pickerQuery string

	// plugins
	pluginDir string // "" loads none, as tests do
	// watchEvery is how often linked plugins' folders are looked at for
	// changes; 0, as in tests, doesn't watch. linkGen ends the watch of
	// plugins loaded before.
	watchEvery time.Duration
	linkGen    atomic.Int64
	plugins    []*plugin.Plugin
	// pluginSecrets are the loaded plugins' secret settings, as kept in
	// the credential store, by plugin id and key.
	pluginSecrets map[string]map[string]string
	pluginErrs    []error
	// pluginsPending are installed plugins waiting for the user to grant
	// their permissions; consent is the one the user is being asked about.
	pluginsPending []*plugin.Manifest
	pluginsKnown   []knownPlugin
	consent        *consentAsk
	downloads      []*download
	pluginsLoading bool
	// loadSeq counts the plugins' loads; loadWait is closed when the last
	// one started is done (afterPluginLoad).
	loadSeq  int
	loadWait []chan struct{}
	pl       pluginsState
	// plTab is the Plugins window's tab; mk the Marketplace's.
	plTab       int
	pluginsOpen bool // the Plugins window
	mk          marketState
	// marketURL is the Marketplace's index; plugin.MarketplaceURL when "".
	marketURL string
	// updates are newer versions of plugins installed from GitHub, by id.
	updates         map[string]*pluginUpdate
	updatesChecking bool
	github          *plugin.GitHub // nil is github.com
	ghi             ghInstall      // the Install from GitHub sheet
	tl              toolsState     // the plugins' tools
	// ghMD renders the README in Install from GitHub; ghMDFor is the
	// repository and version its links point at.
	ghMD    markdown.Options
	ghMDFor string
	// au is updating Dalibre itself.
	au appUpdateState
	// feedbackOpen is whether the sidebar's Feedback menu shows.
	feedbackOpen bool

	// settingsOpen shows the Settings window, on section.
	settingsOpen bool
	section      settingsSection
	catEdit      catalogEdit  // the Add or Edit catalog sheet
	shareEdit    shareEdit    // Settings › OPDS's sharing fields
	mcpEdit      mcpEdit      // Settings › MCP's port field
	wirelessEdit wirelessEdit // Settings › Wireless devices' fields
	mcpTab       int          // the agent Connect an agent shows
	mcpTicking   bool         // a rebuild is due to keep MCP's status current

	// OPDS: the catalogs' sources, what checking each found (by catalog
	// id), the catalog page, and the server sharing the library.
	catalogs      []*plugin.Plugin
	catalogStatus map[string]*catalogStatus
	cp            catalogPage
	share         *shareState
	libShared     atomic.Pointer[calibre.Library]
	// mcpSrv is the server serving the library to the user's agents.
	mcpSrv *mcpState
	// device is the wireless connection readers connect to.
	device *deviceState
	// secrets keeps passwords: the system's credential store; nil in tests
	// until they set one.
	secrets secretStore

	// act is the plugins' actions: the one running, what it asks and found.
	act actionState

	// page is what the main column shows: the library or Get books.
	page page
	gb   getBooksState
	// idIndex maps the library's identifiers, "type:value", to books.
	idIndex map[string]int64
	// builtins loads the plugins that ship inside Dalibre; tests leave
	// it off and set plugins themselves.
	builtins bool
	// pluginTransport makes plugins' requests in tests; nil is the network.
	pluginTransport http.RoundTripper

	busy      string // a long action in progress, as an import
	busyDone  int
	busyTotal int
	toasts    []toastMsg
	// editors are the book editor windows open, by book; editorToasts
	// the messages waiting for theirs.
	editors      map[int64]*bookEditor
	editorToasts map[int64][]toastMsg
	searchFocus  ui.Handle
	mainBox      ui.Handle // the middle column, measured to fit the toolbar
	gridBox      ui.Handle // the Marketplace's grid, measured to fit its cards
}

type toastMsg struct {
	text     string
	error    bool
	action   string // a button, as Undo, which runs onAction
	onAction func()
	onClose  func()
}

type shownKey struct {
	q       catalog.Query
	version int
}

// New returns the app with the settings at settingsPath ("" keeps them
// in memory, as tests do).
func New(settingsPath string) *App {
	a := &App{
		cur:        -1,
		curAt:      -1,
		byID:       map[int64]*calibre.Book{},
		details:    map[int64]*calibre.Details{},
		detailsReq: map[int64]bool{},
		sectionsOn: map[string]bool{"formats": true},
		covers:     newCoverCache(),
		settings:   settings.Load(settingsPath),
		gb:         newGetBooks(),
	}
	a.post = func(fn func()) { fn() }
	a.run = func(fn func()) { go fn() }
	a.trash = mygo.Shell.TrashItem
	a.au.version = mygo.App.Version()
	if !mygo.IsDev() {
		// Plugins needing a newer Dalibre are refused; a development
		// build runs them all.
		plugin.DalibreVersion = a.au.version
	}
	a.au.relaunch = func() {}
	a.open = calibre.Open
	a.query.Sort = catalog.SortKey(a.settings.Sort)
	a.query.Descending = a.settings.Descending
	return a
}

// Attach shows the app in win: work done in the background hands its
// results to the window's main thread. It opens the startup library.
func (a *App) Attach(win *mygo.Window) {
	a.win = win
	// What finishes in the background can change the book editors too, as
	// a cover loading: they draw again with the main window.
	a.post = func(fn func()) {
		win.Update(func() {
			fn()
			for _, ed := range a.editors {
				ed.win.Invalidate()
			}
		})
	}
	registerBookScheme()
	a.pluginDir = plugin.DefaultDir()
	a.watchEvery = defaultWatchEvery
	a.builtins = true
	a.secrets = systemSecrets{}
	a.loadPlugins()
	a.loadCatalogs()
	a.welcome.coversNet = true
	a.findCalibreLibrary()
	a.openLibrary(a.startupLibrary())
	a.applyShare()
	a.applyMCP()
	a.applyDevice()
	a.checkUpdates(false)
	a.au.updater = mygoUpdater{}
	a.au.relaunch = mygo.App.Relaunch
	a.watchAppUpdates()
}

// View builds the window from the state, every frame.
func (a *App) View(c *ui.Context) { a.view(c) }

func (a *App) saveSettings() {
	a.settings.Sort, a.settings.Descending = int(a.query.Sort), a.query.Descending
	_ = a.settings.Save()
}

func (a *App) toast(format string, args ...any) {
	a.toasts = append(a.toasts, toastMsg{text: fmt.Sprintf(format, args...)})
}

func (a *App) toastError(err error) {
	a.toasts = append(a.toasts, toastMsg{text: err.Error(), error: true})
}

// startupLibrary picks the library to open: DALIBRE_LIBRARY, then the
// one used last. With neither, the welcome screen offers calibre's own.
func (a *App) startupLibrary() string {
	if p := os.Getenv("DALIBRE_LIBRARY"); p != "" {
		return p
	}
	return a.settings.Library
}

// calibreLibraryPath reads the library calibre last used from its
// global.py.json, if calibre is installed.
func calibreLibraryPath() string {
	var dirs []string
	if d, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(d, "calibre"))
	}
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(h, ".config", "calibre"), filepath.Join(h, "Library", "Preferences", "calibre"))
	}
	for _, d := range dirs {
		data, err := os.ReadFile(filepath.Join(d, "global.py.json"))
		if err != nil {
			continue
		}
		var g struct {
			LibraryPath string `json:"library_path"`
		}
		if json.Unmarshal(data, &g) == nil && g.LibraryPath != "" {
			return g.LibraryPath
		}
	}
	return ""
}

// openLibrary opens root in the background and shows it. A folder that
// is gone is said on the welcome screen; one without a library asks
// whether to make one there.
func (a *App) openLibrary(root string) {
	if root == "" {
		return
	}
	a.loadGen++
	gen := a.loadGen
	a.loading, a.loadingRoot, a.loadErr, a.loadErrDetail = true, root, nil, nil
	a.run(func() {
		if _, err := os.Stat(root); err != nil {
			a.post(func() {
				if gen != a.loadGen {
					return
				}
				a.loading = false
				a.loadErr = func() string { return locales.AppCouldntOpenLibrary(filepath.Base(root)) }
				a.loadErrDetail = func() string { return locales.AppLibraryGone(abbreviateHome(root)) }
			})
			return
		}
		lib, err := a.open(root)
		var books []*calibre.Book
		var ids map[string]int64
		if err == nil {
			books, err = lib.Books()
		}
		if err == nil {
			ids, _ = lib.Identifiers()
		}
		a.post(func() {
			if gen != a.loadGen { // cancelled, or another library asked for since
				if lib != nil {
					lib.Close()
				}
				return
			}
			a.loading = false
			if err != nil {
				if lib != nil {
					lib.Close()
				}
				if errors.Is(err, calibre.ErrNotLibrary) {
					a.askNotLibrary(root)
					return
				}
				a.loadErr = func() string { return locales.AppCouldntOpenLibrary(filepath.Base(root)) }
				detail := err.Error()
				a.loadErrDetail = func() string { return detail }
				return
			}
			if a.lib != nil {
				a.lib.Close()
			}
			a.lib = lib
			a.libShared.Store(lib)
			a.idIndex = ids
			a.details, a.detailsReq = map[int64]*calibre.Details{}, map[int64]bool{}
			a.curID, a.chosen = 0, ui.Selection[int64]{}
			a.grid, a.table = ui.GridState{}, ui.ListState{}
			a.query.Filter, a.query.Search, a.query.Shelf = catalog.Filter{}, "", catalog.AllBooks
			a.act.results, a.act.review = nil, nil
			a.setBooks(books)
			a.settings.Opened(root)
			a.saveSettings()
			if a.win != nil {
				a.win.SetTitle(locales.AppWindowTitle(filepath.Base(root)))
			}
		})
	})
}

// cancelOpen stops waiting for the library being opened.
func (a *App) cancelOpen() {
	a.loadGen++
	a.loading = false
}

func (a *App) createLibrary(root string) {
	a.run(func() {
		lib, err := calibre.Create(root)
		if err == nil {
			lib.Close()
		}
		a.post(func() {
			if err != nil {
				a.toastError(err)
				return
			}
			a.openLibrary(root)
		})
	})
}

func (a *App) setBooks(books []*calibre.Book) {
	a.books = books
	a.byID = make(map[int64]*calibre.Book, len(books))
	for _, b := range books {
		a.byID[b.ID] = b
	}
	a.booksTime = time.Now()
	a.counts = catalog.Tally(books, a.booksTime)
	// Books edited while a reader is connected reach it.
	a.syncDevice()
	a.version++
}

// reload reads the books again after a change, keeping the selection.
func (a *App) reload(after func()) {
	lib := a.lib
	if lib == nil {
		return
	}
	a.run(func() {
		books, err := lib.Books()
		ids, _ := lib.Identifiers()
		a.post(func() {
			if lib != a.lib {
				return
			}
			if err != nil {
				a.toastError(err)
				return
			}
			a.details, a.detailsReq = map[int64]*calibre.Details{}, map[int64]bool{}
			a.idIndex = ids
			a.setBooks(books)
			if after != nil {
				after()
			}
		})
	})
}

// visible returns the books the query selects, recomputed only when the
// query or the books change.
func (a *App) visible() []*calibre.Book {
	if r := a.act.results; r != nil {
		// An action's results are the list, in the action's groups.
		r.build(a.byID, a.version)
		return r.shown
	}
	k := shownKey{a.query, a.version}
	if k != a.shownKey || a.shown == nil {
		a.shown = catalog.Apply(a.books, a.query, a.booksTime)
		a.shownKey = k
	}
	return a.shown
}

// current is the book the detail panel shows: the last one chosen.
func (a *App) current() *calibre.Book {
	a.adoptCur()
	return a.byID[a.curID]
}

// adoptCur takes the row the grid or table moved cur to as the current book.
func (a *App) adoptCur() {
	if a.cur == a.curAt {
		return
	}
	a.curID = 0
	if a.cur >= 0 && a.cur < len(a.curList) {
		a.curID = a.curList[a.cur].ID
	}
	a.curAt = a.cur
}

// syncCur points cur at the current book's row in shown, wherever the
// query, the order or an edit moved it.
func (a *App) syncCur(shown []*calibre.Book) {
	a.adoptCur()
	a.cur = slices.IndexFunc(shown, func(b *calibre.Book) bool { return b.ID == a.curID })
	a.curAt, a.curList = a.cur, shown
}

// selectedIDs are the books chosen, or the current one.
func (a *App) selectedIDs() []int64 {
	var ids []int64
	for _, b := range a.visible() {
		if a.chosen.Has(b.ID) {
			ids = append(ids, b.ID)
		}
	}
	if len(ids) == 0 {
		if b := a.current(); b != nil {
			ids = []int64{b.ID}
		}
	}
	return ids
}

// detailsOf returns the book's comments and files, loading them once.
func (a *App) detailsOf(b *calibre.Book) *calibre.Details {
	if d := a.details[b.ID]; d != nil {
		return d
	}
	if a.lib != nil && !a.detailsReq[b.ID] {
		a.detailsReq[b.ID] = true
		lib := a.lib
		a.run(func() {
			d, err := lib.Details(b)
			a.post(func() {
				if err == nil && lib == a.lib {
					a.details[b.ID] = d
				}
			})
		})
	}
	return nil
}

func ebookFilter() []mygo.FileFilter {
	return []mygo.FileFilter{
		{Name: locales.AppFilterEbooks(), Extensions: []string{"epub", "pdf", "mobi", "azw3", "azw", "fb2", "txt", "cbz", "djvu", "docx", "rtf"}},
		{Name: locales.AppFilterAllFiles(), Extensions: []string{"*"}},
	}
}

// chooseBooks asks for files and adds them.
func (a *App) chooseBooks() {
	a.run(func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: locales.CommonAddBooks(), Multiple: true, Filters: ebookFilter()})
		if err != nil || len(paths) == 0 {
			return
		}
		a.post(func() { a.importFiles(paths) })
	})
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// saveMetadata writes the editor's changes.
func (a *App) saveMetadata(id int64, m calibre.Metadata, cover []byte) {
	lib := a.lib
	if lib == nil {
		return
	}
	a.run(func() {
		err := lib.UpdateMetadata(id, m)
		if err == nil && len(cover) > 0 {
			err = lib.SetCover(id, cover)
		}
		a.post(func() {
			if err != nil {
				a.toastError(err)
				return
			}
			a.covers.forget(id)
			a.reload(func() { a.toast("%s", locales.AppToastSaved(m.Title)) })
		})
	})
}

// removeBooks moves the books to the Recycle Bin.
func (a *App) removeBooks(ids []int64) {
	lib := a.lib
	if lib == nil || len(ids) == 0 {
		return
	}
	a.run(func() {
		err := lib.RemoveBooks(ids, a.trash)
		a.post(func() {
			a.chosen.Clear()
			a.curID = 0
			a.reload(func() {
				if err != nil {
					a.toastError(err)
				}
				// The books gone: none when the database refused, all when
				// only a folder couldn't be moved.
				gone := 0
				for _, id := range ids {
					if a.byID[id] == nil {
						gone++
					}
				}
				if gone > 0 {
					a.toast("%s", locales.AppToastMovedToTrash(gone))
				}
			})
		})
	})
}

// addFormat asks for a file and adds it to the book.
func (a *App) addFormat(id int64) {
	lib := a.lib
	hooks, after := a.importPlugins(), a.hookPlugins()
	a.run(func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: locales.AppAddFormatTitle(), Filters: ebookFilter()})
		if err != nil || len(paths) == 0 || lib == nil {
			return
		}
		file, done, warned := processImport(context.Background(), hooks, paths[0])
		err = lib.AddFormat(id, file)
		done()
		if err == nil {
			afterAdd(context.Background(), lib, after, id, strings.ToUpper(strings.TrimPrefix(filepath.Ext(file), ".")), false)
		}
		a.post(func() {
			for _, e := range warned {
				a.toastError(e)
			}
			if err != nil {
				a.toastError(err)
				return
			}
			a.reload(func() {
				a.toast("%s", locales.AppToastAddedFormat(strings.ToUpper(strings.TrimPrefix(filepath.Ext(file), "."))))
			})
		})
	})
}

func (a *App) removeFormat(id int64, format string) {
	lib := a.lib
	after := a.hookPlugins()
	a.run(func() {
		err := lib.RemoveFormat(id, format)
		if err == nil {
			runHooks(context.Background(), lib, after, plugin.HookPostDelete, id, format)
		}
		a.post(func() {
			if err != nil {
				a.toastError(err)
				return
			}
			a.reload(func() { a.toast("%s", locales.AppToastRemovedFormat(format)) })
		})
	})
}

// openBook opens the book's preferred format in the default reader.
func (a *App) openBook(b *calibre.Book, format string) {
	if b == nil || a.lib == nil {
		return
	}
	if format == "" {
		for _, f := range []string{"EPUB", "AZW3", "MOBI", "PDF"} {
			if slices.Contains(b.Formats, f) {
				format = f
				break
			}
		}
		if format == "" && len(b.Formats) > 0 {
			format = b.Formats[0]
		}
	}
	if format == "" {
		a.toast("%s", locales.AppToastNoFiles(b.Title))
		return
	}
	lib := a.lib
	a.run(func() {
		d, err := lib.Details(b)
		if err == nil {
			for _, f := range d.Files {
				if f.Format == format {
					err = mygo.Shell.OpenPath(f.Path)
					break
				}
			}
		}
		if err != nil {
			a.post(func() { a.toastError(err) })
		}
	})
}

// shareMenu adds sharing a book's file to m: an item when the book has one
// format, a submenu of its formats when it has more.
func (a *App) shareMenu(m *ui.Menu, b *calibre.Book) {
	if !shareSupported || len(b.Formats) == 0 {
		return
	}
	icon := icons.Menu(icons.Share)
	if len(b.Formats) == 1 {
		if m.Item(locales.AppShareItem()).Icon(icon).Chosen() {
			a.shareBook(b, b.Formats[0])
		}
		return
	}
	m.Submenu(locales.AppShareMenu(), func(m *ui.Menu) {
		for _, f := range b.Formats {
			if m.Item(f).Chosen() {
				a.shareBook(b, f)
			}
		}
	}).Icon(icon)
}

// shareBook shows the system's share sheet for b's file of format.
func (a *App) shareBook(b *calibre.Book, format string) {
	if b == nil || a.lib == nil {
		return
	}
	lib, win := a.lib, a.win
	a.run(func() {
		d, err := lib.Details(b)
		if err != nil {
			a.post(func() { a.toastError(err) })
			return
		}
		for _, f := range d.Files {
			if f.Format == format {
				shareFiles(win, []string{f.Path})
				return
			}
		}
	})
}

func (a *App) revealBook(b *calibre.Book) {
	if b != nil && a.lib != nil {
		mygo.Shell.ShowItemInFolder(a.lib.Dir(b))
	}
}

func (a *App) chooseLibrary() {
	a.run(func() {
		dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: locales.AppOpenCalibreLibraryTitle(), Directory: true, CreateDirectories: true})
		if err != nil || len(dirs) == 0 {
			return
		}
		a.post(func() { a.openLibrary(dirs[0]) })
	})
}
