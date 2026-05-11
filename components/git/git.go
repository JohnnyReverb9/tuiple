// Package git implements the Git tool overlay (PhpStorm-style):
// a full-window panel with tabs for Commit, Log, and Stashes.
//
// The current implementation is a skeleton: tab switching and
// rendering of placeholders are in place; per-tab interactive
// logic will be filled in incrementally.
package git

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/theme"
)

// Tab identifies the active tab inside the overlay.
type Tab int

const (
	TabCommit Tab = iota
	TabLog
	TabStashes
)

var tabTitles = []string{"Commit", "Log", "Stashes"}

// Model is the Git overlay model.
type Model struct {
	active   bool
	tab      Tab
	repoPath string

	root     string
	branch   string
	tracking TrackingInfo
	loadErr  error

	commit commitTab
	log    logTab
	stash  stashTab

	width  int
	height int
}

// New creates a new Git overlay model.
func New() Model {
	return Model{
		commit: newCommitTab(),
		log:    newLogTab(),
		stash:  newStashTab(),
	}
}

// SetSize sets the overlay viewport size.
func (m *Model) SetSize(w, h int) { m.width = w; m.height = h }

// IsActive reports whether the overlay is currently visible.
func (m Model) IsActive() bool { return m.active }

// Start opens the overlay rooted at the given working path. The repo
// root, current branch, and upstream tracking are probed synchronously
// (each is a single `git` invocation and completes in a few ms).
func (m *Model) Start(repoPath string) tea.Cmd {
	m.active = true
	m.tab = TabCommit
	m.repoPath = repoPath
	m.root = ""
	m.branch = ""
	m.tracking = TrackingInfo{}
	m.loadErr = nil

	root, err := FindRoot(repoPath)
	if err != nil {
		m.loadErr = err
		return nil
	}
	m.root = root
	if branch, err := CurrentBranch(root); err == nil {
		m.branch = branch
	}
	m.tracking, _ = Tracking(root)
	m.commit.status = ""
	m.commit.statusErr = false
	m.log.status = ""
	m.log.statusErr = false
	m.stash.status = ""
	m.stash.statusErr = false
	m.reloadCommit()
	m.reloadLog()
	m.reloadStashes()
	return nil
}

// Stop closes the overlay.
func (m *Model) Stop() { m.active = false }

// Update handles overlay-level input. Tab-specific handlers get the first
// chance to consume the message; if they decline, fallback handling
// (tab cycling, number jumps, close) runs.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}

	if m.loadErr == nil {
		var (
			cmd     tea.Cmd
			handled bool
		)
		switch m.tab {
		case TabCommit:
			m, cmd, handled = m.updateCommit(msg)
		case TabLog:
			m, cmd, handled = m.updateLog(msg)
		case TabStashes:
			m, cmd, handled = m.updateStashes(msg)
		}
		if handled {
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+g":
			m.Stop()
			return m, nil
		case "tab", "right":
			m.tab = (m.tab + 1) % Tab(len(tabTitles))
			return m, nil
		case "shift+tab", "left":
			m.tab = (m.tab + Tab(len(tabTitles)) - 1) % Tab(len(tabTitles))
			return m, nil
		case "1":
			m.tab = TabCommit
			return m, nil
		case "2":
			m.tab = TabLog
			return m, nil
		case "3":
			m.tab = TabStashes
			return m, nil
		}
	}

	return m, nil
}

// View renders the overlay.
func (m Model) View() string {
	if !m.active {
		return ""
	}

	w := clamp(m.width-4, 60, 140)
	h := clamp(m.height-2, 12, m.height)

	header := m.renderHeader(w)
	tabs := m.renderTabs(w)
	body := m.renderBody(w, h-5) // header(1) + tabs(1) + box borders(2) + footer(1)
	footer := m.renderFooter(w)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.AccentGreen).
		Width(w + 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, header, tabs, body, footer))

	return box
}

// ── Rendering helpers ──────────────────────────────────────────────────

func (m Model) renderHeader(w int) string {
	headerStyle := lipgloss.NewStyle().
		Background(theme.AccentGreen).
		Foreground(theme.BgColor).
		Bold(true).
		Width(w)

	left := "  Git"
	if m.branch != "" {
		left += "  " + m.branch
	}

	right := ""
	if m.tracking.Ahead > 0 || m.tracking.Behind > 0 {
		right = fmt.Sprintf("↑%d ↓%d ", m.tracking.Ahead, m.tracking.Behind)
	}

	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return headerStyle.Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) renderTabs(w int) string {
	activeStyle := lipgloss.NewStyle().
		Foreground(theme.AccentGreen).
		Bold(true).
		Underline(true)
	inactiveStyle := lipgloss.NewStyle().
		Foreground(theme.FgDimColor)

	var parts []string
	for i, t := range tabTitles {
		label := " " + t + " "
		if Tab(i) == m.tab {
			parts = append(parts, activeStyle.Render(label))
		} else {
			parts = append(parts, inactiveStyle.Render(label))
		}
	}

	tabsLine := " " + strings.Join(parts, theme.Dim.Render("·"))
	return lipgloss.NewStyle().Width(w).Render(tabsLine)
}

func (m Model) renderBody(w, h int) string {
	if h < 1 {
		h = 1
	}

	var content string
	switch {
	case errors.Is(m.loadErr, ErrNotARepo):
		content = "\n" +
			theme.ErrorMsg.Render("  Not inside a git repository.") + "\n" +
			theme.Dim.Render("  Open the panel from a directory that is tracked by git.") + "\n"
	case m.loadErr != nil:
		content = "\n" + theme.ErrorMsg.Render("  "+m.loadErr.Error()) + "\n"
	default:
		switch m.tab {
		case TabCommit:
			content = m.renderCommitTab(w, h)
		case TabLog:
			content = m.renderLogTab(w, h)
		case TabStashes:
			content = m.renderStashesTab(w, h)
		}
	}

	// Pad to fixed height so the box doesn't jump as content changes.
	lines := strings.Split(content, "\n")
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderFooter(w int) string {
	hint := " Tab/Shift+Tab switch · 1/2/3 jump · Esc close "
	return lipgloss.NewStyle().
		Foreground(theme.FgDimColor).
		Background(theme.BgDarkColor).
		Width(w).
		Render(hint)
}

// ── Utilities ─────────────────────────────────────────────────────────

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
