package main

import (
	"bufio"
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// appsProvider matches the installed apps by name, in memory.
type appsProvider struct {
	apps []mygo.InstalledApp // set on the UI thread
}

func (*appsProvider) ID() string           { return "apps" }
func (*appsProvider) Delay() time.Duration { return 0 }

func (p *appsProvider) Search(_ context.Context, q Query) ([]Result, error) {
	if strings.TrimSpace(q.Text) == "" || (q.Filter != FilterAll && q.Filter != FilterApps) {
		return nil, nil
	}
	var rs []Result
	for i := range p.apps {
		app := &p.apps[i]
		rank, m := match(app.Name, q.Text)
		if rank < 0 {
			continue
		}
		rs = append(rs, Result{
			Key: app.Path, Kind: KindApp, Title: app.Name, Subtitle: "应用", Path: app.Path,
			App: app, Match: m, Rank: rank, Score: rankScore[rank] + 10, Source: "apps",
		})
	}
	return rs, nil
}

// spotlightProvider finds files with mdfind, from the Spotlight index:
// by name, or by the words in them (content).
type spotlightProvider struct {
	content bool
	limit   int
	root    string // only files under it; the home directory by default
}

func (p *spotlightProvider) ID() string {
	if p.content {
		return "content"
	}
	return "name"
}

func (p *spotlightProvider) Delay() time.Duration {
	if p.content {
		return 250 * time.Millisecond
	}
	return 100 * time.Millisecond
}

func (p *spotlightProvider) Search(ctx context.Context, q Query) ([]Result, error) {
	expr := p.query(q)
	if expr == "" || (p.content && looksLikeMath(q.Text)) {
		return nil, nil
	}
	root := p.root
	if root == "" {
		root = home()
	}
	cmd := exec.CommandContext(ctx, "mdfind", "-onlyin", root, expr)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	var rs []Result
	sc := bufio.NewScanner(out)
	for read := 0; sc.Scan() && len(rs) < p.limit && read < p.limit*10; read++ {
		path := sc.Text()
		if skipPath(path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		kind := fileKind(path, info)
		if (q.Filter == FilterFolders) != (kind == KindFolder) && q.Filter != FilterAll {
			continue
		}
		name := filepath.Base(path)
		r := Result{
			Key: path, Kind: kind, Title: name, Subtitle: tilde(filepath.Dir(path)), Path: path,
			Rank: -1, Source: p.ID(), ModTime: info.ModTime(), Size: info.Size(),
		}
		if rank, m := match(name, q.Text); rank >= 0 {
			r.Rank, r.Match, r.Score = rank, m, rankScore[rank]
		}
		if time.Since(r.ModTime) < 7*24*time.Hour {
			r.Score += 5
		}
		rs = append(rs, r)
	}
	return rs, ctx.Err()
}

// query builds the mdfind query: every word of the text in the file's
// name (short words as the start of a word in it, which is much faster
// than a substring), or in its content, within the type of the filter.
func (p *spotlightProvider) query(q Query) string {
	words := strings.Fields(q.Text)
	if len(words) == 0 {
		return ""
	}
	switch q.Filter {
	case FilterApps:
		return ""
	case FilterFolders, FilterImages:
		if p.content {
			return ""
		}
	}
	var parts []string
	for _, w := range words {
		w = escapeMD(w)
		switch {
		case p.content:
			if len([]rune(w)) < 3 && len(words) == 1 {
				return ""
			}
			parts = append(parts, `kMDItemTextContent == "`+w+`*"cdw`)
		case len([]rune(w)) < 3:
			parts = append(parts, `kMDItemFSName == "`+w+`*"cdw`)
		default:
			parts = append(parts, `kMDItemFSName == "*`+w+`*"cd`)
		}
	}
	if t := typeClause(q.Filter); t != "" {
		parts = append(parts, t)
	}
	return strings.Join(parts, " && ")
}

func typeClause(f Filter) string {
	switch f {
	case FilterFiles:
		return `kMDItemContentType != "public.folder"`
	case FilterFolders:
		return `kMDItemContentType == "public.folder"`
	case FilterImages:
		return `kMDItemContentTypeTree == "public.image"`
	case FilterDocs:
		return `(kMDItemContentTypeTree == "com.adobe.pdf" || kMDItemContentTypeTree == "public.text" || kMDItemContentTypeTree == "public.composite-content" || kMDItemContentTypeTree == "public.presentation" || kMDItemContentTypeTree == "public.spreadsheet")`
	}
	return ""
}

// escapeMD escapes what is special in a quoted mdfind value.
func escapeMD(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, `*`, `\*`, `'`, `\'`).Replace(s)
}

// skipPath leaves out what a person rarely means: hidden files, the
// Library, the insides of apps and packages, dependencies, and apps,
// which the apps provider finds.
func skipPath(p string) bool {
	rel := strings.TrimPrefix(p, home())
	switch {
	case strings.Contains(p, "/."),
		strings.HasPrefix(rel, "/Library/"),
		strings.HasPrefix(rel, "/go/pkg/"),
		strings.HasSuffix(p, ".app"),
		strings.Contains(p, ".app/"),
		strings.Contains(p, "/node_modules/"),
		strings.Contains(p, ".photoslibrary/"),
		strings.Contains(p, ".xcodeproj/"):
		return true
	}
	return false
}

// calcProvider answers arithmetic.
type calcProvider struct{}

func (calcProvider) ID() string           { return "calc" }
func (calcProvider) Delay() time.Duration { return 0 }

func (calcProvider) Search(_ context.Context, q Query) ([]Result, error) {
	if q.Filter != FilterAll || !looksLikeMath(q.Text) {
		return nil, nil
	}
	v, err := evaluate(q.Text)
	if err != nil {
		return nil, nil
	}
	s := formatNumber(v)
	return []Result{{Key: "calc:", Kind: KindCalc, Title: s, Subtitle: prettyExpr(q.Text), Value: s, Rank: -1, Source: "calc"}}, nil
}

// Search engines for the web fallback, by the name settings show.
var searchEngines = map[string]string{
	"Google":     "https://www.google.com/search?q=",
	"Bing":       "https://www.bing.com/search?q=",
	"百度":         "https://www.baidu.com/s?wd=",
	"DuckDuckGo": "https://duckduckgo.com/?q=",
}

var searchEngineNames = []string{"Google", "Bing", "百度", "DuckDuckGo"}

// webProvider offers to search the web for the query, the row that is
// there when nothing else is.
type webProvider struct {
	engine *string // the setting
}

func (webProvider) ID() string           { return "web" }
func (webProvider) Delay() time.Duration { return 0 }

func (p webProvider) Search(_ context.Context, q Query) ([]Result, error) {
	text := strings.TrimSpace(q.Text)
	if text == "" || q.Filter != FilterAll {
		return nil, nil
	}
	name := *p.engine
	base, ok := searchEngines[name]
	if !ok {
		name, base = "Google", searchEngines["Google"]
	}
	return []Result{{
		Key: "web:", Kind: KindWeb, Title: "用 " + name + " 搜索 “" + text + "”",
		Value: base + url.QueryEscape(text), Rank: -1, Source: "web",
	}}, nil
}
