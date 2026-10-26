// Spectra finds apps and files wherever the user is: a
// shortcut shows a panel over every app; type to find apps, files by name
// and by content (from the Spotlight index), or a calculation; Enter opens
// the chosen result and ⌘K shows what else can be done with it. It lives
// in the menu bar, without a Dock icon.
package main

import (
	"context"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// launcher holds what the panel shows. It is read and changed on the main
// thread, by the view and by the callbacks of the app, the tray and the
// shortcut; work on goroutines comes back through post (Window.Update).
type launcher struct {
	panel       *mygo.Window
	settingsWin *mygo.Window
	tray        *mygo.Tray
	store       *store
	settings    Settings
	shortcut    string // the one registered
	quitting    bool

	engine  *Engine
	apps    *appsProvider
	query   string
	filter  Filter
	found   map[string][]Result
	pending map[string]bool
	gen     int

	rows   []row
	chosen int
	moved  bool // the user chose a row: keep it as results come
	list   ui.ListState
	input  ui.Handle

	menuOpen   bool
	menuFilter string
	menuChosen int
	toast      string
	toastTimer *time.Timer

	icons         map[string]*ui.Bitmap
	previews      map[string]*preview
	previewCancel context.CancelFunc
	scale         float64
	fileIcon      func(path string, size int) ([]byte, error)

	// settings window
	loginOn       bool
	shortcutLabel string
	settingsErr   string

	post func(func())
	now  func() time.Time
	// perform does an action; tests replace it to see what would be done.
	perform func(a action, r Result)
}

func newLauncher(st *store, post func(func())) *launcher {
	l := &launcher{
		store:    st,
		settings: st.settings(),
		apps:     &appsProvider{},
		found:    map[string][]Result{},
		icons:    map[string]*ui.Bitmap{},
		previews: map[string]*preview{},
		scale:    2,
		post:     post,
		now:      time.Now,
	}
	l.perform = func(a action, r Result) { a.run(l, r) }
	l.engine = NewEngine(
		calcProvider{},
		l.apps,
		&spotlightProvider{limit: 60},
		&spotlightProvider{content: true, limit: 30},
		webProvider{engine: &l.settings.SearchEngine},
	)
	return l
}

func (l *launcher) ready() {
	l.fileIcon = mygo.Shell.FileIcon
	for _, d := range mygo.Screen.Displays() {
		l.scale = max(l.scale, d.ScaleFactor)
	}
	l.panel = mygo.NewWindow(mygo.WindowOptions{
		Title:         "Spectra",
		Panel:         true,
		Hidden:        true,
		Frameless:     true,
		Transparent:   true,
		Vibrancy:      mygo.VibrancyPopover,
		Width:         panelWidth,
		Height:        panelHeight,
		DisableResize: true,
		Content:       ui.View(l.view),
	})
	l.post = l.panel.Update
	// Hide rather than close, on Escape, ⌘W and when the user goes
	// elsewhere: the panel shows again at once. Quitting destroys it.
	// Quitting closes every window first: the panel lets it then, or the
	// quit would be cancelled.
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { l.quitting = true })
	l.panel.OnClose(func(e *mygo.CloseEvent) {
		if l.quitting {
			return
		}
		e.PreventDefault()
		l.hide()
	})
	l.panel.OnBlur(l.hide)

	l.registerShortcut(l.settings.Shortcut)
	l.setUpTray()
	l.loadApps()
	if !mygo.App.WasOpenedAtLogin() {
		l.show()
	}
}

// registerShortcut takes the shortcut, or the first free fallback when
// another app has it.
func (l *launcher) registerShortcut(want string) error {
	var first error
	for _, s := range []string{want, "Alt+Space", "Alt+Shift+Space", "CmdOrCtrl+Alt+Space"} {
		if s == l.shortcut {
			return first
		}
		err := mygo.GlobalShortcut.Register(s, l.toggle)
		if err == nil {
			if l.shortcut != "" {
				mygo.GlobalShortcut.Unregister(l.shortcut)
			}
			l.shortcut = s
			return first
		}
		log.Printf("shortcut %s: %v", s, err)
		if first == nil {
			first = err
		}
	}
	return first
}

// loadApps lists the installed apps on a goroutine, as at each show, where
// apps may have come or gone since.
func (l *launcher) loadApps() {
	go func() {
		apps, err := mygo.Shell.InstalledApps()
		if err != nil {
			log.Print(err)
			return
		}
		l.post(func() {
			l.apps.apps = apps
			l.found["apps"], _ = l.apps.Search(context.Background(), Query{Text: l.query, Filter: l.filter})
			l.compose()
		})
	}()
}

// show shows the panel on the display with the pointer, a third of the way
// down, with an empty query.
func (l *launcher) show() {
	d := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	w, h := l.panel.Size()
	l.panel.SetPosition(d.WorkArea.X+(d.WorkArea.Width-w)/2, d.WorkArea.Y+(d.WorkArea.Height-h)/3)
	l.query, l.filter, l.menuOpen, l.toast = "", FilterAll, false, ""
	l.search()
	l.loadApps()
	l.input.Focus()
	l.panel.Show()
}

func (l *launcher) hide() {
	l.menuOpen = false
	l.engine.Stop()
	if l.panel != nil {
		l.panel.Hide()
	}
}

func (l *launcher) toggle() {
	if l.panel.IsVisible() {
		l.hide()
	} else {
		l.show()
	}
}

// flash shows a message in the footer for a moment, then hides the panel
// if asked, as after copying.
func (l *launcher) flash(msg string, thenHide bool) {
	l.toast = msg
	if l.toastTimer != nil {
		l.toastTimer.Stop()
	}
	l.toastTimer = time.AfterFunc(700*time.Millisecond, func() {
		l.post(func() {
			l.toast = ""
			if thenHide {
				l.hide()
			}
		})
	})
}

func (l *launcher) setUpTray() {
	var err error
	l.tray, err = mygo.NewTray(mygo.TrayOptions{ID: "spectra", Icon: trayIcon(), IconIsTemplate: true, ToolTip: "Spectra"})
	if err != nil {
		log.Print("no tray icon: ", err)
		return
	}
	l.tray.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "打开 Spectra", Accelerator: l.shortcut, Click: func(*mygo.MenuItem, *mygo.Window) { l.show() }},
		mygo.Separator(),
		{Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { l.openSettings() }},
		mygo.Separator(),
		{Label: "退出 Spectra", Accelerator: "CmdOrCtrl+Q", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	}))
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { l.tray.Destroy() })
}

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running launcher shows itself (OnSecondInstance)
	}
	mygo.App.SetName("Spectra")
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Print(err)
	}
	l := newLauncher(openStore(dir), func(fn func()) { fn() })
	// No Dock icon or app switcher entry: a bundle says so from the start
	// with LSUIElement in mygo.json.
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {}) // keep running without windows
	mygo.App.OnSecondInstance(func([]string, string) { l.show() })
	mygo.App.WhenReady(l.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
