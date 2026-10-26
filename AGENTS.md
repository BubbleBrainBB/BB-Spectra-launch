# Spectra

Spectra (bundle id `com.bubblebrain.spectra`; the Go module and folder are still named `launcher`) is a launcher for macOS that finds apps, files and their content from one panel, in Go with MyGo native UI (`github.com/egoist/mygo` v0.6.4). Design mockup: `design/mockup.html`; icon concepts: `design/icons-f.html` (F1 chosen, exported to `resources/icon.png`; menu bar glyph drawn in `trayicon.go`).

## Environment

- Go 1.27.3 is installed in `~/.local/go` (PATH set in `~/.zprofile`).
- proxy.golang.org and github.com are only reachable through the local proxy at `127.0.0.1:7890` (`HTTPS_PROXY`/`HTTP_PROXY`/`ALL_PROXY` in `~/.zprofile`; GOPROXY is the default). Non-login shells don't read `~/.zprofile`: pass `HTTPS_PROXY=http://127.0.0.1:7890` explicitly when downloading modules.
- MyGo source/docs: `go list -m -f '{{.Dir}}' github.com/egoist/mygo` (docs in `docs/`, examples in `examples/`). Check APIs against this pinned version: some documented options (e.g. `CornerRadius`, `VibrancyGlass`) are newer than v0.6.4.

## Commands

- Run with live reload: `go tool mygo dev`
- Test: `CGO_ENABLED=0 go test -race ./...`
- Vet: `go tool mygo vet . && go vet ./...`
- Package: `go tool mygo build`

## Layout

- `search.go`: `Provider` interface, `Engine` (runs providers, cancels stale queries), `match` ranking.
- `providers.go`: apps, Spotlight (`mdfind`, by name and by content), calculator, web fallback. New sources (e.g. embeddings) are new `Provider`s.
- `results.go`: composing groups (best match, apps, files, content, web), selection.
- `view.go`: the panel UI; `actions.go`: ⌘K actions; `preview.go`: side preview (mdls + qlmanage thumbnails); `settings.go`; `store.go` (usage + settings JSON in the user data dir).
- UI tests drive the view with `ui.NewTester`; `Tester.Image()` renders PNGs for visual checks.
