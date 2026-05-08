// Package filelist implements the central file-listing panel.
package filelist

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/components/preview/mediarender"
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

func (m Model) nameWidth() int {
	// Layout: " " icon " " name   size(8) " " date(12) → fixed=25
	return max(10, m.width-25)
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
	case "up":
		if m.cursor > 0 {
			m.cursor--
			m.fixScroll()
		}
	case "down":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
			m.fixScroll()
		}
	case "enter", "right":
		return m.enterSelected()
	case "l":
		if entry := m.SelectedEntry(); entry != nil {
			if mediarender.Classify(entry.Extension) == mediarender.KindAudio {
				return m.enterSelected()
			}
		}
	case "backspace", "left":
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

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m Model) renderHeader() string {
	nameW := m.nameWidth()
	name := theme.ListHeader.Width(nameW).Render("Name")
	size := theme.ListHeader.Width(8).Align(lipgloss.Right).Render("Size")
	date := theme.ListHeader.Width(12).Render("Modified")
	return fmt.Sprintf("   %s %s %s", name, size, date)
}

func (m Model) renderEntry(idx int) string {
	entry := m.entries[idx]
	isSelected := idx == m.cursor
	_, isMarked := m.selected[entry.Path]
	icon := icons.GetIcon(entry.Name, entry.Extension, entry.IsDir, entry.IsExec, entry.IsSymlink)

	// Prepare the name
	name := entry.Name
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

	// ── Selected row (solid background) ──────────────────────────────
	if isSelected && m.focused {
		displayName := truncate(name, nameW)
		line := fmt.Sprintf(" %s %s %s %s",
			icon.Symbol,
			lipgloss.NewStyle().Width(nameW).Render(displayName),
			lipgloss.NewStyle().Width(8).Align(lipgloss.Right).Render(sizeStr),
			lipgloss.NewStyle().Width(12).Render(dateStr),
		)
		return theme.ListCursor.Width(m.width).MaxWidth(m.width).Render(line)
	}

	// ── Unfocused cursor (subtle highlight) ──────────────────────────
	if isSelected {
		displayName := truncate(name, nameW)
		line := fmt.Sprintf(" %s %s %s %s",
			icon.Symbol,
			lipgloss.NewStyle().Width(nameW).Render(displayName),
			lipgloss.NewStyle().Width(8).Align(lipgloss.Right).Render(sizeStr),
			lipgloss.NewStyle().Width(12).Render(dateStr),
		)
		return lipgloss.NewStyle().
			Background(theme.BgHighlight).
			Foreground(theme.FgColor).
			Width(m.width).MaxWidth(m.width).Render(line)
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

	iconStr := lipgloss.NewStyle().Foreground(icon.Color).Render(icon.Symbol)
	nameStr := nameStyle.Width(nameW).MaxWidth(nameW).Render(name)
	sizeRendered := theme.FileSize.Width(8).Align(lipgloss.Right).Render(sizeStr)
	
	// Add visual indicator for marked (selected) rows if they aren't the primary selected cursor
	if isMarked {
		iconStr = lipgloss.NewStyle().Foreground(theme.AccentYellow).Render("✓")
		nameStr = lipgloss.NewStyle().Foreground(theme.AccentYellow).Width(nameW).MaxWidth(nameW).Render(name)
	}

	dateRendered := theme.FileDate.Width(12).Render(dateStr)

	return fmt.Sprintf(" %s %s %s %s", iconStr, nameStr, sizeRendered, dateRendered)
}

// ── String helpers ─────────────────────────────────────────────────────

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
