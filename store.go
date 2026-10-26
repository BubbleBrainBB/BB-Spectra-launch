package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Usage is what the launcher remembers of an item the user opened through
// it, which ranks it higher and lists it under Recent.
type Usage struct {
	Path  string    `json:"path"`
	Title string    `json:"title"`
	Kind  Kind      `json:"kind"`
	Count int       `json:"count"`
	Last  time.Time `json:"last"`
}

// Settings are the user's choices.
type Settings struct {
	Shortcut     string `json:"shortcut"`
	SearchEngine string `json:"searchEngine"`
}

func defaultSettings() Settings {
	return Settings{Shortcut: "Alt+Space", SearchEngine: "Google"}
}

// store keeps usage and settings in JSON files of a directory, the app's
// user data directory. It is used on the UI thread; files are written on
// goroutines from copies.
type store struct {
	dir   string
	usage map[string]*Usage
}

func openStore(dir string) *store {
	s := &store{dir: dir, usage: map[string]*Usage{}}
	if dir == "" {
		return s
	}
	var list []*Usage
	if readJSON(filepath.Join(dir, "usage.json"), &list) {
		for _, u := range list {
			s.usage[u.Path] = u
		}
	}
	return s
}

func (s *store) settings() Settings {
	st := defaultSettings()
	if s.dir != "" {
		readJSON(filepath.Join(s.dir, "settings.json"), &st)
	}
	return st
}

func (s *store) saveSettings(st Settings) {
	if s.dir != "" {
		go writeJSON(filepath.Join(s.dir, "settings.json"), st)
	}
}

// record counts an opening of r.
func (s *store) record(r Result, now time.Time) {
	u := s.usage[r.Key]
	if u == nil {
		u = &Usage{Path: r.Key}
		s.usage[r.Key] = u
	}
	u.Title, u.Kind, u.Last = r.Title, r.Kind, now
	u.Count++
	if s.dir == "" {
		return
	}
	list := make([]Usage, 0, len(s.usage))
	for _, u := range s.usage {
		list = append(list, *u)
	}
	go writeJSON(filepath.Join(s.dir, "usage.json"), list)
}

// boost is how much usage raises an item: how often, and how lately.
func (s *store) boost(key string, now time.Time) int {
	u := s.usage[key]
	if u == nil {
		return 0
	}
	b := min(u.Count*5, 40)
	switch age := now.Sub(u.Last); {
	case age < 24*time.Hour:
		b += 20
	case age < 7*24*time.Hour:
		b += 10
	}
	return b
}

// recent lists the items opened lately, the latest first.
func (s *store) recent(n int) []Usage {
	list := make([]Usage, 0, len(s.usage))
	for _, u := range s.usage {
		list = append(list, *u)
	}
	slices.SortFunc(list, func(a, b Usage) int { return b.Last.Compare(a.Last) })
	return list[:min(n, len(list))]
}

func (s *store) forget(key string) { delete(s.usage, key) }

func (s *store) clear() {
	s.usage = map[string]*Usage{}
	if s.dir != "" {
		go writeJSON(filepath.Join(s.dir, "usage.json"), []Usage{})
	}
}

func readJSON(path string, v any) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(data, v); err != nil {
		log.Printf("%s: %v", path, err)
		return false
	}
	return true
}

// writeMu keeps writes of a file from overlapping, as two quick openings.
var writeMu sync.Mutex

func writeJSON(path string, v any) {
	writeMu.Lock()
	defer writeMu.Unlock()
	data, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	if err == nil {
		tmp := path + ".tmp"
		if err = os.WriteFile(tmp, data, 0o644); err == nil {
			err = os.Rename(tmp, path)
		}
	}
	if err != nil {
		log.Printf("saving %s: %v", path, err)
	}
}
