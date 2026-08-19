package search

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JohnnyReverb9/tuiple/filesystem"
	"github.com/JohnnyReverb9/tuiple/theme"
)

// SearchMode defines whether we are searching by name or content.
type SearchMode int

const (
	ModeNameSearch SearchMode = iota
	ModeContentSearch
)

// SearchCompletedMsg is emitted when a search is finished and an item is selected.
type SearchCompletedMsg struct {
	SelectedPath string
	LineNum      int
	Mode         SearchMode
}

const (
	maxDepth = 4
	maxListHeight = 15
)

// Model is the search overlay model.
type Model struct {
	mode       SearchMode
	input      textinput.Model
	results    []filesystem.SearchMatch
	cursor     int
	rootPath   string
	active     bool
	width      int
	height     int
}

// New creates a new Search overlay model.
func New() Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 100
	ti.PlaceholderStyle = theme.Dim
	ti.TextStyle = theme.Normal
	return Model{
		input: ti,
	}
}

func (m *Model) SetSize(w, h int) { m.width = w; m.height = h }
func (m Model) IsActive() bool    { return m.active }

// AcceptsText reports whether the query field is currently taking
// keystrokes, so the app can skip keyboard-layout translation while the
// user types a search term.
func (m Model) AcceptsText() bool { return m.active && m.input.Focused() }

// Start opens the search overlay in the specified mode for the given root path.
func (m *Model) Start(mode SearchMode, rootPath string) tea.Cmd {
	m.active = true
	m.mode = mode
	m.rootPath = rootPath
	m.input.SetValue("")
	m.results = nil
	m.cursor = 0
	
	if mode == ModeNameSearch {
		m.input.Placeholder = " Fuzzy Search (max depth 4) ..."
	} else {
		m.input.Placeholder = " Full-text Content Search ..."
	}
	
	return m.input.Focus()
}

// Stop closes the search overlay.
func (m *Model) Stop() {
	m.active = false
	m.input.Blur()
}

// Update handles search input and list navigation.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}

	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "ctrl+k":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "ctrl+j":
			if m.cursor < len(m.results)-1 {
				m.cursor++
			}
			return m, nil
		case "esc":
			m.Stop()
			return m, nil
		case "enter":
			if len(m.results) > 0 && m.cursor >= 0 && m.cursor < len(m.results) {
				res := m.results[m.cursor]
				mode := m.mode
				m.Stop()
				return m, func() tea.Msg {
					return SearchCompletedMsg{SelectedPath: res.Path, LineNum: res.LineNum, Mode: mode}
				}
			}
			// If no results, just close
			m.Stop()
			return m, nil
		}
	}

	prevVal := m.input.Value()
	m.input, cmd = m.input.Update(msg)

	// If input changed, do search
	if m.input.Value() != prevVal {
		val := m.input.Value()
		if len(val) >= 2 { // start searching only after 2 chars
			if m.mode == ModeNameSearch {
				m.results = filesystem.NameSearch(m.rootPath, val, maxDepth)
			} else {
				m.results = filesystem.ContentSearch(m.rootPath, val, maxDepth)
			}
		} else {
			m.results = nil
		}
		m.cursor = 0
	}

	return m, cmd
}

// View renders the search overlay.
func (m Model) View() string {
	if !m.active {
		return ""
	}

	w := clamp(m.width-4, 40, 100)

	// Header
	headerTitle := " 🔍 MATCH BY NAME "
	if m.mode == ModeContentSearch {
		headerTitle = " 🔍 MATCH IN FILES "
	}

	header := lipgloss.NewStyle().
		Background(theme.AccentMagenta).
		Foreground(theme.BgColor).
		Bold(true).
		Width(w).
		Render(headerTitle)

	// Input
	inputBox := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(theme.BorderColor).
		Width(w).
		Render(m.input.View())

	// Calculate available lines for results list.
	// Overhead: header(1) + input(1) + input border(1) + box border top/bottom(2) = 5
	visibleMax := max(1, m.height-5)

	// List
	var listLines []string
	start := max(0, m.cursor-visibleMax/2)
	end := min(len(m.results), start+visibleMax)

	if start > 0 {
		start = min(start, max(0, len(m.results)-visibleMax))
		end = min(len(m.results), start+visibleMax)
	}

	if len(m.results) == 0 {
		listLines = append(listLines, theme.Dim.Render("  No matches found."))
	} else {
		for i := start; i < end; i++ {
			res := m.results[i]

			var lineStr string
			if m.mode == ModeNameSearch {
				lineStr = filesystem.ShortenPath(res.Path)
			} else {
				var text string
				if len(res.MatchedLine) > 60 {
					text = res.MatchedLine[:60] + "..."
				} else {
					text = res.MatchedLine
				}
				lineStr = fmt.Sprintf("%s:%d | %s", res.Name, res.LineNum, text)
			}

			isCursor := i == m.cursor
			if isCursor {
				lineStr = theme.ListCursor.Width(w).Render(" > " + lineStr)
			} else {
				lineStr = lipgloss.NewStyle().Width(w).Render("   " + lineStr)
			}
			listLines = append(listLines, lineStr)
		}
	}

	// Pad list to fixed height so box size never changes
	for len(listLines) < visibleMax {
		listLines = append(listLines, strings.Repeat(" ", w))
	}

	listView := strings.Join(listLines, "\n")

	// Final box
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.AccentMagenta).
		Width(w + 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, header, inputBox, listView))

	return box
}

func clamp(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}
