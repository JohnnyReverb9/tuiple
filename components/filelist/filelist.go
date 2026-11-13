// Package filelist implements the central file-listing panel.
package filelist

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JohnnyReverb9/tuiple/config"
	"github.com/JohnnyReverb9/tuiple/filesystem"
	"github.com/JohnnyReverb9/tuiple/icons"
	"github.com/JohnnyReverb9/tuiple/keys"
	"github.com/JohnnyReverb9/tuiple/theme"
)

// DirChangedMsg is emitted when the current directory changes.
type DirChangedMsg struct {
	Path string
}

// ── File operation messages ────────────────────────────────────────────

type CopyMsg struct{ Entries []filesystem.FileEntry }
type CutMsg struct{ Entries []filesystem.FileEntry }
type PasteRequestMsg struct{}
type DeleteRequestMsg struct{ Entries []filesystem.FileEntry }
type RenameRequestMsg struct{ Entry filesystem.FileEntry }
type CreateFileRequestMsg struct{}
type CreateDirRequestMsg struct{}
type OpenFileRequestMsg struct{ Path string }

// ── Git operation messages (emitted when the list is inside a repo) ────

// GitStageMsg asks the app to stage a file (abs path).
type GitStageMsg struct{ Path string }

// GitUnstageMsg asks the app to unstage a file (abs path).
type GitUnstageMsg struct{ Path string }

// GitDiscardMsg asks the app to confirm & discard working-tree changes (abs path).
type GitDiscardMsg struct{ Path string }

// GitIgnoreMsg asks the app to add a path to .gitignore (abs path).
type GitIgnoreMsg struct{ Path string }

// GitFileHistoryMsg asks the app to open the file-history popup (abs path).
type GitFileHistoryMsg struct{ Path string }

// GitBlameMsg asks the app to open the blame popup (abs path).
type GitBlameMsg struct{ Path string }

// BulkRenameRequestMsg asks the app to open the marked names (or the
// whole directory when nothing is marked) in the editor.
type BulkRenameRequestMsg struct {
	Dir   string
	Names []string
}

// DirSizeRequestMsg asks the app to measure directories on demand —
// walking every tree in the list on every redraw would be far too slow
// to do automatically.
type DirSizeRequestMsg struct{ Paths []string }

// SetDirSizeMsg carries a finished measurement back into the list.
type SetDirSizeMsg struct {
	Path string
	Size int64
}

// ArchiveRequestMsg asks the app to zip the marked entries.
type ArchiveRequestMsg struct {
	Dir   string
	Names []string
}

// ExtractRequestMsg asks the app to unpack the archive under the cursor.
type ExtractRequestMsg struct{ Path string }

// DuplicateRequestMsg asks the app to copy entries next to themselves.
type DuplicateRequestMsg struct{ Entries []filesystem.FileEntry }

// CopyPathMsg asks the app to put absolute paths on the system clipboard.
type CopyPathMsg struct{ Paths []string }

// OpenExternalMsg asks the app to hand a path to macOS: opened with its
// default application, or revealed in a Finder window.
type OpenExternalMsg struct {
	Path   string
	Reveal bool
}

// OpenInTabMsg asks the app to open a directory in a new tab.
type OpenInTabMsg struct{ Path string }

// ClearSelectionMsg tells the list to drop its active selections.
type ClearSelectionMsg struct{}

// RefreshListMsg asks the list to reload entries.
type RefreshListMsg struct{}

// EntriesLoadedMsg carries the result of a directory read back to the
// list. Reads happen off the UI goroutine, so a slow directory — a
// network share, or one with tens of thousands of files — no longer
// freezes the interface while it is being listed.
type EntriesLoadedMsg struct {
	// ListID names the list that asked. With tabs there are several
	// lists alive at once, and a read started in one of them must not
	// land in whichever happens to be on screen when it finishes.
	ListID  int64
	Path    string
	Token   int
	Entries []filesystem.FileEntry
	Err     error
}

// SetShowHiddenMsg asks the list to show or hide dotfiles. Sent when the
// setting changes so the switch takes effect without a restart.
type SetShowHiddenMsg struct{ Show bool }

// SortListMsg asks the list to reload with a specific sort mode.
type SortListMsg struct {
	Mode filesystem.SortMode
}

// ── Model ──────────────────────────────────────────────────────────────

// location is one stop in the back / forward history: a directory and
// where the cursor was when it was left.
type location struct {
	path   string
	name   string // entry under the cursor, the reliable way back to it
	cursor int
	offset int
}

// historyLimit caps each of the two stacks. Nobody steps back a hundred
// directories, and an unbounded slice in a long-lived tab is a leak.
const historyLimit = 100

// nextID hands every list its own identity; see EntriesLoadedMsg.ListID.
var nextID atomic.Int64

// Model is the file-list Bubble Tea model.
type Model struct {
	id int64

	// allEntries is the directory as read from disk; entries is that
	// list after the live filter. Keeping both means typing in the
	// filter never touches the disk.
	allEntries  []filesystem.FileEntry
	entries     []filesystem.FileEntry
	cursor      int
	offset      int // scroll offset
	currentPath string
	showHidden  bool
	sortMode    filesystem.SortMode
	filter      string
	filtering   bool

	width   int
	height  int
	focused bool

	// back and forward are the two halves of the navigation history,
	// most recent last. Going anywhere new clears forward, the way a
	// browser does.
	back     []location
	forward  []location
	selected map[string]struct{}
	err      error

	gitFileStat map[string]string   // abs path -> 2-char porcelain code
	gitDirHas   map[string]struct{} // dirs that contain changes (abs path)

	// A read in flight is identified by loadToken; results arriving with
	// a stale token belong to a directory the user has already left.
	loadToken int
	loading   bool

	// Where to put the cursor once the pending read lands: on a named
	// entry if it is there, otherwise back at this position.
	pendingSelect string
	pendingCursor int
	pendingOffset int

	// dirSizes holds directory sizes the user asked for with s. Sizes
	// are keyed by absolute path and survive navigation, so stepping
	// back into a directory still shows what was measured earlier.
	dirSizes map[string]int64
}

// SetGitState injects git status data so the list can render per-row
// markers. Passing nil maps clears the markers.
func (m *Model) SetGitState(fileStat map[string]string, dirHas map[string]struct{}) {
	m.gitFileStat = fileStat
	m.gitDirHas = dirHas
}

// New creates a file-list rooted at the given path.
func New(path string) Model {
	cfg := config.Get()
	m := Model{
		id:          nextID.Add(1),
		currentPath: path,
		sortMode:    cfg.SortMode(),
		showHidden:  cfg.Files.ShowHidden,
		focused:     true,
		selected:    make(map[string]struct{}),
		dirSizes:    make(map[string]int64),
	}
	m.loadNow()
	return m
}

// Spawn creates a list for another tab: a different directory and a
// clean history, but the same view of it — whoever turned hidden files
// on or sorted by date expects the new tab to look the same.
func (m Model) Spawn(path string) Model {
	next := Model{
		id:          nextID.Add(1),
		currentPath: path,
		sortMode:    m.sortMode,
		showHidden:  m.showHidden,
		width:       m.width,
		height:      m.height,
		focused:     true,
		selected:    make(map[string]struct{}),
		dirSizes:    m.dirSizes,
		gitFileStat: nil,
	}
	next.loadNow()
	return next
}

// ── Public accessors ───────────────────────────────────────────────────

func (m Model) ID() int64                       { return m.id }
func (m Model) CursorIdx() int                  { return m.cursor }
func (m Model) CurrentPath() string             { return m.currentPath }
func (m Model) Entries() []filesystem.FileEntry { return m.entries }
func (m Model) IsFiltering() bool               { return m.filtering }
func (m Model) Filter() string                  { return m.filter }
func (m Model) ShowHidden() bool                { return m.showHidden }
func (m Model) SortMode() filesystem.SortMode   { return m.sortMode }
func (m Model) CanGoBack() bool                 { return len(m.back) > 0 }
func (m Model) CanGoForward() bool              { return len(m.forward) > 0 }

// MarkedCount is how many entries of the current directory are marked.
func (m Model) MarkedCount() int { return len(m.selected) }

// MarkedSize totals the marked files, for the status bar. Directories
// count only once they have been measured with s.
func (m Model) MarkedSize() int64 {
	var total int64
	for _, e := range m.entries {
		if _, ok := m.selected[e.Path]; !ok {
			continue
		}
		if e.IsDir {
			total += m.dirSizes[e.Path]
		} else {
			total += e.Size
		}
	}
	return total
}

// SelectByName positions the cursor on the entry with the given name.
// When a directory read is still in flight the name is remembered and
// applied the moment the entries land — that is the normal case right
// after a search result jumps to another directory.
func (m Model) SelectByName(name string) Model {
	if m.loading {
		m.pendingSelect = name
		return m
	}
	for i, e := range m.entries {
		if e.Name == name {
			m.cursor = i
			m.fixScroll()
			break
		}
	}
	return m
}

func (m Model) SelectedEntry() *filesystem.FileEntry {
	if m.cursor >= 0 && m.cursor < len(m.entries) {
		e := m.entries[m.cursor]
		return &e
	}
	return nil
}

// bulkRenameNames lists what a bulk rename should cover: the marked
// entries in list order, or every entry in the directory when nothing is
// marked. Order matters — it is the only link between a line in the
// editor and the file it renames.
func (m Model) bulkRenameNames() []string {
	names := make([]string, 0, len(m.entries))
	if len(m.selected) > 0 {
		for _, e := range m.entries {
			if _, marked := m.selected[e.Path]; marked {
				names = append(names, e.Name)
			}
		}
		return names
	}
	for _, e := range m.entries {
		names = append(names, e.Name)
	}
	return names
}

func (m Model) SelectedEntries() []filesystem.FileEntry {
	if len(m.selected) == 0 {
		if e := m.SelectedEntry(); e != nil {
			return []filesystem.FileEntry{*e}
		}
		return nil
	}
	var res []filesystem.FileEntry
	for _, e := range m.entries {
		if _, ok := m.selected[e.Path]; ok {
			res = append(res, e)
		}
	}
	return res
}

// ── Size / focus setters ───────────────────────────────────────────────

func (m *Model) SetSize(w, h int)  { m.width = w; m.height = h }
func (m *Model) SetFocused(f bool) { m.focused = f }

// ── Navigation ─────────────────────────────────────────────────────────

// NavigateTo goes to a directory and records where it came from, so
// Back returns there. Going anywhere new drops the forward half of the
// history, the way a browser does.
func (m Model) NavigateTo(path string) (Model, tea.Cmd) {
	if path == m.currentPath {
		// Already here: reload in place rather than stacking a history
		// entry that Back would have to step over.
		loadCmd := m.startLoad(m.cursorName(), m.cursor, m.offset)
		return m, tea.Batch(loadCmd, func() tea.Msg { return DirChangedMsg{Path: path} })
	}
	m.back = pushLocation(m.back, m.here())
	m.forward = nil
	return m.jump(path, "", 0, 0)
}

// Back returns to the previous directory in the history.
func (m Model) Back() (Model, tea.Cmd) {
	n := len(m.back)
	if n == 0 {
		return m, nil
	}
	prev := m.back[n-1]
	m.back = m.back[:n-1]
	m.forward = pushLocation(m.forward, m.here())
	return m.jump(prev.path, prev.name, prev.cursor, prev.offset)
}

// Forward undoes a Back.
func (m Model) Forward() (Model, tea.Cmd) {
	n := len(m.forward)
	if n == 0 {
		return m, nil
	}
	next := m.forward[n-1]
	m.forward = m.forward[:n-1]
	m.back = pushLocation(m.back, m.here())
	return m.jump(next.path, next.name, next.cursor, next.offset)
}

// here describes the current position, for the history stacks.
func (m Model) here() location {
	return location{path: m.currentPath, name: m.cursorName(), cursor: m.cursor, offset: m.offset}
}

func (m Model) cursorName() string {
	if e := m.SelectedEntry(); e != nil {
		return e.Name
	}
	return ""
}

func pushLocation(stack []location, loc location) []location {
	stack = append(stack, loc)
	if len(stack) > historyLimit {
		stack = append([]location(nil), stack[len(stack)-historyLimit:]...)
	}
	return stack
}

// jump switches directory without touching the history; the callers
// above decide what gets recorded. selectName is the entry to land on,
// cursor and offset where to land if it is gone.
func (m Model) jump(path, selectName string, cursor, offset int) (Model, tea.Cmd) {
	m.currentPath = path
	m.cursor = 0
	m.offset = 0
	m.filter = ""
	m.filtering = false
	m.entries = nil
	m.allEntries = nil
	// Marks belong to the directory they were made in. Carrying them
	// along would leave the bulk operations acting on entries that are
	// no longer on screen — or, worse, on nothing at all.
	m.selected = make(map[string]struct{})
	loadCmd := m.startLoad(selectName, cursor, offset)

	return m, tea.Batch(loadCmd, func() tea.Msg { return DirChangedMsg{Path: path} })
}

// ── Internal helpers ───────────────────────────────────────────────────

// loadNow reads the directory on the spot. Only startup uses it: there
// is nothing on screen yet to keep responsive, and the first directory
// has to be there before the first frame is drawn.
func (m *Model) loadNow() {
	entries, err := filesystem.ReadDir(m.currentPath, m.showHidden)
	if err != nil {
		m.err = err
		m.allEntries = nil
		m.entries = nil
		return
	}
	m.err = nil
	filesystem.SortEntries(entries, m.sortMode)
	m.allEntries = entries
	m.applyFilter()
}

// startLoad kicks off a read of the current directory. selectName is the
// entry to land the cursor on when the read finishes; cursor and offset
// are where to land if that name is gone.
//
// The previous contents stay on screen until the new ones arrive, which
// is what makes a slow directory feel like a pause rather than a blank
// panel.
func (m *Model) startLoad(selectName string, cursor, offset int) tea.Cmd {
	m.loadToken++
	m.loading = true
	m.pendingSelect = selectName
	m.pendingCursor = cursor
	m.pendingOffset = offset

	id := m.id
	token := m.loadToken
	path := m.currentPath
	showHidden := m.showHidden
	sortMode := m.sortMode

	return func() tea.Msg {
		entries, err := filesystem.ReadDir(path, showHidden)
		if err == nil {
			filesystem.SortEntries(entries, sortMode)
		}
		return EntriesLoadedMsg{ListID: id, Path: path, Token: token, Entries: entries, Err: err}
	}
}

// applyFilter derives the visible list from the directory contents.
func (m *Model) applyFilter() {
	if m.filter == "" {
		m.entries = m.allEntries
		return
	}
	lower := strings.ToLower(m.filter)
	filtered := make([]filesystem.FileEntry, 0, len(m.allEntries))
	for _, e := range m.allEntries {
		if strings.Contains(strings.ToLower(e.Name), lower) {
			filtered = append(filtered, e)
		}
	}
	m.entries = filtered
}

// placeCursor restores the cursor after the list contents changed.
func (m *Model) placeCursor(selectName string, cursor, offset int) {
	if selectName != "" {
		for i, e := range m.entries {
			if e.Name == selectName {
				m.cursor = i
				m.offset = offset
				m.fixScroll()
				return
			}
		}
	}
	m.cursor = cursor
	m.offset = offset
	if m.cursor >= len(m.entries) {
		m.cursor = max(0, len(m.entries)-1)
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.fixScroll()
}

func (m *Model) fixScroll() {
	vh := m.visibleHeight()
	if vh <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+vh {
		m.offset = m.cursor - vh + 1
	}
}

func (m Model) visibleHeight() int {
	return max(1, m.height-1) // reserve 1 for the column header
}

// hasGitState reports whether the list currently has git status data.
func (m Model) hasGitState() bool {
	return m.gitFileStat != nil
}

// Column widths. The size column is fixed; the Modified column depends
// on the configured format, because an ISO timestamp needs four more
// cells than "Jan 02 15:04" and a column that is too narrow does not
// truncate — lipgloss wraps it into the row below.
const sizeColW = 8

func dateColW() int {
	if config.Get().Files.TimeFormat == "iso" {
		return 16 // 2006-01-02 15:04
	}
	return 12
}

func (m Model) nameWidth() int {
	// " " icon(1) " " name " " [git(1) " "] size " " date
	fixed := 3 + sizeColW + 1 + dateColW() + 1
	if m.hasGitState() {
		fixed += 2
	}
	return max(10, m.width-fixed)
}

// renderGitMarker returns a 1-cell coloured marker character for the given
// entry, or " " when there is no git change associated with it.
func (m Model) renderGitMarker(entry filesystem.FileEntry) string {
	if entry.IsDir {
		if _, ok := m.gitDirHas[entry.Path]; ok {
			return lipgloss.NewStyle().Foreground(theme.AccentMagenta).Bold(true).Width(1).MaxWidth(1).Render("●")
		}
		return " "
	}
	code, ok := m.gitFileStat[entry.Path]
	if !ok {
		return " "
	}
	ch, col := gitMarkerChar(code)
	return lipgloss.NewStyle().Foreground(col).Bold(true).Width(1).MaxWidth(1).Render(string(ch))
}

// gitMarkerInfo returns the display character and foreground colour for a git
// marker without rendering it, so callers can compose the background themselves
// (e.g. cursor rows need BgSelected applied to each segment).
func (m Model) gitMarkerInfo(entry filesystem.FileEntry) (string, lipgloss.TerminalColor) {
	if entry.IsDir {
		if _, ok := m.gitDirHas[entry.Path]; ok {
			return "●", theme.AccentMagenta
		}
		return " ", theme.FgDimColor
	}
	code, ok := m.gitFileStat[entry.Path]
	if !ok {
		return " ", theme.FgDimColor
	}
	ch, col := gitMarkerChar(code)
	return string(ch), col
}

// gitNameColor is the colour a changed file's name is drawn in, and
// whether it has one at all. Directories keep their own colour — the
// marker column already says there is something inside.
func (m Model) gitNameColor(entry filesystem.FileEntry) (lipgloss.TerminalColor, bool) {
	if entry.IsDir {
		return nil, false
	}
	code, ok := m.gitFileStat[entry.Path]
	if !ok || len(code) < 2 {
		return nil, false
	}
	if code == "??" {
		return theme.GitUntracked, true
	}
	ch, col := gitMarkerChar(code)
	if ch == ' ' {
		return nil, false
	}
	return col, true
}

func gitMarkerChar(code string) (rune, lipgloss.TerminalColor) {
	if len(code) < 2 {
		return ' ', theme.FgDimColor
	}
	idx, wt := code[0], code[1]
	switch {
	case idx == '?' && wt == '?':
		return '?', theme.FgDimColor
	case idx == 'A':
		return 'A', theme.AccentGreen
	case idx == 'D' || wt == 'D':
		return 'D', theme.AccentRed
	case idx == 'R':
		return 'R', theme.AccentMagenta
	case idx == 'M' || wt == 'M':
		return 'M', theme.AccentBlue
	case idx != ' ':
		return rune(idx), theme.AccentYellow
	case wt != ' ':
		return rune(wt), theme.AccentYellow
	}
	return ' ', theme.FgDimColor
}

// ── Update ─────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	// Keyboard and mouse input only belongs to the list while it holds
	// focus; state messages (reload, sort, settings) apply regardless of
	// which panel the user is looking at.
	switch msg.(type) {
	case tea.KeyMsg, tea.MouseMsg:
		if !m.focused {
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case EntriesLoadedMsg:
		// Ignore results for a directory we have already left, or from a
		// read that a newer one has superseded.
		if msg.ListID != m.id || msg.Token != m.loadToken || msg.Path != m.currentPath {
			return m, nil
		}
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			m.allEntries = nil
			m.entries = nil
			return m, nil
		}
		m.err = nil
		m.allEntries = msg.Entries
		m.applyFilter()
		m.placeCursor(m.pendingSelect, m.pendingCursor, m.pendingOffset)
		m.pendingSelect = ""
		return m, nil
	case RefreshListMsg:
		var selectedName string
		if e := m.SelectedEntry(); e != nil {
			selectedName = e.Name
		}
		return m, m.startLoad(selectedName, m.cursor, m.offset)
	case SetShowHiddenMsg:
		if m.showHidden == msg.Show {
			return m, nil
		}
		m.showHidden = msg.Show
		var selectedName string
		if e := m.SelectedEntry(); e != nil {
			selectedName = e.Name
		}
		return m, m.startLoad(selectedName, m.cursor, m.offset)
	case SortListMsg:
		// Re-selecting the current order is a no-op rather than a jump
		// back to the top: settings changes broadcast this message on
		// every edit, and none of them should move the cursor.
		if m.sortMode == msg.Mode {
			return m, nil
		}
		m.sortMode = msg.Mode
		var selectedName string
		if e := m.SelectedEntry(); e != nil {
			selectedName = e.Name
		}
		return m, m.startLoad(selectedName, 0, 0)
	case SetDirSizeMsg:
		if m.dirSizes == nil {
			m.dirSizes = make(map[string]int64)
		}
		m.dirSizes[msg.Path] = msg.Size
		return m, nil
	case ClearSelectionMsg:
		m.selected = make(map[string]struct{})
		return m, nil
	case tea.MouseMsg:
		if msg.Type == tea.MouseWheelUp && m.cursor > 0 {
			m.cursor--
			m.fixScroll()
		} else if msg.Type == tea.MouseWheelDown && m.cursor < len(m.entries)-1 {
			m.cursor++
			m.fixScroll()
		}
		return m, nil
	case tea.KeyMsg:
		if m.filtering {
			return m.updateFilter(msg)
		}
		return m.updateNavigation(msg)
	}

	return m, nil
}

func (m Model) updateFilter(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filter = ""
		m.cursor = 0
		m.offset = 0
		m.applyFilter()
	case "enter":
		m.filtering = false
	case "backspace":
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m.cursor = 0
			m.offset = 0
			m.applyFilter()
		}
	default:
		r := msg.String()
		if len(r) == 1 && r[0] >= 32 {
			m.filter += r
			m.cursor = 0
			m.offset = 0
			m.applyFilter()
		}
	}
	return m, nil
}

func (m Model) updateNavigation(msg tea.KeyMsg) (Model, tea.Cmd) {
	action, bound := keys.Match(keys.ScopeList, msg.String())
	if !bound {
		return m, nil
	}
	return m.Do(action)
}

// Do carries out a file-list action. Keystrokes arrive here through the
// keymap; the command palette and the Cmd shortcuts call it directly,
// which is why it does not care whether the list has focus.
func (m Model) Do(action keys.Action) (Model, tea.Cmd) {
	switch action {
	case keys.CursorUp:
		if m.cursor > 0 {
			m.cursor--
			m.fixScroll()
		}
	case keys.CursorDown:
		if m.cursor < len(m.entries)-1 {
			m.cursor++
			m.fixScroll()
		}
	case keys.Open:
		// For audio files, enterSelected emits OpenFileRequestMsg; the
		// app layer then toggles playback when the same audio is already
		// loaded in the preview pane.
		return m.enterSelected()
	case keys.Parent:
		return m.goUp()
	case keys.HomeDir:
		return m.NavigateTo(filesystem.HomeDir())
	case keys.ToggleHidden:
		m.showHidden = !m.showHidden
		var selectedName string
		if e := m.SelectedEntry(); e != nil {
			selectedName = e.Name
		}
		return m, m.startLoad(selectedName, m.cursor, m.offset)
	case keys.Filter:
		m.filtering = true
		m.filter = ""
	case keys.Top:
		m.cursor = 0
		m.offset = 0
	case keys.Bottom:
		if len(m.entries) > 0 {
			m.cursor = len(m.entries) - 1
			m.fixScroll()
		}
	case keys.HalfPageDown:
		half := m.visibleHeight() / 2
		m.cursor = min(m.cursor+half, max(0, len(m.entries)-1))
		m.fixScroll()
	case keys.HalfPageUp:
		half := m.visibleHeight() / 2
		m.cursor = max(m.cursor-half, 0)
		m.fixScroll()
	case keys.Delete:
		if entries := m.SelectedEntries(); len(entries) > 0 {
			return m, func() tea.Msg { return DeleteRequestMsg{Entries: entries} }
		}
	case keys.Copy:
		if entries := m.SelectedEntries(); len(entries) > 0 {
			return m, func() tea.Msg { return CopyMsg{Entries: entries} }
		}
	case keys.Cut:
		if entries := m.SelectedEntries(); len(entries) > 0 {
			return m, func() tea.Msg { return CutMsg{Entries: entries} }
		}
	case keys.Paste:
		return m, func() tea.Msg { return PasteRequestMsg{} }
	case keys.Mark:
		if entry := m.SelectedEntry(); entry != nil {
			if _, ok := m.selected[entry.Path]; ok {
				delete(m.selected, entry.Path)
			} else {
				m.selected[entry.Path] = struct{}{}
			}
			if m.cursor < len(m.entries)-1 {
				m.cursor++
				m.fixScroll()
			}
		}
	case keys.SelectAll:
		for _, e := range m.entries {
			m.selected[e.Path] = struct{}{}
		}
	case keys.InvertMarks:
		for _, e := range m.entries {
			if _, ok := m.selected[e.Path]; ok {
				delete(m.selected, e.Path)
			} else {
				m.selected[e.Path] = struct{}{}
			}
		}
	case keys.ClearMarks:
		m.selected = make(map[string]struct{})
		if m.filter != "" {
			m.filtering = false
			m.filter = ""
			name := m.cursorName()
			m.applyFilter()
			m.placeCursor(name, 0, 0)
		}
	case keys.Duplicate:
		if entries := m.SelectedEntries(); len(entries) > 0 {
			return m, func() tea.Msg { return DuplicateRequestMsg{Entries: entries} }
		}
	case keys.CopyPath:
		entries := m.SelectedEntries()
		if len(entries) == 0 {
			// An empty directory still has a path worth copying.
			dir := m.currentPath
			return m, func() tea.Msg { return CopyPathMsg{Paths: []string{dir}} }
		}
		paths := make([]string, 0, len(entries))
		for _, e := range entries {
			paths = append(paths, e.Path)
		}
		return m, func() tea.Msg { return CopyPathMsg{Paths: paths} }
	case keys.OpenExternal, keys.Reveal:
		target := m.currentPath
		if e := m.SelectedEntry(); e != nil {
			target = e.Path
		}
		reveal := action == keys.Reveal
		return m, func() tea.Msg { return OpenExternalMsg{Path: target, Reveal: reveal} }
	case keys.OpenInTab:
		// A folder opens as itself; on a file the useful reading is
		// "another tab right here".
		target := m.currentPath
		if e := m.SelectedEntry(); e != nil && e.IsDir {
			target = e.Path
		}
		return m, func() tea.Msg { return OpenInTabMsg{Path: target} }
	case keys.Rename:
		if entry := m.SelectedEntry(); entry != nil {
			return m, func() tea.Msg { return RenameRequestMsg{Entry: *entry} }
		}
	case keys.NewFile:
		return m, func() tea.Msg { return CreateFileRequestMsg{} }
	case keys.NewDir:
		return m, func() tea.Msg { return CreateDirRequestMsg{} }
	case keys.BulkRename:
		// Marked entries if there are any, otherwise the whole
		// directory — the same rule the other bulk operations use.
		names := m.bulkRenameNames()
		if len(names) == 0 {
			return m, nil
		}
		dir := m.currentPath
		return m, func() tea.Msg { return BulkRenameRequestMsg{Dir: dir, Names: names} }
	case keys.DirSize:
		var paths []string
		for _, e := range m.SelectedEntries() {
			if e.IsDir {
				paths = append(paths, e.Path)
			}
		}
		if len(paths) == 0 {
			return m, nil
		}
		return m, func() tea.Msg { return DirSizeRequestMsg{Paths: paths} }
	case keys.Zip:
		entries := m.SelectedEntries()
		if len(entries) == 0 {
			return m, nil
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name)
		}
		dir := m.currentPath
		return m, func() tea.Msg { return ArchiveRequestMsg{Dir: dir, Names: names} }
	case keys.Extract:
		entry := m.SelectedEntry()
		if entry == nil || entry.IsDir {
			return m, nil
		}
		p := entry.Path
		return m, func() tea.Msg { return ExtractRequestMsg{Path: p} }
	}

	// ── Git-aware file operations (only inside a repo) ─────────────
	if m.hasGitState() {
		entry := m.SelectedEntry()
		if entry != nil && !entry.IsDir {
			switch action {
			case keys.GitStage:
				p := entry.Path
				return m, func() tea.Msg { return GitStageMsg{Path: p} }
			case keys.GitUnstage:
				p := entry.Path
				return m, func() tea.Msg { return GitUnstageMsg{Path: p} }
			case keys.GitDiscard:
				p := entry.Path
				return m, func() tea.Msg { return GitDiscardMsg{Path: p} }
			case keys.GitFileHistory:
				p := entry.Path
				return m, func() tea.Msg { return GitFileHistoryMsg{Path: p} }
			case keys.GitBlame:
				p := entry.Path
				return m, func() tea.Msg { return GitBlameMsg{Path: p} }
			}
		}
		if entry != nil && action == keys.GitIgnore {
			p := entry.Path
			return m, func() tea.Msg { return GitIgnoreMsg{Path: p} }
		}
	}

	return m, nil
}

func (m Model) enterSelected() (Model, tea.Cmd) {
	entry := m.SelectedEntry()
	if entry == nil {
		return m, nil
	}
	if entry.IsDir {
		return m.NavigateTo(entry.Path)
	}
	return m, func() tea.Msg { return OpenFileRequestMsg{Path: entry.Path} }
}

// goUp opens the enclosing directory and lands on the one just left.
// It is a navigation like any other, so Back leads into the child again.
func (m Model) goUp() (Model, tea.Cmd) {
	parent := filepath.Dir(m.currentPath)
	if parent == m.currentPath {
		return m, nil
	}
	childName := filepath.Base(m.currentPath)

	// Coming back out the way we went in: put the scroll position back
	// too, so the listing looks exactly as it did before the detour.
	offset := 0
	if n := len(m.back); n > 0 && m.back[n-1].path == parent {
		offset = m.back[n-1].offset
	}

	m.back = pushLocation(m.back, m.here())
	m.forward = nil
	return m.jump(parent, childName, 0, offset)
}

// ── Mouse ──────────────────────────────────────────────────────────────

// EntryAt maps a row of the panel to an entry index. Row 0 is the column
// header, so the first entry sits on row 1.
func (m Model) EntryAt(row int) (int, bool) {
	idx := m.offset + row - 1
	if row < 1 || idx < 0 || idx >= len(m.entries) {
		return 0, false
	}
	return idx, true
}

// SortAt tells which sort order a click on the column header asks for,
// x being the column inside the panel.
func (m Model) SortAt(x int) filesystem.SortMode {
	sizeStart := 3 + m.nameWidth() + 1
	if m.hasGitState() {
		sizeStart += 2
	}
	switch {
	case x < sizeStart:
		return filesystem.SortByName
	case x < sizeStart+sizeColW+1:
		return filesystem.SortBySize
	}
	return filesystem.SortByDate
}

// WithCursor puts the cursor on an entry, as a click does.
func (m Model) WithCursor(idx int) Model {
	if idx >= 0 && idx < len(m.entries) {
		m.cursor = idx
		m.fixScroll()
	}
	return m
}

// ToggleMarkAt marks or unmarks an entry without moving on to the next
// one — the Cmd-click of a file manager.
func (m Model) ToggleMarkAt(idx int) Model {
	if idx < 0 || idx >= len(m.entries) {
		return m
	}
	path := m.entries[idx].Path
	if _, ok := m.selected[path]; ok {
		delete(m.selected, path)
	} else {
		m.selected[path] = struct{}{}
	}
	m.cursor = idx
	m.fixScroll()
	return m
}

// OpenSelected enters the directory or opens the file under the cursor.
func (m Model) OpenSelected() (Model, tea.Cmd) { return m.enterSelected() }

// ── View ───────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	var lines []string

	// One config read per frame rather than one per row — the setting
	// cannot change halfway through drawing a list.
	showIcons := config.Get().Files.Icons

	// Column header
	lines = append(lines, m.renderHeader())

	if m.err != nil {
		errLine := lipgloss.NewStyle().Foreground(theme.AccentRed).
			Render(" Error: " + m.err.Error())
		lines = append(lines, errLine)
	} else if len(m.entries) == 0 {
		if m.loading {
			lines = append(lines, theme.Dim.Render(" reading…"))
		} else {
			lines = append(lines, theme.Dim.Render(" (empty directory)"))
		}
	} else {
		vh := m.visibleHeight()
		end := min(m.offset+vh, len(m.entries))

		for i := m.offset; i < end; i++ {
			lines = append(lines, m.renderEntry(i, showIcons))
		}
	}

	// Filter bar (shown while filtering)
	if m.filtering {
		filterLine := lipgloss.NewStyle().Foreground(theme.AccentBlue).Bold(true).Render("/") +
			lipgloss.NewStyle().Foreground(theme.FgColor).Render(m.filter) +
			lipgloss.NewStyle().Foreground(theme.AccentBlue).Render("█")
		lines = append(lines, filterLine)
	}

	// Normalise every line to exactly m.width visual cells and to a
	// single terminal row. This protects against:
	//   1. Embedded newlines / control chars in any rendered segment
	//      (would otherwise create spurious extra rows in the panel).
	//   2. Per-row width drift caused by ambiguous-width characters
	//      that the terminal renders as 2 cells but lipgloss counts as 1.
	// Both of those issues break Bubble Tea's differential renderer and
	// cause partial-name / blank-row artifacts after cursor movement.
	for i, line := range lines {
		// Collapse any newlines/carriage returns so that one logical
		// list row always equals exactly one terminal row.
		if strings.ContainsAny(line, "\n\r") {
			line = strings.ReplaceAll(line, "\r", "")
			line = strings.ReplaceAll(line, "\n", " ")
		}
		w := lipgloss.Width(line)
		switch {
		case w < m.width:
			line += strings.Repeat(" ", m.width-w)
		case w > m.width:
			// Truncate down to m.width while preserving ANSI codes.
			line = lipgloss.NewStyle().MaxWidth(m.width).Render(line)
		}
		lines[i] = line
	}
	// Pad the block up to m.height rows so any leftover cells from a
	// previous (taller) frame get fully overwritten.
	if m.height > 0 {
		blank := strings.Repeat(" ", m.width)
		for len(lines) < m.height {
			lines = append(lines, blank)
		}
		if len(lines) > m.height {
			lines = lines[:m.height]
		}
	}
	return strings.Join(lines, "\n")
}

// renderHeader draws the column titles. The column the list is sorted by
// is written in the text colour and carries an arrow — ▾ for the usual
// direction, ▴ when the order is reversed — the way Finder marks it.
func (m Model) renderHeader() string {
	nameW := m.nameWidth()
	_, reversed := filesystem.SortOptions()
	arrow := " ▾"
	if reversed {
		arrow = " ▴"
	}

	title := func(label string, modes ...filesystem.SortMode) (string, lipgloss.Style) {
		for _, mode := range modes {
			if m.sortMode == mode {
				return label + arrow, theme.ListHeaderActive
			}
		}
		return label, theme.ListHeader
	}

	nameLabel, nameStyle := title("Name", filesystem.SortByName)
	if m.sortMode == filesystem.SortByType {
		nameLabel, nameStyle = "Name · by type"+arrow, theme.ListHeaderActive
	}
	sizeLabel, sizeStyle := title("Size", filesystem.SortBySize)
	dateLabel, dateStyle := title("Modified", filesystem.SortByDate)

	name := nameStyle.Width(nameW).MaxWidth(nameW).Render(nameLabel)
	size := sizeStyle.Width(sizeColW).Align(lipgloss.Right).Render(sizeLabel)
	date := dateStyle.Width(dateColW()).MaxWidth(dateColW()).Render(dateLabel)
	if m.hasGitState() {
		// Layout mirrors the data row: " " icon " " name " " git(1) " " size " " date
		return fmt.Sprintf("   %s   %s %s", name, size, date)
	}
	// " " icon(1) " " name " " size " " date
	return fmt.Sprintf("   %s %s %s", name, size, date)
}

func (m Model) renderEntry(idx int, showIcons bool) string {
	entry := m.entries[idx]
	isSelected := idx == m.cursor
	_, isMarked := m.selected[entry.Path]
	icon := icons.GetIcon(entry.Name, entry.Extension, entry.IsDir, entry.IsExec, entry.IsSymlink)

	// Prepare the name. Strip any control characters (newlines, tabs, CR,
	// other low-byte bytes) so that a single entry can never expand into
	// multiple terminal rows or move the cursor unexpectedly. Without this,
	// a file whose name contains a `\n` would split its row in half and
	// shift the layout of every entry below it.
	name := sanitizeName(entry.Name)
	if entry.IsDir {
		name += "/"
	}

	nameW := m.nameWidth()

	// File size
	var sizeStr string
	if entry.IsDir {
		if size, measured := m.dirSizes[entry.Path]; measured {
			sizeStr = filesystem.FormatSize(size)
		} else {
			sizeStr = "--"
		}
	} else {
		sizeStr = filesystem.FormatSize(entry.Size)
	}

	// Date
	dateStr := filesystem.FormatTime(entry.ModTime)
	dateW := dateColW()

	// Icon with fixed 1-cell width. Turning icons off keeps the cell —
	// the header and every other row are aligned to it, so blanking the
	// symbol is all that is needed.
	iconCell := lipgloss.NewStyle().Foreground(icon.Color).Width(1).MaxWidth(1).Render(icon.Symbol)
	if !showIcons {
		iconCell = " "
	}

	// Git marker on the right side (between name and size)
	showGit := m.hasGitState()
	var gitMark string
	if showGit {
		gitMark = m.renderGitMarker(entry)
	}

	// buildLine assembles: icon name [git] size date
	buildLine := func(ic, nm, sz, dt string) string {
		if showGit {
			return fmt.Sprintf(" %s %s %s %s %s", ic, nm, gitMark, sz, dt)
		}
		return fmt.Sprintf(" %s %s %s %s", ic, nm, sz, dt)
	}

	// A marked row trades its icon for a tick, and keeps it under the
	// cursor: the cursor must never hide what is and is not marked.
	iconSymbol, iconColor := icon.Symbol, lipgloss.TerminalColor(icon.Color)
	cursorNameColor := lipgloss.TerminalColor(theme.FgColor)
	if isMarked {
		iconSymbol, iconColor = "✓", theme.AccentYellow
		cursorNameColor = theme.AccentYellow
	}

	// ── Selected row (solid background, per-segment) ─────────────────
	// We apply the background to every segment individually so that inner
	// ANSI foreground codes are not reset by an outer Render() wrapper, and
	// MaxWidth is never needed (the math guarantees exactly m.width cells).
	if isSelected && m.focused {
		applyBg := func(s lipgloss.Style) lipgloss.Style {
			return s.Background(theme.BgSelected).Bold(true)
		}
		sp := applyBg(lipgloss.NewStyle()).Render(" ")
		iconStr := applyBg(lipgloss.NewStyle().Foreground(iconColor)).Width(1).MaxWidth(1).Render(iconSymbol)
		nameStr := applyBg(lipgloss.NewStyle().Foreground(cursorNameColor)).Width(nameW).Render(truncate(name, nameW))
		sizeRend := applyBg(lipgloss.NewStyle()).Width(sizeColW).Align(lipgloss.Right).
			MaxWidth(sizeColW).Render(truncate(sizeStr, sizeColW))
		dateRend := applyBg(lipgloss.NewStyle()).Width(dateW).MaxWidth(dateW).
			Render(truncate(dateStr, dateW))
		if showGit {
			ch, col := m.gitMarkerInfo(entry)
			markStr := applyBg(lipgloss.NewStyle().Foreground(col)).Bold(true).Width(1).MaxWidth(1).Render(ch)
			return sp + iconStr + sp + nameStr + sp + markStr + sp + sizeRend + sp + dateRend
		}
		return sp + iconStr + sp + nameStr + sp + sizeRend + sp + dateRend
	}

	// ── Unfocused cursor (subtle highlight, per-segment) ──────────────
	if isSelected {
		applyBg := func(s lipgloss.Style) lipgloss.Style {
			return s.Background(theme.BgHighlight).Foreground(theme.FgColor)
		}
		sp := applyBg(lipgloss.NewStyle()).Render(" ")
		iconStr := applyBg(lipgloss.NewStyle()).Foreground(iconColor).Width(1).MaxWidth(1).Render(iconSymbol)
		nameStr := applyBg(lipgloss.NewStyle()).Foreground(cursorNameColor).Width(nameW).Render(truncate(name, nameW))
		sizeRend := applyBg(lipgloss.NewStyle()).Width(sizeColW).Align(lipgloss.Right).
			MaxWidth(sizeColW).Render(truncate(sizeStr, sizeColW))
		dateRend := applyBg(lipgloss.NewStyle()).Width(dateW).MaxWidth(dateW).
			Render(truncate(dateStr, dateW))
		if showGit {
			ch, col := m.gitMarkerInfo(entry)
			markStr := applyBg(lipgloss.NewStyle().Foreground(col)).Bold(true).Width(1).MaxWidth(1).Render(ch)
			return sp + iconStr + sp + nameStr + sp + markStr + sp + sizeRend + sp + dateRend
		}
		return sp + iconStr + sp + nameStr + sp + sizeRend + sp + dateRend
	}

	// ── Normal row (per-element colors) ──────────────────────────────
	var nameStyle lipgloss.Style
	switch {
	case entry.IsDir:
		nameStyle = theme.DirName
	case entry.IsExec:
		nameStyle = theme.ExecName
	case entry.IsSymlink:
		nameStyle = theme.SymlinkName
	case entry.IsHidden:
		nameStyle = theme.HiddenName
	default:
		nameStyle = theme.FileName
	}

	// Inside a repository a changed file takes the colour of its status,
	// the way a JetBrains project tree paints it: the eye finds what
	// changed without reading the marker column.
	if col, changed := m.gitNameColor(entry); changed {
		nameStyle = lipgloss.NewStyle().Foreground(col)
	}

	nameStr := nameStyle.Width(nameW).MaxWidth(nameW).Render(name)
	sizeRendered := theme.FileSize.Width(sizeColW).Align(lipgloss.Right).
		MaxWidth(sizeColW).Render(truncate(sizeStr, sizeColW))

	// Visual indicator for marked (multi-selected) rows
	if isMarked {
		iconCell = lipgloss.NewStyle().Foreground(theme.AccentYellow).Width(1).MaxWidth(1).Render("✓")
		nameStr = lipgloss.NewStyle().Foreground(theme.AccentYellow).Bold(true).Width(nameW).MaxWidth(nameW).Render(name)
	}

	dateRendered := theme.FileDate.Width(dateW).MaxWidth(dateW).Render(truncate(dateStr, dateW))

	return buildLine(iconCell, nameStr, sizeRendered, dateRendered)
}

// ── String helpers ─────────────────────────────────────────────────────

// sanitizeName replaces control characters (newline, CR, tab, other bytes
// below 0x20, plus DEL) with a visible placeholder so that a filename
// cannot break the row layout by inserting line breaks or moving the
// terminal cursor mid-render.
func sanitizeName(s string) string {
	if s == "" {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}

func truncate(s string, maxW int) string {
	if lipgloss.Width(s) <= maxW {
		return s
	}
	// Trim rune-by-rune until it fits (safe for Unicode)
	runes := []rune(s)
	for len(runes) > 0 {
		candidate := string(runes[:len(runes)-1]) + "..."
		if lipgloss.Width(candidate) <= maxW {
			return candidate
		}
		runes = runes[:len(runes)-1]
	}
	return "..."
}
