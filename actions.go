package main

import (
	"log"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// action is something to do with a result: the first of a result's
// actions is what Enter does.
type action struct {
	title  string
	mods   ui.Modifiers
	key    ui.Key
	keys   []string // as the menu shows the shortcut
	danger bool
	run    func(l *launcher, r Result)
}

var (
	actOpen      = action{title: "打开", key: ui.KeyEnter, keys: []string{"↵"}, run: (*launcher).open}
	actOpenApp   = action{title: "打开应用", key: ui.KeyEnter, keys: []string{"↵"}, run: (*launcher).open}
	actReveal    = action{title: "在 Finder 中显示", mods: ui.Cmd, key: ui.KeyEnter, keys: []string{"⌘", "↵"}, run: (*launcher).reveal}
	actCopyPath  = action{title: "拷贝路径", mods: ui.Cmd | ui.Shift, key: ui.KeyC, keys: []string{"⌘", "⇧", "C"}, run: (*launcher).copyPath}
	actCopyFile  = action{title: "拷贝文件", mods: ui.Cmd | ui.Alt, key: ui.KeyC, keys: []string{"⌘", "⌥", "C"}, run: (*launcher).copyFile}
	actTerminal  = action{title: "在终端中打开", mods: ui.Cmd, key: ui.KeyT, keys: []string{"⌘", "T"}, run: (*launcher).openTerminal}
	actTrash     = action{title: "移到废纸篓", danger: true, run: (*launcher).trash}
	actCopyValue = action{title: "拷贝结果", key: ui.KeyEnter, keys: []string{"↵"}, run: (*launcher).copyValue}
	actSearchWeb = action{title: "在浏览器中搜索", key: ui.KeyEnter, keys: []string{"↵"}, run: (*launcher).open}
)

// actionsFor lists what can be done with r, the default first.
func actionsFor(r Result) []action {
	switch r.Kind {
	case KindApp:
		return []action{actOpenApp, actReveal, actCopyPath}
	case KindFile:
		return []action{actOpen, actReveal, actCopyPath, actCopyFile, actTerminal, actTrash}
	case KindFolder:
		return []action{actOpen, actReveal, actTerminal, actCopyPath, actCopyFile, actTrash}
	case KindCalc:
		return []action{actCopyValue}
	case KindWeb:
		return []action{actSearchWeb}
	}
	return nil
}

// open opens r with its default app, launches an app, or opens a URL. The
// panel hides first, so the app the user was in has the keyboard until
// the opened one takes it.
func (l *launcher) open(r Result) {
	l.hide()
	var err error
	switch {
	case r.Kind == KindWeb:
		err = mygo.Shell.OpenExternal(r.Value)
	case r.App != nil:
		err = mygo.Shell.LaunchApp(*r.App)
	default:
		err = mygo.Shell.OpenPath(r.Path)
	}
	if err != nil {
		log.Printf("opening %s: %v", r.Title, err)
		return
	}
	if r.Kind != KindWeb {
		l.store.record(r, l.now())
	}
}

func (l *launcher) reveal(r Result) {
	l.hide()
	mygo.Shell.ShowItemInFolder(r.Path)
	l.store.record(r, l.now())
}

func (l *launcher) copyPath(r Result) {
	mygo.Clipboard.WriteText(r.Path)
	l.flash("已拷贝路径", true)
}

func (l *launcher) copyFile(r Result) {
	if err := mygo.Clipboard.WriteFiles(r.Path); err != nil {
		log.Print(err)
		return
	}
	l.flash("已拷贝文件", true)
}

func (l *launcher) copyValue(r Result) {
	mygo.Clipboard.WriteText(r.Value)
	l.flash("已拷贝 "+r.Value, true)
}

// openTerminal opens Terminal in the folder, or in the folder of a file.
func (l *launcher) openTerminal(r Result) {
	dir := r.Path
	if r.Kind != KindFolder {
		dir = filepath.Dir(dir)
	}
	l.hide()
	if err := exec.Command("open", "-a", "Terminal", dir).Run(); err != nil {
		log.Printf("opening Terminal in %s: %v", dir, err)
	}
}

// trash moves the file to the Trash, never deleting it for good, and takes
// it out of the results.
func (l *launcher) trash(r Result) {
	if err := mygo.Shell.TrashItem(r.Path); err != nil {
		l.flash("无法移到废纸篓", false)
		log.Print(err)
		return
	}
	for id, rs := range l.found {
		l.found[id] = slicesDeleteKey(rs, r.Key)
	}
	l.store.forget(r.Key)
	l.compose()
	l.flash("已移到废纸篓", false)
}

func slicesDeleteKey(rs []Result, key string) []Result {
	out := rs[:0]
	for _, r := range rs {
		if r.Key != key {
			out = append(out, r)
		}
	}
	return out
}

// filterActions keeps the actions whose title contains the filter.
func filterActions(as []action, filter string) []action {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return as
	}
	var out []action
	for _, a := range as {
		if r, _ := match(a.title, filter); r >= 0 {
			out = append(out, a)
		}
	}
	return out
}
