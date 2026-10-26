package main

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	panelWidth, panelHeight = 720, 460
	rowIcon                 = 26 // DIPs
)

var (
	searchIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/></svg>`))
	calcIcon   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 26 26"><rect x="3" y="2" width="20" height="22" rx="5" fill="#ff9f0a"/><rect x="7" y="6" width="12" height="4" rx="1" fill="#fff" opacity=".9"/><g fill="#fff"><circle cx="9" cy="14.5" r="1.4"/><circle cx="13" cy="14.5" r="1.4"/><circle cx="17" cy="14.5" r="1.4"/><circle cx="9" cy="19" r="1.4"/><circle cx="13" cy="19" r="1.4"/><circle cx="17" cy="19" r="1.4"/></g></svg>`))
	globeIcon  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3.5 3 14.5 0 18M12 3c-3 3.5-3 14.5 0 18"/></svg>`))
)

// brandPNG is the app icon's tile, small, for the footer.
//
//go:embed assets/brand.png
var brandPNG []byte

var brand = sync.OnceValue(func() *ui.Bitmap {
	b, _ := ui.DecodeBitmap(brandPNG)
	return b
})

// palette holds the colors of the design, for the light and dark themes.
type palette struct {
	text, muted, faint, sep, selected, mark, kbd, menu ui.Color
}

func colors(t *ui.Theme) palette {
	if t.Dark {
		return palette{
			text: ui.Hex("#f2f2f5"), muted: ui.RGBA(242, 242, 245, 0.62), faint: ui.RGBA(242, 242, 245, 0.38),
			sep: ui.RGBA(255, 255, 255, 0.08), selected: ui.RGBA(255, 255, 255, 0.11), mark: ui.Hex("#ffd36b"),
			kbd: ui.RGBA(255, 255, 255, 0.09), menu: ui.RGBA(40, 40, 46, 0.97),
		}
	}
	return palette{
		text: ui.Hex("#1c1c1f"), muted: ui.RGBA(28, 28, 31, 0.62), faint: ui.RGBA(28, 28, 31, 0.40),
		sep: ui.RGBA(0, 0, 0, 0.07), selected: ui.RGBA(0, 0, 0, 0.07), mark: ui.Hex("#b7791f"),
		kbd: ui.RGBA(0, 0, 0, 0.06), menu: ui.RGBA(250, 250, 252, 0.98),
	}
}

func (l *launcher) view(c *ui.Context) {
	t := c.Theme()
	p := colors(t)
	root := c.Root()
	if c.Vibrancy() {
		root.Background(ui.Transparent) // the material shows through
	} else {
		root.Background(t.Background)
	}
	l.keys(c)

	ui.Column(c).Fill().Children(func() {
		l.searchBar(c, p)
		l.chips(c, p)
		ui.Box(c).Height(1).Background(p.sep)
		r, ok := l.current()
		side := ok && (r.Kind == KindFile || r.Kind == KindFolder)
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			list := l.resultList(c, p)
			if side {
				list.WidthPercent(56)
				ui.Box(c).Width(1).Background(p.sep)
				l.previewPane(c, p, r)
			} else {
				list.Grow(1)
			}
		})
		ui.Box(c).Height(1).Background(p.sep)
		l.footer(c, p)
	})
	if l.menuOpen {
		l.actionMenu(c, p)
	}
}

// keys handles the keyboard of the panel, wherever the focus is: it stays
// in the search field, which leaves these keys.
func (l *launcher) keys(c *ui.Context) {
	// An Escape left to the panel hides it: with a query, it clears it first.
	if l.query != "" && c.Shortcut(0, ui.KeyEscape) {
		l.query = ""
		l.search()
	}
	if c.Shortcut(0, ui.KeyDown) || c.Shortcut(ui.Ctrl, ui.KeyN) {
		l.move(1)
	}
	if c.Shortcut(0, ui.KeyUp) || c.Shortcut(ui.Ctrl, ui.KeyP) {
		l.move(-1)
	}
	if c.Shortcut(0, ui.KeyTab) {
		l.setFilter((l.filter + 1) % Filter(len(filterLabels)))
	}
	if c.Shortcut(ui.Shift, ui.KeyTab) {
		l.setFilter((l.filter + Filter(len(filterLabels)) - 1) % Filter(len(filterLabels)))
	}
	if c.Shortcut(ui.Cmd, ui.KeyK) {
		if _, ok := l.current(); ok {
			l.menuOpen, l.menuFilter, l.menuChosen = true, "", 0
		}
	}
	for n, k := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9} {
		if c.Shortcut(ui.Cmd, k) {
			if i, ok := l.nth(n); ok {
				l.choose(i)
				l.run(actionsFor(l.rows[i].r)[0])
			}
		}
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) {
		l.openSettings()
	}
	c.OnShortcut(ui.Cmd, ui.KeyQ, mygo.App.Quit)
	if r, ok := l.current(); ok {
		for _, a := range actionsFor(r) {
			if a.key != 0 && c.Shortcut(a.mods, a.key) {
				l.run(a)
				break
			}
		}
	}
}

func (l *launcher) setFilter(f Filter) {
	if l.filter != f {
		l.filter = f
		l.search()
	}
}

// run does an action with the chosen result.
func (l *launcher) run(a action) {
	if r, ok := l.current(); ok {
		l.closeMenu()
		l.perform(a, r)
	}
}

// closeMenu closes the actions menu and gives the keyboard back to the
// search field.
func (l *launcher) closeMenu() {
	if l.menuOpen {
		l.menuOpen = false
		l.input.Focus()
	}
}

func (l *launcher) searchBar(c *ui.Context, p palette) {
	ui.Row(c).Height(56).Padding(0, 18).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, searchIcon).FontSize(20).TextColor(p.faint)
		input := ui.TextInputBase(c, &l.query).Bind(&l.input).Grow(1).FontSize(20).TextColor(p.text).
			Placeholder("搜索应用和文件…").Label("搜索")
		if input.Changed() {
			l.search()
		}
		// The field takes Enter, as its submission: it opens the chosen result.
		if input.Submitted() {
			if r, ok := l.current(); ok {
				l.run(actionsFor(r)[0])
			}
		}
		if l.busy() {
			ui.Spinner(c).Size(14, 14)
		}
	})
}

func (l *launcher) chips(c *ui.Context, p palette) {
	ui.Row(c).Padding(0, 14, 10, 14).Gap(6).Children(func() {
		for i, label := range filterLabels {
			chip := ui.Text(c.Key(label), label).FontSize(12).Padding(3, 10).Radius(999)
			if Filter(i) == l.filter {
				chip.Background(p.selected).TextColor(p.text)
			} else {
				chip.Border(1, p.sep).TextColor(p.muted)
			}
			if chip.Clicked() {
				l.setFilter(Filter(i))
				l.input.Focus()
			}
		}
	})
}

func (l *launcher) resultList(c *ui.Context, p palette) ui.Element {
	l.list.Key = func(i int) any {
		if l.rows[i].header != "" {
			return "h:" + l.rows[i].header
		}
		return l.rows[i].r.Key
	}
	l.list.Label = func(i int) string {
		if l.rows[i].header != "" {
			return l.rows[i].header
		}
		return l.rows[i].r.Title
	}
	return ui.List(c, &l.list, len(l.rows), func(i int) { l.resultRow(c, p, i) }).Padding(6, 8).Children(func() {
		if len(l.rows) == 0 {
			msg := "输入以搜索应用、文件和文件内容"
			switch {
			case l.query != "" && l.busy():
				msg = "搜索中…"
			case l.query != "":
				msg = "没有找到结果"
			}
			ui.Text(c, msg).FontSize(13).TextColor(p.faint).Padding(14, 10)
		}
	})
}

func (l *launcher) resultRow(c *ui.Context, p palette, i int) {
	rw := l.rows[i]
	if rw.header != "" {
		ui.Text(c, rw.header).FontSize(11).FontWeight(600).TextColor(p.faint).Padding(10, 10, 4, 10)
		return
	}
	r := rw.r
	big := r.Kind == KindCalc
	h := float32(40)
	if big {
		h = 60
	}
	line := ui.Row(c).Height(h).Padding(0, 10).Gap(10).Radius(9).AlignItems(ui.Center)
	if i == l.chosen {
		line.Background(p.selected)
	}
	line.Children(func() {
		l.resultIcon(c, p, r, big)
		if big {
			ui.Column(c).Grow(1).Children(func() {
				ui.Text(c, r.Title).FontSize(22).FontWeight(600).TextColor(p.text).SingleLine()
				ui.Text(c, r.Subtitle).FontSize(12).TextColor(p.faint).SingleLine()
			})
		} else {
			ui.Row(c).Grow(1).Gap(8).AlignItems(ui.Center).Children(func() {
				l.title(c, p, r).Shrink(1).SingleLine()
				if r.Subtitle != "" {
					ui.Text(c, r.Subtitle).FontSize(12).TextColor(p.faint).Shrink(3).SingleLine()
				}
			})
		}
		if rw.tail != "" {
			ui.Text(c, rw.tail).FontSize(12).TextColor(p.faint)
		}
		if i == l.chosen {
			l.kbd(c, p, "↵")
		}
	})
	if line.DoubleClicked() {
		l.choose(i)
		l.run(actionsFor(r)[0])
	} else if line.Clicked() {
		l.choose(i)
		l.input.Focus()
	}
}

// title shows a result's name with what the query matched in it marked.
func (l *launcher) title(c *ui.Context, p palette, r Result) ui.Element {
	if len(r.Match) == 0 {
		return ui.Text(c, r.Title).FontSize(13).TextColor(p.text)
	}
	rs := []rune(r.Title)
	var spans []ui.Span
	at := 0
	for _, m := range r.Match {
		if m[0] < at || m[1] > len(rs) {
			continue
		}
		if m[0] > at {
			spans = append(spans, ui.Span{Text: string(rs[at:m[0]])})
		}
		spans = append(spans, ui.Span{Text: string(rs[m[0]:m[1]]), Color: p.mark, Weight: 600})
		at = m[1]
	}
	if at < len(rs) {
		spans = append(spans, ui.Span{Text: string(rs[at:])})
	}
	return ui.RichText(c, spans...).FontSize(13).TextColor(p.text)
}

func (l *launcher) resultIcon(c *ui.Context, p palette, r Result, big bool) {
	size := float32(rowIcon)
	if big {
		size = 32
	}
	switch r.Kind {
	case KindCalc:
		ui.Image(c, calcIcon).Size(size, size) // in its own colors
	case KindWeb:
		ui.Box(c).Size(size, size).Center().Children(func() {
			ui.Icon(c, globeIcon).FontSize(18).TextColor(p.muted)
		})
	default:
		if b := l.icon(r.Path, rowIcon); b != nil {
			ui.Image(c, b).Size(size, size)
		} else {
			ui.Box(c).Size(size, size)
		}
	}
}

// icon returns the icon Finder shows for a file, drawn for the sharpest
// display the first time it shows, and kept.
func (l *launcher) icon(path string, size int) *ui.Bitmap {
	key := fmt.Sprintf("%s@%d", path, size)
	if b, ok := l.icons[key]; ok {
		return b
	}
	var b *ui.Bitmap
	if l.fileIcon != nil {
		if data, err := l.fileIcon(path, int(float64(size)*l.scale)); err == nil {
			b, _ = ui.DecodeBitmap(data)
		}
	}
	l.icons[key] = b
	return b
}

func (l *launcher) previewPane(c *ui.Context, p palette, r Result) {
	l.loadPreview(r)
	pv := l.previews[r.Path]
	ui.Column(c).Grow(1).Padding(18).Gap(14).Children(func() {
		thumb := ui.Box(c).Height(150).Radius(10).Border(1, p.sep).Center().Clip()
		thumb.Children(func() {
			if pv != nil && pv.thumb != nil && !pv.decoded {
				pv.bitmap, _ = ui.DecodeBitmap(pv.thumb)
				pv.decoded = true
			}
			if pv != nil && pv.bitmap != nil {
				ui.Image(c, pv.bitmap).Size(260, 140).Fit(ui.Contain)
			} else if b := l.icon(r.Path, 96); b != nil {
				ui.Image(c, b).Size(96, 96)
			}
		})
		ui.Text(c, r.Title).FontSize(14).FontWeight(600).TextColor(p.text).MaxLines(3)
		ui.Column(c).Children(func() {
			kind := ""
			if pv != nil {
				kind = pv.kind
			}
			if kind == "" && r.Kind == KindFolder {
				kind = "文件夹"
			}
			l.meta(c, p, "类型", kind)
			if pv != nil && pv.detail != "" {
				l.meta(c, p, "信息", pv.detail)
			}
			if r.Kind == KindFile {
				l.meta(c, p, "大小", formatSize(r.Size))
			}
			if !r.ModTime.IsZero() {
				l.meta(c, p, "修改时间", r.ModTime.Format("2006/01/02 15:04"))
			}
			l.meta(c, p, "位置", r.Subtitle)
		})
	})
}

func (l *launcher) meta(c *ui.Context, p palette, label, value string) {
	if value == "" {
		return
	}
	ui.Row(c).Padding(5, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.sep).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(p.faint).Width(64)
		ui.Text(c, value).FontSize(12).TextColor(p.muted).Grow(1).Shrink(1).MaxLines(2)
	})
}

func (l *launcher) kbd(c *ui.Context, p palette, keys ...string) {
	ui.Row(c).Gap(3).Children(func() {
		for _, k := range keys {
			ui.Text(c, k).FontSize(11).TextColor(p.muted).Padding(2, 5).Radius(5).Background(p.kbd).MinWidth(18).TextAlign(ui.Center)
		}
	})
}

func (l *launcher) footer(c *ui.Context, p palette) {
	ui.Row(c).Height(38).Padding(0, 12, 0, 16).Gap(14).AlignItems(ui.Center).Children(func() {
		if l.toast != "" {
			ui.Text(c, "✓ "+l.toast).FontSize(12).FontWeight(500).TextColor(p.text)
		} else {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				if b := brand(); b != nil {
					ui.Image(c, b).Size(16, 16)
				}
				ui.Text(c, "Spectra").FontSize(12).TextColor(p.faint)
			})
		}
		ui.Spacer(c)
		r, ok := l.current()
		if !ok {
			return
		}
		as := actionsFor(r)
		if l.menuOpen {
			hint(c, l, p, "执行", true, "↵")
			ui.Box(c).Size(1, 14).Background(p.sep)
			hint(c, l, p, "关闭", false, "esc")
			return
		}
		hint(c, l, p, as[0].title, true, as[0].keys...)
		if len(as) > 1 && as[1].key != 0 {
			ui.Box(c).Size(1, 14).Background(p.sep)
			hint(c, l, p, as[1].title, false, as[1].keys...)
		}
		ui.Box(c).Size(1, 14).Background(p.sep)
		hint(c, l, p, "操作", false, "⌘", "K")
	})
}

func hint(c *ui.Context, l *launcher, p palette, label string, primary bool, keys ...string) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		t := ui.Text(c, label).FontSize(12).TextColor(p.muted)
		if primary {
			t.TextColor(p.text).FontWeight(500)
		}
		l.kbd(c, p, keys...)
	})
}

// actionMenu shows what can be done with the chosen result, above the
// bottom right of the panel; ⌘K opens it.
func (l *launcher) actionMenu(c *ui.Context, p palette) {
	r, ok := l.current()
	if !ok {
		l.menuOpen = false
		return
	}
	as := filterActions(actionsFor(r), l.menuFilter)
	l.menuChosen = max(0, min(l.menuChosen, len(as)-1))
	ui.Overlay(c, func() {
		back := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Modal()
		back.Children(func() {
			menu := ui.Column(c).Absolute().Right(10).Bottom(46).Width(300).Padding(6).Radius(12).
				Background(p.menu).Border(1, p.sep).Shadow(0, 16, 48, 0, ui.RGBA(0, 0, 0, 0.35))
			menu.Children(func() {
				ui.Text(c, r.Title).FontSize(11).TextColor(p.faint).Padding(6, 8, 4, 8).SingleLine()
				for i, a := range as {
					item := ui.Row(c.Key(a.title)).Height(32).Padding(0, 8).Gap(10).Radius(7).AlignItems(ui.Center)
					if i == l.menuChosen {
						item.Background(p.selected)
					}
					item.Children(func() {
						color := p.text
						if a.danger {
							color = ui.Hex("#ff6b6b")
						}
						ui.Text(c, a.title).FontSize(13).TextColor(color).Grow(1)
						if len(a.keys) > 0 {
							l.kbd(c, p, a.keys...)
						}
					})
					if item.Clicked() {
						l.run(a)
					}
				}
				if len(as) == 0 {
					ui.Text(c, "没有匹配的操作").FontSize(12).TextColor(p.faint).Padding(8)
				}
				ui.Box(c).Height(1).Background(p.sep).Margin(6, 2, 0, 2)
				filter := ui.TextInputBase(c, &l.menuFilter).AutoFocus().FontSize(12).TextColor(p.text).
					Placeholder("筛选操作…").Label("筛选操作").Padding(8, 6, 2, 6)
				if filter.Changed() {
					l.menuChosen = 0
				}
				if filter.Submitted() && len(as) > 0 {
					l.run(as[l.menuChosen])
				}
			})
			if menu.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) || back.OverlayShortcut(ui.Cmd, ui.KeyK) {
				l.closeMenu()
			}
			if back.OverlayShortcut(0, ui.KeyDown) {
				l.menuChosen = min(l.menuChosen+1, len(as)-1)
			}
			if back.OverlayShortcut(0, ui.KeyUp) {
				l.menuChosen = max(l.menuChosen-1, 0)
			}
			if back.OverlayShortcut(0, ui.KeyEnter) && len(as) > 0 {
				l.run(as[l.menuChosen])
			}
		})
	})
}
