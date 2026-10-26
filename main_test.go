package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestMatch(t *testing.T) {
	for _, tc := range []struct {
		name, q string
		rank    int
		match   [][2]int
	}{
		{"Calendar", "cal", 0, [][2]int{{0, 3}}},
		{"Visual Studio Code", "code", 1, [][2]int{{14, 18}}},
		{"Xcode", "code", 2, [][2]int{{1, 5}}},
		{"Visual Studio Code", "vsc", 3, [][2]int{{0, 1}, {7, 8}, {14, 15}}},
		{"sales-report-q3.xlsx", "report", 1, [][2]int{{6, 12}}},
		{"AnnualReport.pdf", "report", 1, [][2]int{{6, 12}}}, // camelCase word
		{"2026 Q3 财报.pdf", "财报", 1, [][2]int{{8, 10}}},
		{"Annual Report 2026.pdf", "annual 2026", 1, [][2]int{{0, 6}, {14, 18}}},
		{"Calendar", "zzz", -1, nil},
		{"Notes", "", 0, nil},
	} {
		rank, m := match(tc.name, tc.q)
		if rank != tc.rank || !slices.Equal(m, tc.match) {
			t.Errorf("match(%q, %q) = %d %v, want %d %v", tc.name, tc.q, rank, m, tc.rank, tc.match)
		}
	}
}

func TestEvaluate(t *testing.T) {
	for in, want := range map[string]string{
		"1+2":           "3",
		"(1280-96)*0.8": "947.2",
		"2^3^2":         "512",
		"10 % 3":        "1",
		"-3 + 5":        "2",
		"0.1+0.2":       "0.3",
		"7÷2":           "3.5",
		"3×（2+1）":       "9",
		"1,000*2":       "2000",
	} {
		if !looksLikeMath(in) {
			t.Errorf("%q does not look like math", in)
			continue
		}
		v, err := evaluate(in)
		if err != nil || formatNumber(v) != want {
			t.Errorf("evaluate(%q) = %v %v, want %s", in, formatNumber(v), err, want)
		}
	}
	for _, in := range []string{"report", "2026", "-5", "q3", "1+", "1/0", "(1+2"} {
		if looksLikeMath(in) {
			if _, err := evaluate(in); err == nil {
				t.Errorf("%q evaluates", in)
			}
		}
	}
	if got := prettyExpr("(1280-96)*0.8"); got != "(1280 − 96) × 0.8" {
		t.Errorf("prettyExpr = %q", got)
	}
}

func TestSpotlightQuery(t *testing.T) {
	name, content := &spotlightProvider{}, &spotlightProvider{content: true}
	for _, tc := range []struct {
		p    *spotlightProvider
		q    Query
		want string
	}{
		{name, Query{Text: "report"}, `kMDItemFSName == "*report*"cd`},
		{name, Query{Text: "q3"}, `kMDItemFSName == "q3*"cdw`},
		{name, Query{Text: "annual q3"}, `kMDItemFSName == "*annual*"cd && kMDItemFSName == "q3*"cdw`},
		{name, Query{Text: `a"b*c`}, `kMDItemFSName == "*a\"b\*c*"cd`},
		{name, Query{Text: "png", Filter: FilterImages}, `kMDItemFSName == "*png*"cd && kMDItemContentTypeTree == "public.image"`},
		{name, Query{Text: "src", Filter: FilterFolders}, `kMDItemFSName == "*src*"cd && kMDItemContentType == "public.folder"`},
		{name, Query{Text: "x", Filter: FilterApps}, ``},
		{name, Query{Text: "  "}, ``},
		{content, Query{Text: "invoice"}, `kMDItemTextContent == "invoice*"cdw`},
		{content, Query{Text: "ab"}, ``},
		{content, Query{Text: "invoice", Filter: FilterImages}, ``},
	} {
		if got := tc.p.query(tc.q); got != tc.want {
			t.Errorf("%s %+v:\n got %s\nwant %s", tc.p.ID(), tc.q, got, tc.want)
		}
	}
}

func TestSkipPath(t *testing.T) {
	h := home()
	for p, skip := range map[string]bool{
		h + "/Documents/report.pdf":             false,
		h + "/Library/Caches/x":                 true,
		h + "/code/.git/HEAD":                   true,
		h + "/code/app/node_modules/x/index.js": true,
		h + "/Applications/Foo.app":             true,
		h + "/Applications/Foo.app/Contents/x":  true,
	} {
		if skipPath(p) != skip {
			t.Errorf("skipPath(%q) = %v", p, !skip)
		}
	}
}

// fakeProvider answers with fixed results, after a delay.
type fakeProvider struct {
	id    string
	delay time.Duration
	rs    func(q Query) []Result
}

func (f fakeProvider) ID() string           { return f.id }
func (f fakeProvider) Delay() time.Duration { return f.delay }
func (f fakeProvider) Search(_ context.Context, q Query) ([]Result, error) {
	return f.rs(q), nil
}

func file(name, dir string, source string) Result {
	r := Result{Key: dir + "/" + name, Kind: KindFile, Title: name, Subtitle: dir, Path: dir + "/" + name, Source: source, Rank: -1}
	if source == "name" {
		r.Rank, r.Match = 0, nil
	}
	return r
}

// testLauncher is a launcher whose async providers run at once, on the
// test's goroutine, so the view sees their results in the next frame.
func testLauncher(t *testing.T) (*launcher, func()) {
	t.Helper()
	l := newLauncher(openStore(""), nil)
	queue := make(chan func(), 64)
	l.post = func(fn func()) { queue <- fn }
	l.apps.apps = []mygo.InstalledApp{
		{Name: "Safari", Path: "/Applications/Safari.app"},
		{Name: "System Report", Path: "/System/Applications/Utilities/System Information.app"},
		{Name: "Notes", Path: "/System/Applications/Notes.app"},
	}
	names := func(q Query) []Result {
		var rs []Result
		for _, f := range []Result{
			file("Annual Report 2026.pdf", "~/Documents/Work", "name"),
			file("sales-report-q3.xlsx", "~/Documents/Work", "name"),
			file("weekly report.md", "~/Notes", "name"),
		} {
			if rank, m := match(f.Title, q.Text); rank >= 0 && q.Filter != FilterApps {
				f.Rank, f.Match, f.Score = rank, m, rankScore[rank]
				rs = append(rs, f)
			}
		}
		return rs
	}
	contents := func(q Query) []Result {
		if q.Text == "" || q.Filter != FilterAll {
			return nil
		}
		return []Result{file("Annual Report 2026.pdf", "~/Documents/Work", "content"), file("minutes.txt", "~/Notes", "content")}
	}
	l.engine = NewEngine(calcProvider{}, l.apps,
		fakeProvider{"name", time.Millisecond, names},
		fakeProvider{"content", time.Millisecond, contents},
		webProvider{engine: &l.settings.SearchEngine})
	flush := func() {
		deadline := time.Now().Add(time.Second)
		for l.busy() && time.Now().Before(deadline) {
			select {
			case fn := <-queue:
				fn()
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	return l, flush
}

func titles(l *launcher) []string {
	var out []string
	for _, rw := range l.rows {
		if rw.header != "" {
			out = append(out, "# "+rw.header)
		} else {
			out = append(out, rw.r.Title)
		}
	}
	return out
}

func TestCompose(t *testing.T) {
	l, flush := testLauncher(t)
	l.query = "report"
	l.search()
	flush()
	// Apps rank a little above files that match as well; shorter names
	// first.
	want := []string{
		"# 最佳匹配", "System Report",
		"# 文件", "weekly report.md", "sales-report-q3.xlsx", "Annual Report 2026.pdf",
		"# 内容包含", "minutes.txt", // the PDF found by name shows once
		"# 网页搜索", "用 Google 搜索 “report”",
	}
	if got := titles(l); !slices.Equal(got, want) {
		t.Errorf("rows\n got %q\nwant %q", got, want)
	}
	if r, _ := l.current(); r.Title != "System Report" {
		t.Errorf("chosen %q, want the best match", r.Title)
	}

	// What the user opened often ranks first.
	pdf := l.rows[5].r
	for range 5 {
		l.store.record(pdf, time.Now())
	}
	l.search()
	flush()
	if got := titles(l)[:4]; !slices.Equal(got, []string{"# 最佳匹配", "Annual Report 2026.pdf", "# 应用", "System Report"}) {
		t.Errorf("after use, rows start %q", got)
	}

	// The filter keeps one kind.
	l.filter = FilterApps
	l.search()
	flush()
	if got := titles(l); !slices.Equal(got, []string{"# 应用", "System Report"}) {
		t.Errorf("apps only: %q", got)
	}

	l.filter, l.query = FilterAll, "(1280-96)*0.8"
	l.search()
	flush()
	if got := titles(l); len(got) < 2 || got[1] != "947.2" {
		t.Errorf("calculation: %q", got)
	}
}

func TestKeepChoiceAsResultsCome(t *testing.T) {
	l, flush := testLauncher(t)
	l.query = "report"
	l.search()
	l.move(1) // the web search, before files come
	flush()
	if r, _ := l.current(); r.Kind != KindWeb {
		t.Errorf("chosen %q after files came, want the web search", r.Title)
	}
}

func TestView(t *testing.T) {
	l, flush := testLauncher(t)
	l.search()
	tt := ui.NewTester(l.view, panelWidth, panelHeight)
	if !tt.HasText("输入以搜索应用、文件和文件内容") {
		t.Errorf("idle texts %q", tt.Texts())
	}
	if err := tt.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	tt.Type("report")
	flush()
	tt.Frame()
	for _, s := range []string{"最佳匹配", "System Report", "内容包含", "打开应用", "操作"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}

	tt.Key(0, ui.KeyDown) // over the header, to the first file
	if r, _ := l.current(); r.Title != "weekly report.md" {
		t.Errorf("Down chose %q", r.Title)
	}
	for _, s := range []string{"大小", "位置", "在 Finder 中显示"} { // the file's preview and actions
		if !tt.HasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyUp) // stays on the first
	if r, _ := l.current(); r.Title != "System Report" {
		t.Errorf("Up chose %q", r.Title)
	}
	tt.Key(0, ui.KeyDown)

	// ⌘K lists the file's actions; typing filters them; Escape closes it.
	tt.Key(ui.Cmd, ui.KeyK)
	if !l.menuOpen || !tt.HasText("移到废纸篓") || !tt.HasText("拷贝路径") {
		t.Fatalf("menu %v, texts %q", l.menuOpen, tt.Texts())
	}
	tt.Type("终端")
	if tt.HasText("拷贝路径") || !tt.HasText("在终端中打开") {
		t.Errorf("filtered menu %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	if l.menuOpen {
		t.Error("Escape left the menu open")
	}
	if l.query != "report" {
		t.Errorf("query %q after closing the menu", l.query)
	}
	tt.Type("s") // the search field has the keyboard again
	if l.query != "reports" {
		t.Errorf("typing after the menu: query %q", l.query)
	}
	tt.Key(0, ui.KeyBackspace)

	// Tab moves to the next category.
	tt.Key(0, ui.KeyTab)
	if l.filter != FilterApps {
		t.Errorf("Tab: filter %d", l.filter)
	}
	tt.Key(ui.Shift, ui.KeyTab)
	if l.filter != FilterAll {
		t.Errorf("Shift-Tab: filter %d", l.filter)
	}
	if err := tt.Click("图片"); err != nil {
		t.Fatal(err)
	}
	if l.filter != FilterImages {
		t.Errorf("click: filter %d", l.filter)
	}

	// Escape clears the query first.
	tt.Key(0, ui.KeyEscape)
	if l.query != "" {
		t.Errorf("Escape left %q", l.query)
	}
	if strings.Contains(strings.Join(tt.Texts(), " "), "System Report") {
		t.Error("results stay after clearing")
	}
	// The search field keeps the keyboard.
	if err := tt.Click("全部"); err != nil {
		t.Fatal(err)
	}
	tt.Type("1+2")
	if l.query != "1+2" || !tt.HasText("3") {
		t.Errorf("typing after Escape: query %q, texts %q", l.query, tt.Texts())
	}
}

func TestTrayIcon(t *testing.T) {
	im, err := png.Decode(bytes.NewReader(trayIcon()))
	if err != nil {
		t.Fatal(err)
	}
	if b := im.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
		t.Fatalf("size %v", b)
	}
	alpha := func(x, y float64) uint32 { // in the 24-unit grid of the SVG
		_, _, _, a := im.At(int(x*32/24), int(y*32/24)).RGBA()
		return a >> 8
	}
	for _, tc := range []struct {
		name   string
		x, y   float64
		opaque bool
	}{
		{"corner", 0.5, 0.5, false},
		{"inside the prism", 9.5, 11.5, false},
		{"the prism's base", 9.5, 14.7, true},
		{"the spectrum", 20, 12.5, true},
		{"the beam", 3, 12.3, true},
	} {
		if got := alpha(tc.x, tc.y) > 128; got != tc.opaque {
			t.Errorf("%s: alpha %d", tc.name, alpha(tc.x, tc.y))
		}
	}
	if os.Getenv("TRAY_OUT") != "" {
		big := image.NewNRGBA(image.Rect(0, 0, 320, 320))
		for y := 0; y < 320; y++ {
			for x := 0; x < 320; x++ {
				_, _, _, a := im.At(x/10, y/10).RGBA()
				v := uint8(255 - a>>8)
				big.SetNRGBA(x, y, color.NRGBA{v, v, v, 255})
			}
		}
		f, _ := os.Create(os.Getenv("TRAY_OUT"))
		png.Encode(f, big)
		f.Close()
	}
}

func TestEnterOpensTheChosenResult(t *testing.T) {
	l, flush := testLauncher(t)
	var did []string
	l.perform = func(a action, r Result) { did = append(did, a.title+" "+r.Title) }
	l.search()
	tt := ui.NewTester(l.view, panelWidth, panelHeight)
	if err := tt.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	tt.Type("report")
	flush()
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyDown)
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Key(ui.Cmd, ui.KeyK) // in the menu, Enter does the action chosen there
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if l.menuOpen {
		t.Error("the menu stays open after its action")
	}
	want := []string{"打开应用 System Report", "在 Finder 中显示 weekly report.md", "拷贝路径 weekly report.md"}
	if !slices.Equal(did, want) {
		t.Errorf("did %q, want %q", did, want)
	}
}
