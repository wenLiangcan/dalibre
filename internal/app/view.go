package app

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"dalibre/cmd/dalibre/locales"
	"dalibre/internal/app/icons"
	"dalibre/internal/app/theme"
	"dalibre/internal/app/widgets"
	"dalibre/internal/catalog"
)

// view builds the window from the state, every frame.
func (a *App) view(c *ui.Context) {
	sys := c.Theme()
	c.SetTheme(theme.Use(sys.Dark).Theme(sys))
	c.SetMenuPresentation(ui.PopoverMenus) // menus drawn in the window, in the design's look

	for _, t := range a.toasts {
		typ := "success"
		if t.error {
			typ = "error"
		}
		c.AddToast(ui.Toast{Title: t.text, Type: typ, Action: t.action, OnAction: t.onAction, OnClose: t.onClose})
	}
	a.toasts = nil

	a.shortcuts(c)

	if a.lib == nil {
		a.welcomeScreen(c)
		a.dialogs(c)
		return
	}
	a.welcome.missing = nil // looked at again when the welcome screen next shows

	root := ui.Row(c).Fill().AlignItems(ui.Stretch).Background(pal().BgApp).TextColor(pal().Text)
	if files := root.DroppedFiles(); files != nil && a.lib != nil {
		a.importFiles(files)
	}
	dragOver := root.FileDragOver() && a.lib != nil
	root.Children(func() {
		a.sidebar(c)
		// The toolbar heads the middle column only; the detail panel runs
		// the window's full height beside it, as in Figma.
		ui.Column(c.Key("main")).Grow(1).MinWidth(0).ClipX().Bind(&a.mainBox).AlignItems(ui.Stretch).Children(func() {
			switch a.page {
			case pageGetBooks:
				a.getBooksToolbar(c)
			case pageCatalog:
				a.catalogToolbar(c)
			default:
				a.toolbar(c)
			}
			ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				switch a.page {
				case pageGetBooks:
					a.getBooksContent(c)
				case pageCatalog:
					a.catalogContent(c)
				default:
					a.mainContent(c)
				}
			})
		})
		if a.detailShown() {
			a.detailPanel(c)
		}
		if dragOver {
			a.dropOverlay(c)
		}
	})
	a.dialogs(c)
}

func (a *App) shortcuts(c *ui.Context) {
	c.OnWindowShortcut(ui.Cmd, ui.KeyO, func() {
		if a.lib != nil {
			a.chooseBooks()
		} else if !a.anyDialog() {
			a.chooseLibrary()
		}
	})
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyN, func() {
		if !a.anyDialog() {
			a.openCreateSheet()
		}
	})
	focus := func() {
		if a.page == pageGetBooks {
			a.gb.focus.Focus()
		} else {
			a.searchFocus.Focus()
		}
	}
	c.OnWindowShortcut(ui.Cmd, ui.KeyComma, func() { a.openSettings(a.section) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyF, focus)
	c.OnWindowShortcut(ui.Cmd, ui.KeyK, focus)
	if a.anyDialog() {
		return
	}
	c.OnWindowShortcut(ui.Cmd, ui.KeyI, func() { a.openEditor(a.current()) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyE, func() { a.editBook(a.current(), "") })
	c.OnWindowShortcut(ui.Cmd|ui.Alt, ui.KeyI, func() {
		a.settings.HideDetail = !a.settings.HideDetail
		a.saveSettings()
	})
	if !c.FocusTakesText() && a.page == pageLibrary {
		if c.Shortcut(0, ui.KeyDelete) || c.Shortcut(0, ui.KeyBackspace) {
			a.askDelete(a.selectedIDs())
		}
	}
}

func (a *App) anyDialog() bool {
	return a.editOpen || a.deleteOpen || a.welcome.notLib.open || a.welcome.create.open || a.settingsOpen || a.consent != nil ||
		a.act.form != nil || a.act.review != nil
}

// ---- Sidebar

func (a *App) sidebar(c *ui.Context) {
	bar := c.TitleBar()
	ui.Column(c).Width(theme.SidebarWidth).Background(pal().BgSidebar).BorderWidth(0, 1, 0, 0).BorderColor(pal().BorderSubtle).AlignItems(ui.Stretch).Children(func() {
		// Room for macOS's traffic lights; a place to drag the window by.
		top := float32(theme.Space12)
		if bar.Left > 0 {
			top = bar.Height
			if runtime.GOOS == "darwin" {
				// The inset title bar is taller than the traffic lights need;
				// the switcher can sit just under them.
				top = max(bar.Height-theme.Space12, theme.Space12)
			}
		}
		ui.Box(c).Height(top).DragWindow()
		ui.Box(c).Padding(0, theme.Space8, theme.Space8, theme.Space8).Children(func() { a.librarySwitcher(c) })
		ui.Scroll(c).Grow(1).Padding(theme.Space8, theme.Space8, theme.Space16, theme.Space8).Gap(theme.Space20).AlignItems(ui.Stretch).Children(func() {
			if a.lib == nil {
				return
			}
			defer a.getBooksNav(c)
			ui.Column(c).Gap(theme.Space2).AlignItems(ui.Stretch).Children(func() {
				a.navItem(c, "all", icons.Library, locales.MainNavAllBooks(), a.counts.All, a.query.Shelf == catalog.AllBooks && a.query.Filter.Kind == catalog.NoFilter, nil, func() {
					a.setQuery(catalog.AllBooks, catalog.Filter{})
				})
				a.navItem(c, "recent", icons.Clock, locales.MainNavRecentlyAdded(), a.counts.Recent, a.query.Shelf == catalog.RecentlyAdded && a.query.Filter.Kind == catalog.NoFilter, nil, func() {
					a.setQuery(catalog.RecentlyAdded, catalog.Filter{})
				})
			})
			a.navSection(c, "formats", locales.MainNavFormats(), catalog.ByFormat, a.counts.Formats, icons.File, 0)
			a.toolsNav(c)
			a.catalogsNav(c)
		})
		if a.lib == nil {
			ui.Column(c).Padding(0, theme.Space8, theme.Space8, theme.Space8).Gap(theme.Space20).AlignItems(ui.Stretch).Children(func() {
				a.catalogsNav(c)
				a.getBooksNav(c)
			})
		}
		a.settingsNav(c)
		a.sidebarFooter(c)
	})
}

func (a *App) setQuery(shelf catalog.Shelf, f catalog.Filter) {
	a.page = pageLibrary
	a.closeResults()
	a.query.Shelf, a.query.Filter = shelf, f
	a.curID = 0
	a.chosen.Clear()
}

func (a *App) navItem(c *ui.Context, key string, icon *ui.SVG, label string, count int, selected bool, iconColor *ui.Color, onClick func()) {
	if key != "getbooks" && !strings.HasPrefix(key, "catalog:") && !strings.HasPrefix(key, "settings:") && a.page != pageLibrary {
		selected = false
	}
	if a.act.results != nil && a.page == pageLibrary && !strings.HasPrefix(key, "tool:") {
		selected = false // an action's results show instead of the shelf
	}
	row := ui.ButtonBase(c.Key(key)).Height(30).Radius(theme.RadiusSM).Padding(0, theme.Space8).Gap(theme.Space8).AlignItems(ui.Center).Label(label)
	fg := pal().Icon
	switch {
	case selected:
		row.Background(pal().SurfaceRaised).Shadow(0, 1, 2, 0, ui.RGBA(26, 23, 18, 0.06)).Shadow(0, 0, 0, 1, pal().BorderSubtle.Alpha(0.6))
		fg = pal().TextAccent
	case row.Hovered():
		row.Background(pal().Hover)
	}
	if iconColor != nil {
		fg = *iconColor
	}
	row.OnClick(onClick).Children(func() {
		ui.Icon(c, icon).FontSize(16).TextColor(fg)
		t := ui.Text(c, label).Grow(1).SingleLine().TextColor(pal().Text)
		if selected {
			t.FontWeight(500)
		}
		if count > 0 {
			ui.Textf(c, "%s", groupDigits(count)).Font(theme.FontMono).FontSize(12).TextColor(pal().TextTertiary)
		}
	})
}

// navSection is a collapsible group of filters; limit caps how many rows
// show before "Show all".
func (a *App) navSection(c *ui.Context, key, title string, kind catalog.FilterKind, items []catalog.Count, icon *ui.SVG, limit int) {
	if len(items) == 0 {
		return
	}
	open := a.sectionsOn[key]
	ui.Column(c.Key(key)).Gap(theme.Space2).AlignItems(ui.Stretch).Children(func() {
		h := ui.ButtonBase(c).Height(24).Padding(0, theme.Space8).AlignItems(ui.Center).Radius(theme.RadiusXS).Label(title)
		h.OnClick(func() { a.sectionsOn[key] = !a.sectionsOn[key] }).Children(func() {
			widgets.Overline(c, strings.ToUpper(title)).Grow(1)
			if h.Hovered() || !open {
				ui.Textf(c, "%d", len(items)).Font(theme.FontMono).FontSize(11).TextColor(pal().TextTertiary)
			}
			chev := icons.ChevronDown
			if !open {
				chev = icons.ChevronRight
			}
			ui.Icon(c, chev).FontSize(12).TextColor(pal().IconMuted)
		})
		if !open {
			return
		}
		all := a.sectionsOn[key+":all"]
		for i, it := range items {
			if limit > 0 && i >= limit && !all {
				more := ui.ButtonBase(c).Height(26).Padding(0, theme.Space8, 0, theme.Space32).AlignItems(ui.Center).Radius(theme.RadiusSM).Label(locales.CommonShowAll())
				if more.Hovered() {
					more.Background(pal().Hover)
				}
				more.OnClick(func() { a.sectionsOn[key+":all"] = true }).Children(func() {
					ui.Text(c, locales.MainShowAllCount(len(items))).FontSize(12).TextColor(pal().TextAccent)
				})
				break
			}
			value := it.Value
			label := value
			var col *ui.Color
			if kind == catalog.ByFormat {
				fg := pal().Format(value).FG
				col = &fg
			}
			if kind == catalog.ByLanguage {
				label = languageName(value)
			}
			sel := a.query.Filter == catalog.Filter{Kind: kind, Value: value}
			a.navItem(c, key+":"+value, icon, label, it.N, sel, col, func() {
				a.setQuery(catalog.AllBooks, catalog.Filter{Kind: kind, Value: value})
			})
		}
	})
}

func (a *App) librarySwitcher(c *ui.Context) {
	name, sub := locales.MainNoLibrary(), locales.MainOpenOrCreateLibrary()
	if a.lib != nil {
		name = filepath.Base(a.lib.Root)
		sub = locales.CommonBookCount(a.counts.All)
	}
	m := ui.MenuBase(c, &a.libraryMenuOpen)
	b := m.Trigger.Padding(theme.Space8).Gap(theme.Space10).AlignItems(ui.Center).Radius(theme.RadiusMD).
		Background(pal().SurfaceRaised).Border(1, pal().BorderSubtle).Shadow(0, 1, 2, 0, ui.RGBA(26, 23, 18, 0.05)).Label(locales.CommonLibrary())
	if b.Hovered() {
		b.Border(1, pal().Border)
	}
	m.Popup(func(panel ui.Element) {
		t := c.Theme()
		// Keep the switcher compact regardless of how many libraries were opened.
		panel.MinWidth(180).MaxWidth(340).MaxHeight(360).Padding(theme.Space4).Radius(theme.RadiusSM+2).
			Background(t.Background).Border(1, t.Border).Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.18)).Margin(theme.Space4, 0)
		// Every recently opened library, the open one checked.
		recent := a.settings.Recent
		if a.lib != nil && !slices.Contains(recent, a.lib.Root) {
			recent = append([]string{a.lib.Root}, recent...)
		}
		for _, r := range recent {
			open := a.lib != nil && r == a.lib.Root
			item := m.CheckItem(open).Label(filepath.Base(r)).Description(r)
			text, muted := libraryMenuRow(item, t)
			item.Children(func() {
				mark := ui.Box(c).Size(16, 16).Shrink(0).Center()
				if open {
					mark.Children(func() { ui.Icon(c, icons.Check).FontSize(14).TextColor(text) })
				}
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					a.recentLibraryRow(ui.MenuItemContent{Context: c, Highlighted: item.Highlighted(), Text: text, Muted: muted}, r)
				})
			})
			if item.Clicked() && !open {
				a.openLibrary(r)
			}
		}
		if len(recent) > 0 {
			ui.Box(c).Height(1).Margin(theme.Space4, theme.Space8).Background(t.Border).Shrink(0)
		}
		if libraryMenuAction(c, m, t, locales.MainOpenLibraryMenu(), len(recent) > 0) {
			a.chooseLibrary()
		}
		if libraryMenuAction(c, m, t, locales.MainNewLibraryMenu(), len(recent) > 0) {
			a.openCreateSheet()
		}
	})
	b.Children(func() {
		ui.Box(c).Size(28, 28).Radius(theme.RadiusSM).Background(pal().Inverse).Center().Children(func() {
			ui.Text(c, "D").FontWeight(700).FontSize(16).TextColor(pal().TextOnInverse)
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, name).FontSize(13).FontWeight(500).SingleLine()
			ui.Text(c, sub).FontSize(12).TextColor(pal().TextTertiary).SingleLine()
		})
		ui.Icon(c, icons.ChevronDown).FontSize(14).TextColor(pal().IconMuted)
	})
}

// libraryMenuRow keeps custom switcher rows styled like the toolkit's menus.
func libraryMenuRow(row ui.Element, t *ui.Theme) (text, muted ui.Color) {
	row.AlignItems(ui.Center).Gap(theme.Space8).Padding(theme.Space6, theme.Space8).Radius(theme.RadiusSM).Shrink(0)
	text, muted = t.Text, t.TextMuted
	if row.Highlighted() {
		text, muted = t.AccentText, t.AccentText
		row.Background(t.Accent).TextColor(text)
	}
	return
}

func libraryMenuAction(c *ui.Context, m ui.MenuParts, t *ui.Theme, label string, marks bool) bool {
	row := m.Item().Label(label)
	text, _ := libraryMenuRow(row, t)
	row.Children(func() {
		if marks {
			ui.Box(c).Size(16, 16).Shrink(0)
		}
		ui.Text(c, label).SingleLine().TextColor(text)
	})
	return row.Clicked()
}

// revealLabel names showing a file in the system's file manager.
func revealLabel() string {
	if runtime.GOOS == "darwin" {
		return locales.MainShowInFinder()
	}
	return locales.CommonShowInFolder()
}

// recentLibraryRow shows a library in the switcher's menu: its name and
// path, and, while the item is highlighted, a button revealing its folder.
func (a *App) recentLibraryRow(x ui.MenuItemContent, root string) {
	c := x.Context
	ui.Row(c).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, filepath.Base(root)).SingleLine().TextColor(x.Text)
			ui.Text(c, root).FontSize(12).SingleLine().TextColor(x.Muted)
		})
		reveal := ui.ButtonBase(c).Size(24, 24).Radius(theme.RadiusXS).Center().Label(locales.MainRevealRow(revealLabel(), filepath.Base(root))).Tooltip(revealLabel())
		if !x.Highlighted {
			reveal.Opacity(0)
		} else if reveal.Hovered() {
			reveal.Background(x.Text.Alpha(0.12))
		}
		reveal.OnClick(func() { revealPath(root) }).Children(func() {
			ui.Icon(c, icons.Folder).FontSize(14).TextColor(x.Text)
		})
	})
}

func (a *App) sidebarFooter(c *ui.Context) {
	if a.busy == "" && len(a.downloads) == 0 {
		return
	}
	ui.Column(c).Padding(0, theme.Space8, theme.Space12, theme.Space8).Gap(theme.Space8).AlignItems(ui.Stretch).Children(func() {
		widgets.Hairline(c)
		if a.busy != "" {
			ui.Row(c).Padding(theme.Space4, theme.Space4, theme.Space4, theme.Space8).Gap(theme.Space10).AlignItems(ui.Center).Children(func() {
				if stop := a.act.cancel; stop != nil {
					// An action can be stopped: what it does is the plugin's.
					defer func() {
						if widgets.IconButton(c, icons.X, locales.ActionStop(a.busy), false).Clicked() {
							stop()
						}
					}()
				}
				ui.Spinner(c).Size(16, 16).Label(a.busy)
				ui.Column(c).Grow(1).Gap(theme.Space6).AlignItems(ui.Stretch).Children(func() {
					label := locales.MainBusyEllipsis(a.busy)
					if a.busyTotal > 0 {
						label = locales.MainBusyProgress(a.busy, a.busyDone, a.busyTotal)
					}
					ui.Text(c, label).FontSize(12).FontWeight(500).SingleLine()
					v := -1.0
					if a.busyTotal > 0 {
						v = float64(a.busyDone) / float64(a.busyTotal)
					}
					ui.Progress(c, v).Height(4)
				})
			})
		}
		if len(a.downloads) > 0 {
			a.jobsButton(c)
		}
	})
}

// ---- Toolbar

func (a *App) toolbar(c *ui.Context) {
	// The window's caption buttons, where there are some on the right,
	// sit over the detail panel when it shows, else over the toolbar.
	right := float32(theme.Space24)
	if !a.detailShown() {
		right += c.TitleBar().Right
	}
	ui.Row(c).Height(theme.ToolbarHeight).Padding(0, right, 0, theme.Space32).Gap(theme.Space8).AlignItems(ui.Center).DragWindow().Children(func() {
		title, count := "Dalibre", ""
		if a.lib != nil {
			title, count = a.viewTitle(), groupDigits(len(a.visible()))
		}
		results := a.act.results
		if results != nil {
			title, count = results.title, locales.ActionGroups(results.groupCount())
		}
		// In a narrow middle column (a small window with the detail panel
		// open) the controls come first: the search field gives up width,
		// the title goes (the sidebar still names the view) and Add books
		// keeps only its icon, so nothing runs under the detail panel.
		w := a.mainBox.Bounds(c).W
		noTitle, compact := w > 0 && w < 640, w > 0 && w < 540
		ui.Row(c).Grow(1).MinWidth(0).Gap(theme.Space8).AlignItems(ui.Baseline).Children(func() {
			if noTitle {
				return
			}
			ui.Text(c, title).FontWeight(600).FontSize(22).LetterSpacing(-0.4).SingleLine().Shrink(1)
			if count != "" {
				ui.Text(c, count).Font(theme.FontMono).FontSize(12).TextColor(pal().TextTertiary)
			}
		})
		if a.lib == nil {
			return
		}
		// An action's results are a list in the action's own order: nothing
		// to search, lay out or sort.
		if results == nil {
			a.listControls(c)
		}
		if widgets.IconButton(c, icons.PanelRight, locales.MainDetailsTooltip(), !a.settings.HideDetail).Clicked() {
			a.settings.HideDetail = !a.settings.HideDetail
			a.saveSettings()
		}
		a.sharingButton(c)
		a.agentsButton(c)
		a.pluginsButton(c)
		ui.Box(c).Size(1, 20).Background(pal().Border).Margin(0, theme.Space4)
		label := locales.CommonAddBooks()
		if compact {
			label = ""
		}
		add := widgets.Button(c, widgets.Primary, icons.Plus, label).Label(locales.CommonAddBooks()).Tooltip(locales.MainAddBooksTooltip()).Disabled(a.busy != "")
		if compact {
			add.Width(theme.ControlM).Padding(0)
		}
		if add.Clicked() {
			a.chooseBooks()
		}
	})
}

// listControls are the toolbar's controls for the list of books: search,
// grid or list, and the order.
func (a *App) listControls(c *ui.Context) {
	ui.SearchField(c, &a.query.Search).Placeholder(locales.MainSearchPlaceholder()).Label(locales.MainSearchLabel()).Width(260).Shrink(1).MinWidth(104).Bind(&a.searchFocus).OnChange(func() {
		a.curID = 0
	})
	a.viewToggle(c)
	sort := widgets.IconButton(c, icons.Sort, locales.MainSortButton(sortLabel(a.query.Sort)), false)
	sort.PopoverMenuButton(func(m *ui.Menu) {
		for _, k := range catalog.SortKeys {
			if m.Item(sortLabel(k.Key)).Radio(a.query.Sort == k.Key).Chosen() {
				a.query.Sort = k.Key
				a.saveSettings()
			}
		}
		m.Separator()
		if m.Item(locales.MainSortAscending()).Radio(!a.query.Descending).Chosen() {
			a.query.Descending = false
			a.saveSettings()
		}
		if m.Item(locales.MainSortDescending()).Radio(a.query.Descending).Chosen() {
			a.query.Descending = true
			a.saveSettings()
		}
	})
}

func (a *App) viewTitle() string {
	f := a.query.Filter
	switch f.Kind {
	case catalog.ByLanguage:
		return languageName(f.Value)
	case catalog.NoFilter:
		if a.query.Shelf == catalog.RecentlyAdded {
			return locales.MainNavRecentlyAdded()
		}
		return locales.MainNavAllBooks()
	}
	return f.Value
}

// viewToggle is the ViewToggle component: grid or list.
func (a *App) viewToggle(c *ui.Context) {
	sel := 0
	if a.settings.ListView {
		sel = 1
	}
	seg := ui.SegmentedBase(c, &sel, 2)
	seg.Track.Padding(theme.Space2).Gap(theme.Space2).Radius(theme.RadiusSM).Background(pal().Subtle).Label(locales.MainViewToggleLabel()).Children(func() {
		for i, ic := range []*ui.SVG{icons.Grid, icons.List} {
			s := seg.Segment(i).Size(36, 28).Radius(theme.RadiusXS).Center().Label([]string{locales.MainViewGrid(), locales.MainViewList()}[i])
			fg := pal().IconMuted
			if i == sel {
				s.Background(pal().SurfaceRaised).Shadow(0, 1, 2, 0, ui.RGBA(26, 23, 18, 0.08))
				fg = pal().Text
			} else if s.Hovered() {
				fg = pal().Text
			}
			s.TextColor(fg).Children(func() { ui.Icon(c, ic).FontSize(16) })
		}
	})
	if (sel == 1) != a.settings.ListView {
		a.settings.ListView = sel == 1
		a.saveSettings()
	}
}

// ---- Main content

func (a *App) mainContent(c *ui.Context) {
	switch {
	case a.loading:
		ui.Column(c).Grow(1).Center().Gap(theme.Space12).Children(func() {
			ui.Spinner(c).Size(24, 24).Label(locales.CommonLoading())
			ui.Text(c, locales.MainOpeningLibrary()).TextColor(pal().TextSecondary)
		})
		return
	}
	if a.act.results != nil {
		a.resultsView(c)
		return
	}
	a.filterBar(c)
	shown := a.visible()
	if len(shown) == 0 {
		a.emptyState(c)
		return
	}
	a.syncCur(shown)
	if a.settings.ListView {
		a.listView(c, shown)
	} else {
		a.gridView(c, shown)
	}
	a.adoptCur()
}

func filterKindName(k catalog.FilterKind) string {
	switch k {
	case catalog.ByFormat:
		return locales.MainFilterFormat()
	case catalog.ByAuthor:
		return locales.CommonAuthor()
	case catalog.BySeries:
		return locales.CommonSeries()
	case catalog.ByTag:
		return locales.CommonTag()
	case catalog.ByPublisher:
		return locales.CommonPublisher()
	case catalog.ByLanguage:
		return locales.CommonLanguage()
	}
	return ""
}

// sortLabel names an order in the sort menu.
func sortLabel(k catalog.SortKey) string {
	switch k {
	case catalog.SortDateAdded:
		return locales.MainSortDateAdded()
	case catalog.SortTitle:
		return locales.CommonTitle()
	case catalog.SortAuthor:
		return locales.CommonAuthor()
	case catalog.SortSeries:
		return locales.CommonSeries()
	case catalog.SortRating:
		return locales.CommonRating()
	case catalog.SortPublished:
		return locales.CommonPublished()
	case catalog.SortSize:
		return locales.MainSortSize()
	case catalog.SortModified:
		return locales.MainSortModified()
	}
	return k.Label()
}

func (a *App) emptyState(c *ui.Context) {
	ui.Column(c).Grow(1).Center().Gap(theme.Space12).Padding(theme.Space48).Children(func() {
		ui.Box(c).Size(56, 56).Radius(theme.RadiusFull).Background(pal().Subtle).Center().Children(func() {
			ic := icons.Search
			if len(a.books) == 0 {
				ic = icons.BookOpen
			}
			ui.Icon(c, ic).FontSize(24).TextColor(pal().IconMuted)
		})
		if len(a.books) == 0 {
			ui.Text(c, locales.MainEmptyTitle()).FontWeight(600).FontSize(18)
			ui.Text(c, locales.MainEmptyHint()).TextColor(pal().TextSecondary)
			if widgets.Button(c, widgets.Primary, icons.Plus, locales.CommonAddBooks()).Margin(theme.Space8, 0, 0, 0).Clicked() {
				a.chooseBooks()
			}
			return
		}
		ui.Text(c, locales.MainNoMatchTitle()).FontWeight(600).FontSize(18)
		ui.Text(c, locales.MainNoMatchHint()).TextColor(pal().TextSecondary)
		if widgets.Button(c, widgets.Secondary, nil, locales.MainClearFilters()).Margin(theme.Space8, 0, 0, 0).Clicked() {
			a.query.Search = ""
			a.setQuery(catalog.AllBooks, catalog.Filter{})
		}
	})
}

func groupDigits(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

var languageNames = map[string]string{
	"eng": "English", "zho": "Chinese", "jpn": "Japanese", "kor": "Korean", "fra": "French", "deu": "German",
	"spa": "Spanish", "ita": "Italian", "por": "Portuguese", "rus": "Russian", "ara": "Arabic", "nld": "Dutch",
	"swe": "Swedish", "pol": "Polish", "tur": "Turkish", "lat": "Latin", "und": "Undetermined",
}

func languageName(code string) string {
	if n, ok := languageNames[code]; ok {
		return n
	}
	return code
}

// pal is the palette of the frame being built.
func pal() *theme.Palette { return theme.P() }
