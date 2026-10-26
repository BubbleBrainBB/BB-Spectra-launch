package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// preview is what the side of the panel shows about a file or folder.
type preview struct {
	kind    string // as Finder says it: "PDF document"
	detail  string // pages, pixels, or items in a folder
	thumb   []byte // a Quick Look thumbnail, PNG
	bitmap  *ui.Bitmap
	decoded bool
	loaded  bool
}

// loadPreview reads what the preview shows on a goroutine and hands it to
// the view, for the result chosen; a later choice cancels it.
func (l *launcher) loadPreview(r Result) {
	if r.Kind != KindFile && r.Kind != KindFolder {
		return
	}
	if _, ok := l.previews[r.Path]; ok {
		return
	}
	if l.previewCancel != nil {
		l.previewCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.previewCancel = cancel
	path, kind := r.Path, r.Kind
	go func() {
		// Wait for the choice to rest, as while holding Down.
		select {
		case <-ctx.Done():
			return
		case <-time.After(80 * time.Millisecond):
		}
		p := readPreview(ctx, path, kind)
		if ctx.Err() != nil {
			return
		}
		l.post(func() {
			if len(l.previews) > 200 {
				clear(l.previews)
			}
			l.previews[path] = p
		})
	}()
}

func readPreview(ctx context.Context, path string, kind Kind) *preview {
	p := &preview{loaded: true}
	out, _ := exec.CommandContext(ctx, "mdls", "-raw",
		"-name", "kMDItemKind", "-name", "kMDItemNumberOfPages",
		"-name", "kMDItemPixelWidth", "-name", "kMDItemPixelHeight",
		"-name", "kMDItemDurationSeconds", path).Output()
	v := strings.Split(string(out), "\x00")
	for len(v) < 5 {
		v = append(v, "(null)")
	}
	val := func(i int) string {
		if v[i] == "(null)" {
			return ""
		}
		return v[i]
	}
	p.kind = val(0)
	switch {
	case kind == KindFolder:
		if entries, err := os.ReadDir(path); err == nil {
			n := 0
			for _, e := range entries {
				if !strings.HasPrefix(e.Name(), ".") {
					n++
				}
			}
			p.detail = fmt.Sprintf("%d 项", n)
		}
	case val(1) != "":
		p.detail = val(1) + " 页"
	case val(2) != "" && val(3) != "":
		p.detail = val(2) + " × " + val(3) + " 像素"
	case val(4) != "":
		var secs float64
		fmt.Sscan(val(4), &secs)
		d := time.Duration(secs) * time.Second
		p.detail = fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
	}
	if kind == KindFile {
		p.thumb = thumbnail(ctx, path)
	}
	return p
}

// thumbnail asks Quick Look for a picture of the file, as Finder shows:
// a page of a PDF, a photo, the first slide.
func thumbnail(ctx context.Context, path string) []byte {
	dir, err := os.MkdirTemp("", "spectra-thumb")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "qlmanage", "-t", "-s", "400", "-o", dir, path)
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Run(); err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.Base(path)+".png"))
	if err != nil || !bytes.HasPrefix(data, []byte("\x89PNG")) {
		return nil
	}
	return data
}

// formatSize writes a size as Finder does, in powers of 1000.
func formatSize(n int64) string {
	units := []string{"字节", "KB", "MB", "GB", "TB"}
	f, i := float64(n), 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d %s", n, units[0])
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}
