package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/egoist/mygo"
)

// Kind is what a result is, which decides its icon, its preview and its
// actions.
type Kind int

const (
	KindApp Kind = iota
	KindFile
	KindFolder
	KindCalc
	KindWeb
)

// Filter is the category chip the user chose.
type Filter int

const (
	FilterAll Filter = iota
	FilterApps
	FilterFiles
	FilterFolders
	FilterDocs
	FilterImages
)

var filterLabels = []string{"全部", "应用", "文件", "文件夹", "文档", "图片"}

// Query is what providers search for.
type Query struct {
	Text   string
	Filter Filter
}

// Result is one row of the list.
type Result struct {
	// Key identifies the result across providers, its path for files and
	// apps, so the same file found twice shows once.
	Key      string
	Kind     Kind
	Title    string
	Subtitle string
	Path     string
	App      *mygo.InstalledApp
	// Value is the answer of a calculation or the URL of a web search.
	Value string
	// Match holds the rune ranges of Title the query matched, to highlight.
	Match [][2]int
	// Rank is how well Title matches: 0 a prefix, 1 a word, 2 a substring,
	// 3 the letters in order; -1 no match on the name (content matches).
	Rank    int
	Score   int
	Source  string
	ModTime time.Time
	Size    int64
}

// Provider is a source of results. Providers without a Delay run on the UI
// thread as the query changes, so they must be fast (apps, calculator);
// the others run on goroutines after the query has rested for Delay, and
// are cancelled when it changes again (Spotlight, later embeddings).
type Provider interface {
	ID() string
	Delay() time.Duration
	Search(ctx context.Context, q Query) ([]Result, error)
}

// Engine runs providers for each query and hands their results back
// tagged with the generation of the query, so stale ones can be dropped.
type Engine struct {
	providers []Provider
	cancel    context.CancelFunc
	gen       int
}

func NewEngine(ps ...Provider) *Engine { return &Engine{providers: ps} }

// Run starts a search. deliver is called for each provider, with nil
// results on errors: synchronously for providers without a delay, and
// through post (Window.Update) for the others. It returns the generation
// and the IDs of the providers still running.
func (e *Engine) Run(q Query, deliver func(gen int, id string, rs []Result), post func(func())) (int, []string) {
	if e.cancel != nil {
		e.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.gen++
	gen := e.gen
	var pending []string
	for _, p := range e.providers {
		if p.Delay() == 0 {
			rs, _ := p.Search(ctx, q)
			deliver(gen, p.ID(), rs)
			continue
		}
		pending = append(pending, p.ID())
		go func(p Provider) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.Delay()):
			}
			rs, _ := p.Search(ctx, q)
			if ctx.Err() != nil {
				return
			}
			post(func() { deliver(gen, p.ID(), rs) })
		}(p)
	}
	return gen, pending
}

// Stop cancels the running search.
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
}

// match reports how name matches q, case-insensitively: a prefix (0),
// the start of a word (1), a substring (2), or q's letters in order (3),
// with the rune ranges of name that matched. A query of several words
// matches when every word does, ranked by the worst. -1 is no match.
func match(name, q string) (int, [][2]int) {
	q = strings.TrimSpace(q)
	if q == "" {
		return 0, nil
	}
	words := strings.Fields(q)
	if len(words) > 1 {
		if r, m := matchWord(name, q); r >= 0 && r < 3 {
			return r, m
		}
		worst, all := 0, [][2]int{}
		for _, w := range words {
			r, m := matchWord(name, w)
			if r < 0 || r == 3 {
				return -1, nil
			}
			worst = max(worst, r)
			all = append(all, m...)
		}
		return max(worst, 1), all
	}
	return matchWord(name, q)
}

func lowerRunes(s string) []rune {
	rs := []rune(s)
	for i, r := range rs {
		rs[i] = unicode.ToLower(r)
	}
	return rs
}

func indexRunes(s, sub []rune, from int) int {
	for i := from; i+len(sub) <= len(s); i++ {
		ok := true
		for j := range sub {
			if s[i+j] != sub[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

func isWordStart(rs []rune, i int) bool {
	if i == 0 {
		return true
	}
	p, r := rs[i-1], rs[i]
	if !unicode.IsLetter(p) && !unicode.IsDigit(p) {
		return true
	}
	return unicode.IsLower(p) && unicode.IsUpper(r)
}

func matchWord(name, q string) (int, [][2]int) {
	orig := []rune(name)
	n, w := lowerRunes(name), lowerRunes(q)
	if len(w) == 0 {
		return 0, nil
	}
	first := -1
	for i := indexRunes(n, w, 0); i >= 0; i = indexRunes(n, w, i+1) {
		if first < 0 {
			first = i
		}
		if i == 0 {
			return 0, [][2]int{{0, len(w)}}
		}
		if isWordStart(orig, i) {
			return 1, [][2]int{{i, i + len(w)}}
		}
	}
	if first >= 0 {
		return 2, [][2]int{{first, first + len(w)}}
	}
	// The letters in order: at the starts of words first, as "vsc" is the
	// initials of Visual Studio Code, else wherever they are.
	if ranges := inOrder(orig, n, w, true); ranges != nil {
		return 3, ranges
	}
	if ranges := inOrder(orig, n, w, false); ranges != nil {
		return 3, ranges
	}
	return -1, nil
}

func inOrder(orig, n, w []rune, wordStarts bool) [][2]int {
	var ranges [][2]int
	j := 0
	for i := 0; i < len(n) && j < len(w); i++ {
		if n[i] != w[j] || (wordStarts && !isWordStart(orig, i)) {
			continue
		}
		if len(ranges) > 0 && ranges[len(ranges)-1][1] == i {
			ranges[len(ranges)-1][1] = i + 1
		} else {
			ranges = append(ranges, [2]int{i, i + 1})
		}
		j++
	}
	if j < len(w) {
		return nil
	}
	return ranges
}

var rankScore = [...]int{100, 80, 60, 30}

// home is the user's home directory, for paths shown as ~/….
var home = sync.OnceValue(func() string {
	h, _ := os.UserHomeDir()
	return h
})

// tilde shortens a path in the home directory to ~/….
func tilde(p string) string {
	h := home()
	if h != "" && (p == h || strings.HasPrefix(p, h+"/")) {
		return "~" + p[len(h):]
	}
	return p
}

// fileKind tells a folder from a file: a directory with an extension is a
// package (a Keynote file, a bundle), which shows as a file.
func fileKind(path string, info os.FileInfo) Kind {
	if info.IsDir() && filepath.Ext(path) == "" {
		return KindFolder
	}
	return KindFile
}
