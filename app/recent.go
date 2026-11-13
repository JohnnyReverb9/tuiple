package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/JohnnyReverb9/tuiple/config"
)

// Recent files and places — what Cmd+E is in a JetBrains IDE.
//
// Two kinds of thing are remembered: files that were opened, and
// directories that were arrived at on purpose (a path typed in, a
// bookmark, a search result, a project root). Directories merely passed
// through on the way somewhere are not, or the list would be a log of
// every keystroke instead of the short list of places worth returning to.

// RecentEntry is one remembered file or directory.
type RecentEntry struct {
	Path  string    `json:"path"`
	IsDir bool      `json:"dir,omitempty"`
	At    time.Time `json:"at"`
}

// recentLimit bounds the list; the oldest entries fall off the end.
const recentLimit = 200

var (
	recentMu     sync.Mutex
	recentItems  []RecentEntry
	recentLoaded string // the file recentItems was read from
)

func recentPath() string {
	return filepath.Join(filepath.Dir(config.Path()), "recent.json")
}

// loadRecentLocked reads the list once per location. Tests point the
// config somewhere else between runs, which is why the location is
// compared rather than a plain "already loaded" flag.
func loadRecentLocked() {
	path := recentPath()
	if recentLoaded == path {
		return
	}
	recentLoaded = path
	recentItems = nil
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &recentItems)
}

// TouchRecent moves a path to the top of the list, adding it if needed.
func TouchRecent(path string, isDir bool) {
	if path == "" {
		return
	}
	recentMu.Lock()
	defer recentMu.Unlock()
	loadRecentLocked()

	next := make([]RecentEntry, 0, len(recentItems)+1)
	next = append(next, RecentEntry{Path: path, IsDir: isDir, At: time.Now()})
	for _, e := range recentItems {
		if e.Path != path {
			next = append(next, e)
		}
	}
	if len(next) > recentLimit {
		next = next[:recentLimit]
	}
	recentItems = next

	// Best effort, like the session file: losing the list costs nothing
	// but convenience.
	data, err := json.MarshalIndent(recentItems, "", "  ")
	if err != nil {
		return
	}
	target := recentPath()
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, target)
}

// RecentEntries returns the list, newest first, leaving out whatever has
// been deleted or moved since it was recorded.
func RecentEntries() []RecentEntry {
	recentMu.Lock()
	defer recentMu.Unlock()
	loadRecentLocked()

	out := make([]RecentEntry, 0, len(recentItems))
	for _, e := range recentItems {
		if _, err := os.Stat(e.Path); err == nil {
			out = append(out, e)
		}
	}
	return out
}
