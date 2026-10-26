package main

import (
	"log"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// shortcutChoices are the shortcuts settings offer, as macOS writes them.
var shortcutChoices = []struct{ label, accelerator string }{
	{"⌥ Space", "Alt+Space"},
	{"⌥ ⇧ Space", "Alt+Shift+Space"},
	{"⌃ Space", "Ctrl+Space"},
	{"⌘ ⇧ Space", "Cmd+Shift+Space"},
	{"⌘ ⌥ Space", "CmdOrCtrl+Alt+Space"},
}

func shortcutLabel(acc string) string {
	for _, s := range shortcutChoices {
		if strings.EqualFold(s.accelerator, acc) {
			return s.label
		}
	}
	return acc
}

func (l *launcher) openSettings() {
	if l.settingsWin != nil {
		l.settingsWin.Show()
		l.settingsWin.Focus()
		mygo.App.Focus()
		return
	}
	l.hide()
	l.loginOn = mygo.App.OpenAtLogin()
	l.shortcutLabel = shortcutLabel(l.shortcut)
	l.settingsErr = ""
	l.settingsWin = mygo.NewWindow(mygo.WindowOptions{
		Title:         "Spectra 设置",
		Width:         480,
		Height:        300,
		DisableResize: true,
		Content:       ui.View(l.settingsView),
	})
	l.settingsWin.OnClosed(func() { l.settingsWin = nil })
	l.settingsWin.Focus()
	mygo.App.Focus()
}

func (l *launcher) settingsView(c *ui.Context) {
	t := c.Theme()
	field := func(label string, fn func()) {
		ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
			ui.Text(c, label).TextColor(t.TextMuted).Width(120).TextAlign(ui.End).Padding(5, 0)
			ui.Column(c).Grow(1).Gap(6).Children(fn)
		})
	}
	ui.Column(c).Fill().Padding(28, 32).Gap(18).Children(func() {
		field("唤起快捷键", func() {
			labels := make([]string, len(shortcutChoices))
			for i, s := range shortcutChoices {
				labels[i] = s.label
			}
			if ui.Select(c, &l.shortcutLabel, labels).Width(180).Label("唤起快捷键").Changed() {
				l.changeShortcut()
			}
			msg := "如果与 Spotlight、Raycast 等冲突，请换一个。"
			color := t.TextMuted
			if l.settingsErr != "" {
				msg, color = l.settingsErr, t.Danger
			}
			ui.Text(c, msg).FontSize(11.5).TextColor(color)
		})
		field("启动", func() {
			if ui.Checkbox(c, &l.loginOn, "登录时自动启动").Changed() {
				if err := mygo.App.SetOpenAtLogin(l.loginOn); err != nil {
					log.Print(err)
					l.loginOn = !l.loginOn
				}
			}
		})
		field("网页搜索", func() {
			if ui.Select(c, &l.settings.SearchEngine, searchEngineNames).Width(180).Label("网页搜索").Changed() {
				l.store.saveSettings(l.settings)
			}
		})
		field("最近记录", func() {
			if ui.Button(c, "清除最近记录").Clicked() {
				l.store.clear()
				l.compose()
			}
		})
	})
}

func (l *launcher) changeShortcut() {
	acc := ""
	for _, s := range shortcutChoices {
		if s.label == l.shortcutLabel {
			acc = s.accelerator
		}
	}
	if acc == "" || acc == l.shortcut {
		return
	}
	if err := mygo.GlobalShortcut.Register(acc, l.toggle); err != nil {
		l.settingsErr = shortcutLabel(acc) + " 已被其他应用占用"
		l.shortcutLabel = shortcutLabel(l.shortcut)
		return
	}
	if l.shortcut != "" && l.shortcut != acc {
		mygo.GlobalShortcut.Unregister(l.shortcut)
	}
	l.shortcut, l.settingsErr = acc, ""
	l.settings.Shortcut = acc
	l.store.saveSettings(l.settings)
}
