// Package config holds the user-editable settings that live in
// ~/.config/tuiple/config.json — next to bookmarks.json and audit.log.
//
// The whole file is optional: a missing or malformed config falls back
// to Defaults(), so a fresh checkout behaves exactly like the hardcoded
// build did before settings existed. Values are validated on load, which
// means a hand-edited file can never put the app into a broken state
// (an unknown delete mode degrades to "timer", a zero poll interval to
// 1 second, and so on).
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/JohnnyReverb9/tuiple/filesystem"
)

// ── Schema ─────────────────────────────────────────────────────────────

// DeleteMode selects what pressing `d` actually does to a file.
type DeleteMode string

const (
	// DeleteTimer moves the file to a hidden sibling and purges it after
	// Delete.TimerSeconds — undoable until the timer fires.
	DeleteTimer DeleteMode = "timer"
	// DeleteTrash hands the file to the macOS Trash (Finder), where it
	// stays until the user empties it. Undo moves it straight back.
	DeleteTrash DeleteMode = "trash"
	// DeletePermanent removes the file immediately. Not undoable.
	DeletePermanent DeleteMode = "permanent"
)

// DeleteConfig covers everything about removing files.
type DeleteConfig struct {
	Mode         DeleteMode `json:"mode"`
	TimerSeconds int        `json:"timer_seconds"`
	Confirm      bool       `json:"confirm"`
}

// StartDir decides where tuiple opens when no path is given.
type StartDir string

const (
	StartInHome StartDir = "home"
	StartInCwd  StartDir = "cwd"
)

// PasteConflict decides what happens when a pasted name is taken.
type PasteConflict string

const (
	// PasteRename appends _1, _2 … to the incoming name.
	PasteRename PasteConflict = "rename"
	// PasteSkip leaves the existing file alone.
	PasteSkip PasteConflict = "skip"
	// PasteOverwrite replaces it.
	PasteOverwrite PasteConflict = "overwrite"
)

// FilesConfig covers the file list: what it shows and how it sorts.
type FilesConfig struct {
	ShowHidden bool     `json:"show_hidden"`
	Sort       string   `json:"sort"` // name | size | date | type
	Reverse    bool     `json:"reverse_sort"`
	DirsFirst  bool     `json:"dirs_first"`
	Icons      bool     `json:"icons"`
	StartDir   StartDir `json:"start_dir"`

	// TimeFormat picks how the Modified column reads: "short"
	// (Jan 02 15:04), "iso" (2006-01-02 15:04) or "relative" (5m ago).
	TimeFormat string `json:"time_format"`
	// BinaryUnits shows sizes in KiB/MiB (1024) instead of kB/MB (1000).
	BinaryUnits bool `json:"binary_units"`

	// PasteConflict is what p does when the name already exists.
	PasteConflict PasteConflict `json:"paste_conflict"`
}

// PreviewConfig bounds what the preview panel is willing to render.
type PreviewConfig struct {
	Media      bool `json:"media"`        // image / video / PDF thumbnails
	MaxTextKB  int  `json:"max_text_kb"`  // text files above this show a placeholder
	MaxMediaMB int  `json:"max_media_mb"` // media files above this are skipped

	// LineNumbers puts a gutter in front of previewed text.
	LineNumbers bool `json:"line_numbers"`
	// HexDump renders binary files as hex; off just says it is binary.
	HexDump bool `json:"hex_dump"`
	// DirSizes walks a previewed directory to total it up. Off on a
	// machine where that walk is expensive (network shares, huge trees).
	DirSizes bool `json:"dir_sizes"`
}

// GitConfig gates the git integration. Turning it off stops all
// background `git status` probing, which is the single biggest source of
// work tuiple does while idle.
type GitConfig struct {
	Enabled     bool `json:"enabled"`
	PollSeconds int  `json:"poll_seconds"`
}

// UIConfig covers the three-panel layout.
type UIConfig struct {
	Sidebar      bool `json:"sidebar"`
	Preview      bool `json:"preview"`
	SidebarWidth int  `json:"sidebar_width_percent"`
	PreviewWidth int  `json:"preview_width_percent"`
}

// OpenRule routes a set of extensions to a command. The first rule whose
// extension list contains the file's extension wins; when no rule matches
// tuiple falls back to its built-in handlers (chafa / bookokrat /
// pdftotext / $EDITOR).
//
// Command may contain {} as a placeholder for the file path; without a
// placeholder the quoted path is appended. Terminal decides whether the
// command takes over the terminal (tuiple suspends itself and restores
// on exit — right for TUI programs) or is launched detached (right for
// GUI programs like `open -a Preview`).
type OpenRule struct {
	Extensions []string `json:"extensions"`
	Command    string   `json:"command"`
	Terminal   bool     `json:"terminal"`
}

// Handler names — the kinds of file tuiple knows how to open by itself.
// A handler set to a command replaces the built-in behaviour for that
// kind; left empty, the built-in stays.
const (
	HandlerImage   = "image"
	HandlerVideo   = "video"
	HandlerPDF     = "pdf"
	HandlerEbook   = "ebook"
	HandlerAudio   = "audio"
	HandlerArchive = "archive"
	HandlerDefault = "default"
)

// HandlerKinds lists the handlers in the order the settings screen shows
// them.
var HandlerKinds = []string{
	HandlerImage, HandlerVideo, HandlerPDF, HandlerEbook,
	HandlerAudio, HandlerArchive, HandlerDefault,
}

// OpenConfig covers how files and shells are launched.
type OpenConfig struct {
	Editor string `json:"editor"` // empty → $EDITOR, then vi
	Shell  string `json:"shell"`  // empty → $SHELL, then sh

	// Handlers maps a file kind to the command that opens it, with {}
	// standing for the path. This is the coarse knob — "open PDFs with
	// Preview.app" — while Rules below matches specific extensions.
	Handlers map[string]string `json:"handlers,omitempty"`

	Rules []OpenRule `json:"rules"`
}

// KeysConfig covers keyboard handling.
type KeysConfig struct {
	// CyrillicLayout translates ЙЦУКЕН keystrokes back to the QWERTY key
	// in the same physical position, so shortcuts keep working without
	// switching layouts. Text input is never translated.
	CyrillicLayout bool `json:"cyrillic_layout"`

	// Bindings rebinds actions by name — {"delete": ["d", "x"]}. Actions
	// left out keep their defaults; an empty list unbinds one. The
	// authoritative list of action names is in package keys, and `?`
	// shows whatever is in force.
	Bindings map[string][]string `json:"bindings,omitempty"`
}

// Config is the whole settings file.
type Config struct {
	Delete  DeleteConfig  `json:"delete"`
	Files   FilesConfig   `json:"files"`
	Preview PreviewConfig `json:"preview"`
	UI      UIConfig      `json:"ui"`
	Git     GitConfig     `json:"git"`
	Open    OpenConfig    `json:"open"`
	Keys    KeysConfig    `json:"keys"`
}

// Defaults returns the configuration tuiple ships with — identical to
// the behaviour that was hardcoded before this package existed.
func Defaults() Config {
	return Config{
		Delete: DeleteConfig{
			Mode:         DeleteTimer,
			TimerSeconds: 20,
			Confirm:      true,
		},
		Files: FilesConfig{
			ShowHidden:    false,
			Sort:          "name",
			Reverse:       false,
			DirsFirst:     true,
			Icons:         true,
			StartDir:      StartInHome,
			TimeFormat:    "short",
			BinaryUnits:   true,
			PasteConflict: PasteRename,
		},
		Preview: PreviewConfig{
			Media:       true,
			MaxTextKB:   1024,
			MaxMediaMB:  50,
			LineNumbers: true,
			HexDump:     true,
			DirSizes:    true,
		},
		UI: UIConfig{
			Sidebar:      true,
			Preview:      true,
			SidebarWidth: 20,
			PreviewWidth: 30,
		},
		Git: GitConfig{
			Enabled:     true,
			PollSeconds: 1,
		},
		Open: OpenConfig{
			Editor: "",
			Shell:  "",
			Rules:  nil,
		},
		Keys: KeysConfig{
			CyrillicLayout: true,
		},
	}
}

// ── Store ──────────────────────────────────────────────────────────────

var (
	mu   sync.RWMutex
	cfg  = Defaults()
	path = defaultPath()
)

// defaultPath is ~/.config/tuiple/config.json, unless TUIPLE_CONFIG
// points somewhere else. The override is what makes a throwaway or
// project-local configuration possible — and it is what keeps the tests
// from writing over the settings of whoever runs them.
func defaultPath() string {
	if p := strings.TrimSpace(os.Getenv("TUIPLE_CONFIG")); p != "" {
		return p
	}
	return filepath.Join(filesystem.HomeDir(), ".config", "tuiple", "config.json")
}

// Path returns the location of the settings file, for display in the UI.
func Path() string {
	mu.RLock()
	defer mu.RUnlock()
	return path
}

// Get returns a snapshot of the current settings. Preview loading runs
// in background goroutines, so the copy (including a fresh rules slice)
// matters: callers must never observe a half-applied edit.
func Get() Config {
	mu.RLock()
	defer mu.RUnlock()
	out := cfg
	out.Open.Rules = append([]OpenRule(nil), cfg.Open.Rules...)
	if cfg.Open.Handlers != nil {
		out.Open.Handlers = make(map[string]string, len(cfg.Open.Handlers))
		for k, v := range cfg.Open.Handlers {
			out.Open.Handlers[k] = v
		}
	}
	return out
}

// Load reads the settings file. A missing file is not an error — it just
// leaves the defaults in place. A corrupt file is reported so the status
// bar can say so, but the defaults still apply.
func Load() error {
	// Re-read the location every time: TUIPLE_CONFIG may have been set
	// after this package was initialised.
	mu.Lock()
	path = defaultPath()
	current := path
	mu.Unlock()

	data, err := os.ReadFile(current)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	parsed := Defaults()
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	sanitize(&parsed)

	mu.Lock()
	cfg = parsed
	mu.Unlock()
	return nil
}

// Update applies fn to the settings and writes the result to disk. The
// mutation and the save happen under one lock so two rapid keystrokes in
// the settings popup can't race each other into a torn file.
func Update(fn func(*Config)) error {
	mu.Lock()
	defer mu.Unlock()
	next := cfg
	next.Open.Rules = append([]OpenRule(nil), cfg.Open.Rules...)
	if cfg.Open.Handlers != nil {
		next.Open.Handlers = make(map[string]string, len(cfg.Open.Handlers))
		for k, v := range cfg.Open.Handlers {
			next.Open.Handlers[k] = v
		}
	}
	fn(&next)
	sanitize(&next)
	cfg = next
	return save(next)
}

// Reset restores the shipped defaults and persists them.
func Reset() error {
	return Update(func(c *Config) { *c = Defaults() })
}

// save writes the settings file. Callers hold the lock.
func save(c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Write via a temp file in the same directory so an interrupted
	// write can't leave a truncated config behind.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// sanitize clamps hand-edited values into ranges the app can honour.
func sanitize(c *Config) {
	switch c.Delete.Mode {
	case DeleteTimer, DeleteTrash, DeletePermanent:
	default:
		c.Delete.Mode = DeleteTimer
	}
	c.Delete.TimerSeconds = clamp(c.Delete.TimerSeconds, 3, 600)

	switch c.Files.StartDir {
	case StartInHome, StartInCwd:
	default:
		c.Files.StartDir = StartInHome
	}

	switch strings.ToLower(c.Files.Sort) {
	case "name", "size", "date", "type":
		c.Files.Sort = strings.ToLower(c.Files.Sort)
	default:
		c.Files.Sort = "name"
	}

	switch c.Files.TimeFormat {
	case "short", "iso", "relative":
	default:
		c.Files.TimeFormat = "short"
	}

	switch c.Files.PasteConflict {
	case PasteRename, PasteSkip, PasteOverwrite:
	default:
		c.Files.PasteConflict = PasteRename
	}

	c.UI.SidebarWidth = clamp(c.UI.SidebarWidth, 10, 40)
	c.UI.PreviewWidth = clamp(c.UI.PreviewWidth, 15, 60)

	c.Preview.MaxTextKB = clamp(c.Preview.MaxTextKB, 4, 65536)
	c.Preview.MaxMediaMB = clamp(c.Preview.MaxMediaMB, 1, 4096)
	c.Git.PollSeconds = clamp(c.Git.PollSeconds, 1, 60)

	// Normalise extensions to ".ext" in lower case so matching at open
	// time is a plain map lookup against filepath.Ext.
	for i := range c.Open.Rules {
		exts := c.Open.Rules[i].Extensions
		for j, e := range exts {
			e = strings.ToLower(strings.TrimSpace(e))
			if e != "" && !strings.HasPrefix(e, ".") {
				e = "." + e
			}
			exts[j] = e
		}
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ── Derived helpers ────────────────────────────────────────────────────

// Editor returns the editor command to launch, honouring the config
// first, then $EDITOR, then a vi fallback.
func (c Config) Editor() string {
	if e := strings.TrimSpace(c.Open.Editor); e != "" {
		return e
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	return "vi"
}

// Shell returns the shell to drop into with `S`.
func (c Config) Shell() string {
	if s := strings.TrimSpace(c.Open.Shell); s != "" {
		return s
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "sh"
}

// RuleFor returns the first open rule matching ext (".png", lower case),
// or nil when the built-in handlers should take over.
func (c Config) RuleFor(ext string) *OpenRule {
	ext = strings.ToLower(ext)
	for i := range c.Open.Rules {
		if strings.TrimSpace(c.Open.Rules[i].Command) == "" {
			continue
		}
		for _, e := range c.Open.Rules[i].Extensions {
			if e == ext {
				return &c.Open.Rules[i]
			}
		}
	}
	return nil
}

// Handler returns the command configured for a file kind, or "" when the
// built-in behaviour should stay.
func (c Config) Handler(kind string) string {
	return strings.TrimSpace(c.Open.Handlers[kind])
}

// SetHandler records (or clears, when cmd is empty) a handler command.
func (c *Config) SetHandler(kind, cmd string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		delete(c.Open.Handlers, kind)
		return
	}
	if c.Open.Handlers == nil {
		c.Open.Handlers = make(map[string]string, len(HandlerKinds))
	}
	c.Open.Handlers[kind] = cmd
}

// HandlerIsGUI reports whether a handler command opens a windowed app
// rather than taking over the terminal. macOS has exactly one way to
// launch a GUI application from a shell, so the test is that simple —
// and `rules` still carries an explicit terminal flag for the rest.
func HandlerIsGUI(cmd string) bool {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "open", "/usr/bin/open":
		return true
	}
	return false
}

// SortMode converts the configured sort name into the filesystem enum.
func (c Config) SortMode() filesystem.SortMode {
	switch c.Files.Sort {
	case "size":
		return filesystem.SortBySize
	case "date":
		return filesystem.SortByDate
	case "type":
		return filesystem.SortByType
	}
	return filesystem.SortByName
}
