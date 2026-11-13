// Package keys is the single source of truth for tuiple's keyboard
// shortcuts.
//
// Before this package the same binding was written down three times — in
// the switch that handled it, in the help screen, and in the README —
// and they drifted apart. Now the table below is the only place a
// default lives: the handlers ask it what a keystroke means, the help
// screen is generated from it, and users override it from config.json.
//
// The table covers the two scopes a key can belong to: the ones that
// work anywhere (Global) and the ones that belong to the file list
// (List). Modal popups — git, search, settings, history — keep their own
// local keys, because there a key means whatever that popup is currently
// showing, and hoisting them into a global table would only obscure it.
package keys

import (
	"strings"
	"sync"
)

// Action is what a keystroke asks for, independent of which key produced
// it.
type Action string

// Scope is where a binding applies.
type Scope int

const (
	// ScopeGlobal keys work from any panel.
	ScopeGlobal Scope = iota
	// ScopeList keys work while the file list has focus.
	ScopeList
)

// Global actions.
const (
	Quit          Action = "quit"
	Help          Action = "help"
	Settings      Action = "settings"
	GoToPath      Action = "goto-path"
	History       Action = "history"
	SearchName    Action = "search-name"
	SearchContent Action = "search-content"
	GitPanel      Action = "git-panel"
	Branches      Action = "branches"
	Bookmark      Action = "bookmark"
	SortMenu      Action = "sort-menu"
	Shell         Action = "shell"
	Undo          Action = "undo"
	Redo          Action = "redo"
	PanelNext     Action = "panel-next"
	PanelPrev     Action = "panel-prev"
	NavBack       Action = "back"
	NavForward    Action = "forward"
	Palette       Action = "find-action"
	Recent        Action = "recent"
	TabNew        Action = "tab-new"
	TabClose      Action = "tab-close"
	TabReopen     Action = "tab-reopen"
	TabNext       Action = "tab-next"
	TabPrev       Action = "tab-prev"
	TabJump       Action = "tab-jump"
	ToggleSidebar Action = "toggle-sidebar"
	TogglePreview Action = "toggle-preview"
	SeekBack      Action = "audio-seek-back"
	SeekForward   Action = "audio-seek-forward"
	SeekBackFar   Action = "audio-seek-back-far"
	SeekFwdFar    Action = "audio-seek-forward-far"
)

// File-list actions.
const (
	CursorUp     Action = "cursor-up"
	CursorDown   Action = "cursor-down"
	Open         Action = "open"
	Parent       Action = "parent"
	Top          Action = "top"
	Bottom       Action = "bottom"
	HalfPageUp   Action = "half-page-up"
	HalfPageDown Action = "half-page-down"
	HomeDir      Action = "home-dir"
	ToggleHidden Action = "toggle-hidden"
	Filter       Action = "filter"
	Mark         Action = "mark"
	ClearMarks   Action = "clear-marks"
	Copy         Action = "copy"
	Cut          Action = "cut"
	Paste        Action = "paste"
	Delete       Action = "delete"
	Rename       Action = "rename"
	BulkRename   Action = "bulk-rename"
	NewFile      Action = "new-file"
	NewDir       Action = "new-dir"
	Zip          Action = "zip"
	Extract      Action = "extract"
	DirSize      Action = "dir-size"
	SelectAll    Action = "select-all"
	InvertMarks  Action = "invert-selection"
	Duplicate    Action = "duplicate"
	CopyPath     Action = "copy-path"
	OpenExternal Action = "open-default-app"
	Reveal       Action = "reveal-in-finder"
	OpenInTab    Action = "open-in-new-tab"

	GitStage       Action = "git-stage"
	GitUnstage     Action = "git-unstage"
	GitDiscard     Action = "git-discard"
	GitIgnore      Action = "git-ignore"
	GitFileHistory Action = "git-file-history"
	GitBlame       Action = "git-blame"
)

// Binding ties an action to the keys that trigger it, plus the text the
// help screen shows for it.
type Binding struct {
	Action   Action
	Scope    Scope
	Keys     []string
	Desc     string
	Category string // help-screen grouping
	// HelpKeys overrides how the keys are printed in help, for bindings
	// whose raw key list would read badly ("up ↑ k" → "↑ ↓ / k j").
	HelpKeys string
}

// Defaults is the shipped keymap. Order matters only for the help
// screen, which follows it top to bottom within each category.
var Defaults = []Binding{
	// ── Navigation ────────────────────────────────────────────────
	{CursorUp, ScopeList, []string{"up", "k"}, "Move up", "Navigation", "↑ / k"},
	{CursorDown, ScopeList, []string{"down", "j"}, "Move down", "Navigation", "↓ / j"},
	{Open, ScopeList, []string{"enter", "right", "l"}, "Enter dir / open file", "Navigation", "Enter / → / l"},
	{Parent, ScopeList, []string{"backspace", "left", "h"}, "Go back to parent", "Navigation", "Backspace / ← / h"},
	{Top, ScopeList, []string{"g"}, "Go to top", "Navigation", ""},
	{Bottom, ScopeList, []string{"G"}, "Go to bottom", "Navigation", ""},
	{HalfPageUp, ScopeList, []string{"ctrl+u"}, "Half page up", "Navigation", "Ctrl+U"},
	{HalfPageDown, ScopeList, []string{"ctrl+d"}, "Half page down", "Navigation", "Ctrl+D"},
	{HomeDir, ScopeList, []string{"~"}, "Go to home directory", "Navigation", ""},
	{GoToPath, ScopeGlobal, []string{":"}, "Go to path", "Navigation", ""},
	{NavBack, ScopeGlobal, []string{"[", "cmd+["}, "Back", "Navigation", ""},
	{NavForward, ScopeGlobal, []string{"]", "cmd+]"}, "Forward", "Navigation", ""},
	{PanelNext, ScopeGlobal, []string{"tab"}, "Next panel", "Navigation", "Tab"},
	{PanelPrev, ScopeGlobal, []string{"shift+tab"}, "Previous panel", "Navigation", "Shift+Tab"},

	// ── Tabs ──────────────────────────────────────────────────────
	//
	// Finder's own shortcuts with the Cmd taken off: Cmd+T becomes t,
	// Cmd+Shift+] becomes }, and so on.
	{TabNew, ScopeGlobal, []string{"t"}, "New tab here", "Tabs", ""},
	{OpenInTab, ScopeList, []string{"T"}, "Open the folder in a new tab", "Tabs", ""},
	{TabClose, ScopeGlobal, []string{"w"}, "Close tab", "Tabs", ""},
	{TabReopen, ScopeGlobal, []string{"ctrl+t"}, "Reopen the closed tab", "Tabs", ""},
	{TabNext, ScopeGlobal, []string{"}"}, "Next tab", "Tabs", ""},
	{TabPrev, ScopeGlobal, []string{"{"}, "Previous tab", "Tabs", ""},
	{TabJump, ScopeGlobal, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}, "Jump to tab", "Tabs", "1 … 9"},

	// ── Selection, clipboard, file operations, undo ───────────────
	{Mark, ScopeList, []string{" "}, "Toggle selection", "Selection & Clipboard", "Space"},
	{SelectAll, ScopeList, []string{"ctrl+a", "cmd+a"}, "Select all", "Selection & Clipboard", ""},
	{InvertMarks, ScopeList, []string{"*"}, "Invert selection", "Selection & Clipboard", ""},
	{ClearMarks, ScopeList, []string{"esc"}, "Clear all selections", "Selection & Clipboard", "Esc"},
	{Copy, ScopeList, []string{"c"}, "Copy", "Selection & Clipboard", ""},
	{Cut, ScopeList, []string{"x"}, "Cut", "Selection & Clipboard", ""},
	{Paste, ScopeList, []string{"p"}, "Paste", "Selection & Clipboard", ""},
	{Duplicate, ScopeList, []string{"C", "cmd+d"}, "Duplicate", "File Operations", ""},
	{Delete, ScopeList, []string{"d", "cmd+backspace"}, "Delete (mode set in settings)", "File Operations", ""},
	{Rename, ScopeList, []string{"r"}, "Rename", "File Operations", ""},
	{BulkRename, ScopeList, []string{"E"}, "Bulk rename in $EDITOR", "File Operations", ""},
	{NewFile, ScopeList, []string{"n"}, "New file", "File Operations", ""},
	{NewDir, ScopeList, []string{"N"}, "New directory", "File Operations", ""},
	{Zip, ScopeList, []string{"z"}, "Zip marked entries", "File Operations", ""},
	{Extract, ScopeList, []string{"Z"}, "Extract archive", "File Operations", ""},
	{DirSize, ScopeList, []string{"s"}, "Measure directory size", "File Operations", ""},
	{CopyPath, ScopeList, []string{"y"}, "Copy path to the clipboard", "File Operations", ""},
	{OpenExternal, ScopeList, []string{"O"}, "Open with the default app", "File Operations", ""},
	{Reveal, ScopeList, []string{"ctrl+o"}, "Reveal in Finder", "File Operations", ""},
	{Undo, ScopeGlobal, []string{"u", "cmd+z"}, "Undo", "Undo & History", ""},
	{Redo, ScopeGlobal, []string{"U", "cmd+shift+z"}, "Redo", "Undo & History", ""},
	{History, ScopeGlobal, []string{"Y"}, "Action history", "Undo & History", ""},

	// ── Search & bookmarks ────────────────────────────────────────
	{Palette, ScopeGlobal, []string{"ctrl+p", "cmd+shift+a"}, "Find action", "Search & Bookmarks", ""},
	{Recent, ScopeGlobal, []string{"e", "cmd+e"}, "Recent files and places", "Search & Bookmarks", ""},
	{SearchName, ScopeGlobal, []string{"f", "cmd+shift+o"}, "Go to file by name", "Search & Bookmarks", ""},
	{SearchContent, ScopeGlobal, []string{"F", "cmd+shift+f"}, "Find in files", "Search & Bookmarks", ""},
	{Filter, ScopeList, []string{"/", "cmd+f"}, "Filter the current list", "Search & Bookmarks", ""},
	{Bookmark, ScopeGlobal, []string{"'"}, "(Un)bookmark this directory", "Search & Bookmarks", ""},

	// ── Audio ─────────────────────────────────────────────────────
	{SeekBack, ScopeGlobal, []string{"-", "−"}, "Seek back 5 seconds", "Audio Player", "-"},
	{SeekForward, ScopeGlobal, []string{"="}, "Seek forward 5 seconds", "Audio Player", "="},
	{SeekBackFar, ScopeGlobal, []string{"_"}, "Seek back 30 seconds", "Audio Player", ""},
	{SeekFwdFar, ScopeGlobal, []string{"+"}, "Seek forward 30 seconds", "Audio Player", ""},

	// ── System ────────────────────────────────────────────────────
	{ToggleHidden, ScopeList, []string{"."}, "Toggle hidden files", "System & Options", ""},
	{SortMenu, ScopeGlobal, []string{"o"}, "Sort by name / size / date", "System & Options", "o n / o s / o d"},
	{ToggleSidebar, ScopeGlobal, []string{"B"}, "Show / hide the sidebar", "System & Options", ""},
	{TogglePreview, ScopeGlobal, []string{"V"}, "Show / hide the preview", "System & Options", ""},
	{Settings, ScopeGlobal, []string{","}, "Settings", "System & Options", ""},
	{Shell, ScopeGlobal, []string{"S"}, "Open a shell here", "System & Options", ""},
	{Help, ScopeGlobal, []string{"?"}, "Keyboard shortcuts", "System & Options", ""},
	{Quit, ScopeGlobal, []string{"q", "ctrl+c"}, "Quit", "System & Options", "q / Ctrl+C"},

	// ── Git ───────────────────────────────────────────────────────
	{GitPanel, ScopeGlobal, []string{"ctrl+g"}, "Git panel (commit / log / stashes)", "Git", "Ctrl+G"},
	{Branches, ScopeGlobal, []string{"b"}, "Branches popup", "Git", ""},
	{GitStage, ScopeList, []string{"a"}, "Stage the file under the cursor", "Git", ""},
	{GitUnstage, ScopeList, []string{"R"}, "Unstage the file", "Git", ""},
	{GitDiscard, ScopeList, []string{"D"}, "Discard the file's changes", "Git", ""},
	{GitIgnore, ScopeList, []string{"i"}, "Add to .gitignore", "Git", ""},
	{GitFileHistory, ScopeList, []string{"H"}, "File history", "Git", ""},
	{GitBlame, ScopeList, []string{"L"}, "Blame the file", "Git", ""},
}

// ── Runtime keymap ─────────────────────────────────────────────────────

var (
	mu       sync.RWMutex
	bindings = cloneDefaults()
	lookup   = buildLookup(bindings)
)

func cloneDefaults() []Binding {
	out := make([]Binding, len(Defaults))
	copy(out, Defaults)
	for i := range out {
		out[i].Keys = append([]string(nil), Defaults[i].Keys...)
	}
	return out
}

type lookupKey struct {
	scope Scope
	key   string
}

func buildLookup(bs []Binding) map[lookupKey]Action {
	m := make(map[lookupKey]Action, len(bs)*2)
	for _, b := range bs {
		for _, k := range b.Keys {
			m[lookupKey{b.Scope, k}] = b.Action
		}
	}
	return m
}

// SetOverrides replaces the keys of the named actions. Anything absent
// from the map keeps its default, an unknown action name is ignored, and
// an empty key list unbinds the action outright — which is a legitimate
// thing to want for a key you keep hitting by accident.
func SetOverrides(overrides map[string][]string) {
	next := cloneDefaults()
	for i := range next {
		if keys, ok := overrides[string(next[i].Action)]; ok {
			next[i].Keys = append([]string(nil), keys...)
			// The hand-written help label described the default keys.
			next[i].HelpKeys = ""
		}
	}

	mu.Lock()
	bindings = next
	lookup = buildLookup(next)
	mu.Unlock()
}

// Match resolves a keystroke within a scope.
func Match(scope Scope, key string) (Action, bool) {
	mu.RLock()
	defer mu.RUnlock()
	a, ok := lookup[lookupKey{scope, key}]
	return a, ok
}

// Is reports whether a keystroke triggers a specific action.
func Is(scope Scope, key string, action Action) bool {
	a, ok := Match(scope, key)
	return ok && a == action
}

// All returns the active keymap, for the help screen.
func All() []Binding {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Binding, len(bindings))
	copy(out, bindings)
	return out
}

// Display renders a binding's keys the way help should show them.
func (b Binding) Display() string {
	if b.HelpKeys != "" {
		return b.HelpKeys
	}
	if len(b.Keys) == 0 {
		return "(unbound)"
	}
	out := Pretty(b.Keys[0])
	for _, k := range b.Keys[1:] {
		out += " / " + Pretty(k)
	}
	return out
}

// Short is the one key worth showing where space is tight — the command
// palette's right-hand column.
func (b Binding) Short() string {
	if len(b.Keys) == 0 {
		return ""
	}
	return Pretty(b.Keys[0])
}

// Pretty turns a raw key name into what is printed on the keyboard:
// "cmd+shift+o" reads as ⌘⇧O, "ctrl+p" as Ctrl+P, " " as Space.
func Pretty(key string) string {
	named := map[string]string{
		" ": "Space", "enter": "Enter", "esc": "Esc", "tab": "Tab",
		"backspace": "⌫", "up": "↑", "down": "↓", "left": "←", "right": "→",
	}
	// Peel the modifiers off the front; what is left is the key itself.
	var cmd, shift, alt, ctrl bool
	rest := key
	for {
		switch {
		case strings.HasPrefix(rest, "cmd+") && len(rest) > 4:
			cmd, rest = true, rest[4:]
			continue
		case strings.HasPrefix(rest, "shift+") && len(rest) > 6:
			shift, rest = true, rest[6:]
			continue
		case strings.HasPrefix(rest, "alt+") && len(rest) > 4:
			alt, rest = true, rest[4:]
			continue
		case strings.HasPrefix(rest, "ctrl+") && len(rest) > 5:
			ctrl, rest = true, rest[5:]
			continue
		}
		break
	}
	name, isNamed := named[rest]
	if !isNamed {
		name = rest
	}
	if !cmd && !shift && !alt && !ctrl {
		return name
	}
	if !isNamed && len(rest) == 1 {
		name = strings.ToUpper(rest)
	}
	if cmd {
		// The macOS menu spelling: modifiers as glyphs, no separators.
		out := ""
		if ctrl {
			out += "⌃"
		}
		if alt {
			out += "⌥"
		}
		if shift {
			out += "⇧"
		}
		return out + "⌘" + name
	}
	out := ""
	if ctrl {
		out += "Ctrl+"
	}
	if alt {
		out += "Alt+"
	}
	if shift {
		out += "Shift+"
	}
	return out + name
}

// Find returns the active binding of an action.
func Find(action Action) (Binding, bool) {
	mu.RLock()
	defer mu.RUnlock()
	for _, b := range bindings {
		if b.Action == action {
			return b, true
		}
	}
	return Binding{}, false
}

// Categories lists the help-screen groups in table order.
func Categories() []string {
	var out []string
	seen := make(map[string]bool)
	for _, b := range All() {
		if b.Category == "" || seen[b.Category] {
			continue
		}
		seen[b.Category] = true
		out = append(out, b.Category)
	}
	return out
}

// InCategory returns the bindings of one help-screen group, in table
// order.
func InCategory(category string) []Binding {
	var out []Binding
	for _, b := range All() {
		if b.Category == category {
			out = append(out, b)
		}
	}
	return out
}

// ActionNames lists every action a user may rebind, for validation and
// documentation.
func ActionNames() []string {
	out := make([]string, 0, len(Defaults))
	for _, b := range Defaults {
		out = append(out, string(b.Action))
	}
	return out
}
