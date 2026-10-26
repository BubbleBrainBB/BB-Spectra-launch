package main

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo"
)

// row is a line of the list: a group's header, or a result.
type row struct {
	header string
	r      Result
	tail   string // the muted text at the end, as when a recent item was opened
}

// search runs the providers for the query and filter as they are.
func (l *launcher) search() {
	l.found = map[string][]Result{}
	l.moved = false
	gen, pending := l.engine.Run(Query{Text: l.query, Filter: l.filter}, l.deliver, l.post)
	l.gen = gen
	l.pending = map[string]bool{}
	for _, id := range pending {
		l.pending[id] = true
	}
	l.compose()
}

// deliver takes a provider's results, if they are of the latest query.
func (l *launcher) deliver(gen int, id string, rs []Result) {
	if gen != l.engine.gen {
		return
	}
	l.found[id] = rs
	if l.pending != nil {
		delete(l.pending, id)
	}
	if l.gen == gen { // asynchronous: synchronous ones compose in search
		l.compose()
	}
}

func (l *launcher) busy() bool { return len(l.pending) > 0 }

// compose builds the rows from what the providers found: the best match,
// then each group, ranked by how well they match and how often and lately
// the user opened them.
func (l *launcher) compose() {
	var key string
	if l.moved && l.chosen < len(l.rows) {
		key = l.rows[l.chosen].r.Key
	}
	l.rows = l.rows[:0]
	if l.query == "" {
		l.composeIdle()
	} else {
		l.composeResults()
	}
	l.chosen = -1
	for i, rw := range l.rows {
		if rw.header != "" {
			continue
		}
		if l.chosen < 0 || (key != "" && rw.r.Key == key) {
			l.chosen = i
		}
		if key == "" || rw.r.Key == key {
			break
		}
	}
	if l.chosen < 0 {
		l.chosen = 0
	}
	l.list.ScrollIntoView(l.chosen)
}

func (l *launcher) composeIdle() {
	now := l.now()
	var rows []row
	for _, u := range l.store.recent(12) {
		if l.filter != FilterAll && !kindInFilter(u.Kind, l.filter) {
			continue
		}
		info, err := os.Stat(u.Path)
		if err != nil {
			continue
		}
		r := Result{Key: u.Path, Kind: u.Kind, Title: u.Title, Path: u.Path, ModTime: info.ModTime(), Size: info.Size()}
		if u.Kind == KindApp {
			r.Subtitle = "应用"
			r.App = l.appByPath(u.Path)
		} else {
			r.Subtitle = tilde(filepath.Dir(u.Path))
		}
		rows = append(rows, row{r: r, tail: ago(now, u.Last)})
		if len(rows) == 8 {
			break
		}
	}
	if len(rows) > 0 {
		l.rows = append(l.rows, row{header: "最近"})
		l.rows = append(l.rows, rows...)
	}
}

func kindInFilter(k Kind, f Filter) bool {
	switch f {
	case FilterApps:
		return k == KindApp
	case FilterFolders:
		return k == KindFolder
	case FilterFiles, FilterDocs, FilterImages:
		return k == KindFile
	}
	return true
}

func (l *launcher) composeResults() {
	now := l.now()
	ranked := func(id string) []Result {
		rs := slices.Clone(l.found[id])
		for i := range rs {
			rs[i].Score += l.store.boost(rs[i].Key, now)
		}
		slices.SortStableFunc(rs, func(a, b Result) int {
			if c := cmp.Compare(b.Score, a.Score); c != 0 {
				return c
			}
			if c := cmp.Compare(len(a.Title), len(b.Title)); c != 0 {
				return c
			}
			return b.ModTime.Compare(a.ModTime)
		})
		return rs
	}
	apps, names := ranked("apps"), ranked("name")
	seen := map[string]bool{}
	for _, r := range names {
		seen[r.Key] = true
	}
	var contents []Result
	for _, r := range ranked("content") {
		if !seen[r.Key] {
			contents = append(contents, r)
		}
	}
	limit := func(n int) int {
		if l.filter != FilterAll {
			return 100
		}
		return n
	}
	add := func(header string, rs []Result, n int) {
		if len(rs) == 0 {
			return
		}
		l.rows = append(l.rows, row{header: header})
		for _, r := range rs[:min(n, len(rs))] {
			tail := ""
			if r.Source == "content" {
				tail = "内容匹配"
			}
			l.rows = append(l.rows, row{r: r, tail: tail})
		}
	}
	add("计算器", l.found["calc"], 1)

	// The best match is the top app or file, when its name starts with the
	// query or has a word that does.
	var best *Result
	for _, rs := range [][]Result{apps, names} {
		if len(rs) > 0 && rs[0].Rank >= 0 && rs[0].Rank <= 1 && (best == nil || rs[0].Score > best.Score) {
			best = &rs[0]
		}
	}
	if best != nil && l.filter == FilterAll {
		b := *best
		add("最佳匹配", []Result{b}, 1)
		apps, names = slicesDeleteKey(apps, b.Key), slicesDeleteKey(names, b.Key)
	}
	add("应用", apps, limit(5))
	add("文件", names, limit(8))
	add("内容包含", contents, limit(6))
	add("网页搜索", l.found["web"], 1)
}

func (l *launcher) appByPath(path string) *mygo.InstalledApp {
	for i := range l.apps.apps {
		if l.apps.apps[i].Path == path {
			return &l.apps.apps[i]
		}
	}
	return nil
}

// ago says how long ago t was, shortly.
func ago(now, t time.Time) string {
	switch d := now.Sub(t); {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	case d < 48*time.Hour:
		return "昨天"
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	}
	return t.Format("2006/01/02")
}

// selectable reports whether row i is a result.
func (l *launcher) selectable(i int) bool {
	return i >= 0 && i < len(l.rows) && l.rows[i].header == ""
}

// move chooses the result delta results away, skipping headers.
func (l *launcher) move(delta int) {
	i := l.chosen
	for {
		i += delta
		if i < 0 || i >= len(l.rows) {
			return
		}
		if l.selectable(i) {
			l.choose(i)
			return
		}
	}
}

func (l *launcher) choose(i int) {
	if !l.selectable(i) {
		return
	}
	l.chosen, l.moved = i, true
	// Show the header above the first result too.
	if i > 0 && l.rows[i-1].header != "" {
		l.list.ScrollIntoView(i - 1)
	}
	l.list.ScrollIntoView(i)
}

// current returns the chosen result.
func (l *launcher) current() (Result, bool) {
	if !l.selectable(l.chosen) {
		return Result{}, false
	}
	return l.rows[l.chosen].r, true
}

// nth returns the nth result, from 0.
func (l *launcher) nth(n int) (int, bool) {
	for i := range l.rows {
		if l.selectable(i) {
			if n == 0 {
				return i, true
			}
			n--
		}
	}
	return 0, false
}
