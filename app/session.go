package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/JohnnyReverb9/tuiple/config"
	"github.com/JohnnyReverb9/tuiple/filesystem"
)

// The session is the set of open tabs, written next to config.json so
// that `files.start_dir: last` can put them back on the next start —
// what an IDE does when it reopens the projects you had open.
//
// It is saved whether or not that setting is on. The file is a few
// hundred bytes, and it means switching the setting on restores the
// session you already had rather than starting to record one.

type sessionState struct {
	Tabs   []string `json:"tabs"`
	Active int      `json:"active"`
}

func sessionPath() string {
	return filepath.Join(filepath.Dir(config.Path()), "session.json")
}

// LoadSession returns the tabs of the last session that still exist on
// disk. ok is false when there is nothing usable to restore.
func LoadSession() (paths []string, active int, ok bool) {
	data, err := os.ReadFile(sessionPath())
	if err != nil {
		return nil, 0, false
	}
	var st sessionState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, 0, false
	}
	// A directory deleted since last time simply drops out; the active
	// index follows whatever is left of the tabs before it.
	for i, p := range st.Tabs {
		if !filesystem.IsDir(p) {
			if i < st.Active {
				active--
			}
			continue
		}
		paths = append(paths, p)
	}
	active += st.Active
	if len(paths) == 0 {
		return nil, 0, false
	}
	if active < 0 || active >= len(paths) {
		active = 0
	}
	return paths, active, true
}

// saveSession writes the open tabs out. Failures are silent for the same
// reason writeExitCwd's are: this is a convenience, and an unwritable
// config directory is not worth interrupting anyone over.
func (m *Model) saveSession() {
	m.sessionDirty = false
	if m.sessionOff {
		return
	}
	st := sessionState{Tabs: m.tabPaths(), Active: m.tabIdx}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	path := sessionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
