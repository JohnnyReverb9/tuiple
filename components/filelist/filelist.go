// Package filelist implements the central file-listing panel.
package filelist

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/filesystem"
	"tuiple/icons"
	"tuiple/theme"
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

// ClearSelectionMsg tells the list to drop its active selections.
type ClearSelectionMsg struct{}

// RefreshListMsg asks the list to reload entries.
type RefreshListMsg struct{}

// SortListMsg asks the list to reload with a specific sort mode.
type SortListMsg struct {
	Mode filesystem.SortMode
}


// ── Model ──────────────────────────────────────────────────────────────

type historyEntry struct {
	path   string
	cursor int
	offset int
}

// Model is the file-list Bubble Tea model.
type Model struct {
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

	history  []historyEntry
	selected map[string]struct{}
	err      error

	gitFileStat map[string]string   // abs path -> 2-char porcelain code
	gitDirHas   map[string]struct{} // dirs that contain changes (abs path)
}

// SetGitState injects git status data so the list can render per-row
// markers. Passing nil maps clears the markers.
func (m *Model) SetGitState(fileStat map[string]string, dirHas map[string]struct{}) {
	m.gitFileStat = fileStat
	m.gitDirHas = dirHas
}

// New creates a file-list rooted at the given path.
func New(path string) Model {
	m := Model{
		currentPath: path,
		sortMode:    filesystem.SortByName,
		focused:     true,
		selected:    make(map[string]struct{}),
	}
	m.loadEntries()
	return m
}

// ── Public accessors ───────────────────────────────────────────────────

func (m Model) CursorIdx() int                    { return m.cursor }
func (m Model) CurrentPath() string                { return m.currentPath }
func (m Model) Entries() []filesystem.FileEntry    { return m.entries }
func (m Model) IsFiltering() bool                  { return m.filtering }

// SelectByName positions the cursor on the entry with the given name.
func (m Model) SelectByName(name string) Model {
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

func (m *Model) SetSize(w, h int) { m.width = w; m.height = h }
func (m *Model) SetFocused(f bool) { m.focused = f }

// ── NavigateTo ─────────────────────────────────────────────────────────

// NavigateTo pushes the current dir onto the history stack and navigates.
func (m Model) NavigateTo(path string) (Model, tea.Cmd) {
	m.history = append(m.history, historyEntry{
		path: m.currentPath, cursor: m.cursor, offset: m.offset,
	})
	m.currentPath = path
	m.cursor = 0
	m.offset = 0
	m.filter = ""
	m.filtering = false
	m.loadEntries()

	return m, func() tea.Msg { return DirChangedMsg{Path: path} }
}

// ── Internal helpers ───────────────────────────────────────────────────

func (m *Model) loadEntries() {
	entries, err := filesystem.ReadDir(m.currentPath, m.showHidden)
	if err != nil {
		m.err = err
		m.entries = nil
		return
	}
	m.err = nil

	if m.filter != "" {
		lower := strings.ToLower(m.filter)
		var filtered []filesystem.FileEntry
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.Name), lower) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	filesystem.SortEntries(entries, m.sortMode)
	m.entries = entries
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

func (m Model) nameWidth() int {
	if m.hasGitState() {
		// Layout: " " icon(1) " " name " " git(1) " " size(8) " " date(12) → fixed=27
		return max(10, m.width-27)
	}
	// Layout: " " icon(1) " " name " " size(8) " " date(12) → fixed=25
	return max(10, m.width-25)
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
	if !m.focused {
		return m, nil
	}

	switch msg := msg.(type) {
	case RefreshListMsg:
		var selectedName string
		if e := m.SelectedEntry(); e != nil {
			selectedName = e.Name
		}
		m.loadEntries()
		if selectedName != "" {
			for i, e := range m.entries {
				if e.Name == selectedName {
					m.cursor = i
					break
				}
			}
		}
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		m.fixScroll()
		return m, nil
	case SortListMsg:
		m.sortMode = msg.Mode
		m.cursor = 0
		m.offset = 0
		m.loadEntries()
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
		m.loadEntries()
	case "enter":
		m.filtering = false
	case "backspace":
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m.cursor = 0
			m.offset = 0
			m.loadEntries()
		}
	default:
		r := msg.String()
		if len(r) == 1 && r[0] >= 32 {
			m.filter += r
			m.cursor = 0
			m.offset = 0
			m.loadEntries()
		}
	}
	return m, nil
}

func (m Model) updateNavigation(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.fixScroll()
		}
	case "down", "j":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
			m.fixScroll()
		}
	case "enter", "right", "l":
		// For audio files, enterSelected emits OpenFileRequestMsg; the
		// app layer then toggles playback when the same audio is already
		// loaded in the preview pane.
		return m.enterSelected()
	case "backspace", "left", "h":
		return m.goUp()
	case "~":
		return m.NavigateTo(filesystem.HomeDir())
	case ".":
		m.showHidden = !m.showHidden
		m.loadEntries()
		if m.cursor >= len(m.entries) {
			m.cursor = max(0, len(m.entries)-1)
		}
		m.fixScroll()
	case "/":
		m.filtering = true
		m.filter = ""
	case "g":
		m.cursor = 0
		m.offset = 0
	case "G":
		if len(m.entries) > 0 {
			m.cursor = len(m.entries) - 1
			m.fixScroll()
		}
	case "ctrl+d":
		half := m.visibleHeight() / 2
		m.cursor = min(m.cursor+half, max(0, len(m.entries)-1))
		m.fixScroll()
	case "ctrl+u":
		half := m.visibleHeight() / 2
		m.cursor = max(m.cursor-half, 0)
		m.fixScroll()
	case "d":
		if entries := m.SelectedEntries(); len(entries) > 0 {
			return m, func() tea.Msg { return DeleteRequestMsg{Entries: entries} }
		}
	case "c":
		if entries := m.SelectedEntries(); len(entries) > 0 {
			return m, func() tea.Msg { return CopyMsg{Entries: entries} }
		}
	case "x":
		if entries := m.SelectedEntries(); len(entries) > 0 {
			return m, func() tea.Msg { return CutMsg{Entries: entries} }
		}
	case "p":
		return m, func() tea.Msg { return PasteRequestMsg{} }
	case " ":
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
	case "esc":
		m.selected = make(map[string]struct{})
		m.filtering = false
		m.filter = ""
	case "r":
		if entry := m.SelectedEntry(); entry != nil {
			return m, func() tea.Msg { return RenameRequestMsg{Entry: *entry} }
		}
	case "n":
		return m, func() tea.Msg { return CreateFileRequestMsg{} }
	case "N": // Shift+N
		return m, func() tea.Msg { return CreateDirRequestMsg{} }
	}

	// ── Git-aware file operations (only inside a repo) ─────────────
	if m.hasGitState() {
		entry := m.SelectedEntry()
		if entry != nil && !entry.IsDir {
			switch msg.String() {
			case "a":
				p := entry.Path
				return m, func() tea.Msg { return GitStageMsg{Path: p} }
			case "R":
				p := entry.Path
				return m, func() tea.Msg { return GitUnstageMsg{Path: p} }
			case "D": // Shift+D — discard working-tree changes
				p := entry.Path
				return m, func() tea.Msg { return GitDiscardMsg{Path: p} }
			case "H":
				p := entry.Path
				return m, func() tea.Msg { return GitFileHistoryMsg{Path: p} }
			case "L":
				p := entry.Path
				return m, func() tea.Msg { return GitBlameMsg{Path: p} }
			}
		}
		if entry != nil {
			switch msg.String() {
			case "i":
				p := entry.Path
				return m, func() tea.Msg { return GitIgnoreMsg{Path: p} }
			}
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

func (m Model) goUp() (Model, tea.Cmd) {
	childName := filepath.Base(m.currentPath)

	if len(m.history) > 0 {
		prev := m.history[len(m.history)-1]
		m.history = m.history[:len(m.history)-1]
		m.currentPath = prev.path
		m.cursor = prev.cursor
		m.offset = prev.offset
		m.filter = ""
		m.filtering = false
		m.loadEntries()
		for i, e := range m.entries {
			if e.Name == childName {
				m.cursor = i
				m.fixScroll()
				break
			}
		}
		return m, func() tea.Msg { return DirChangedMsg{Path: m.currentPath} }
	}

	parent := filepath.Dir(m.currentPath)
	if parent == m.currentPath {
		return m, nil
	}

	newM, cmd := m.NavigateTo(parent)
	for i, e := range newM.entries {
		if e.Name == childName {
			newM.cursor = i
			newM.fixScroll()
			break
		}
	}
	return newM, cmd
}

// ── View ───────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	var lines []string

	// Column header
	lines = append(lines, m.renderHeader())

	if m.err != nil {
		errLine := lipgloss.NewStyle().Foreground(theme.AccentRed).
			Render(" Error: " + m.err.Error())
		lines = append(lines, errLine)
	} else if len(m.entries) == 0 {
		lines = append(lines, theme.Dim.Render(" (empty directory)"))
	} else {
		vh := m.visibleHeight()
		end := min(m.offset+vh, len(m.entries))

		for i := m.offset; i < end; i++ {
			lines = append(lines, m.renderEntry(i))
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

func (m Model) renderHeader() string {
	nameW := m.nameWidth()
	name := theme.ListHeader.Width(nameW).Render("Name")
	size := theme.ListHeader.Width(8).Align(lipgloss.Right).Render("Size")
	date := theme.ListHeader.Width(12).Render("Modified")
	if m.hasGitState() {
		// Layout mirrors the data row: " " icon " " name " " S(1) " " size " " date
		gitHdr := theme.ListHeader.Width(1).Render("S")
		return fmt.Sprintf("   %s %s %s %s", name, gitHdr, size, date)
	}
	// " " icon(1) " " name " " size " " date
	return fmt.Sprintf("   %s %s %s", name, size, date)
}

func (m Model) renderEntry(idx int) string {
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
		sizeStr = "--"
	} else {
		sizeStr = filesystem.FormatSize(entry.Size)
	}

	// Date
	dateStr := filesystem.FormatTime(entry.ModTime)

	// Icon with fixed 1-cell width
	iconCell := lipgloss.NewStyle().Foreground(icon.Color).Width(1).MaxWidth(1).Render(icon.Symbol)

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

	// ── Selected row (solid background, per-segment) ─────────────────
	// We apply the background to every segment individually so that inner
	// ANSI foreground codes are not reset by an outer Render() wrapper, and
	// MaxWidth is never needed (the math guarantees exactly m.width cells).
	if isSelected && m.focused {
		applyBg := func(s lipgloss.Style) lipgloss.Style {
			return s.Background(theme.BgSelected).Bold(true)
		}
		sp := applyBg(lipgloss.NewStyle()).Render(" ")
		iconStr := applyBg(lipgloss.NewStyle().Foreground(icon.Color)).Width(1).MaxWidth(1).Render(icon.Symbol)
		nameStr := applyBg(lipgloss.NewStyle()).Width(nameW).Render(truncate(name, nameW))
		sizeRend := applyBg(lipgloss.NewStyle()).Width(8).Align(lipgloss.Right).MaxWidth(8).Render(sizeStr)
		dateRend := applyBg(lipgloss.NewStyle()).Width(12).MaxWidth(12).Render(dateStr)
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
		iconStr := applyBg(lipgloss.NewStyle().Foreground(icon.Color)).Width(1).MaxWidth(1).Render(icon.Symbol)
		nameStr := applyBg(lipgloss.NewStyle()).Width(nameW).Render(truncate(name, nameW))
		sizeRend := applyBg(lipgloss.NewStyle()).Width(8).Align(lipgloss.Right).MaxWidth(8).Render(sizeStr)
		dateRend := applyBg(lipgloss.NewStyle()).Width(12).MaxWidth(12).Render(dateStr)
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

	nameStr := nameStyle.Width(nameW).MaxWidth(nameW).Render(name)
	sizeRendered := theme.FileSize.Width(8).Align(lipgloss.Right).MaxWidth(8).Render(sizeStr)

	// Visual indicator for marked (multi-selected) rows
	if isMarked {
		iconCell = lipgloss.NewStyle().Foreground(theme.AccentYellow).Width(1).MaxWidth(1).Render("✓")
		nameStr = lipgloss.NewStyle().Foreground(theme.AccentYellow).Width(nameW).MaxWidth(nameW).Render(name)
	}

	dateRendered := theme.FileDate.Width(12).MaxWidth(12).Render(dateStr)

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
		candidate := string(runes[:len(runes)-1]) + "…"
		if lipgloss.Width(candidate) <= maxW {
			return candidate
		}
		runes = runes[:len(runes)-1]
	}
	return "…"
}
