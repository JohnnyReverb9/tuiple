package git

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/theme"
)

// OpenBlameMsg asks the app to open the blame popup.
type OpenBlameMsg struct {
	Repo string
	Path string // repo-relative path
}

// BlamePopup shows `git blame` output for a single file in a scrollable pane.
type BlamePopup struct {
	active bool
	repo   string
	path   string

	lines  []string
	offset int

	status    string
	statusErr bool

	width  int
	height int
}

func NewBlame() BlamePopup { return BlamePopup{} }

func (m *BlamePopup) SetSize(w, h int) { m.width = w; m.height = h }
func (m BlamePopup) IsActive() bool    { return m.active }
func (m *BlamePopup) Stop()            { m.active = false }

func (m *BlamePopup) Start(repo, path string) {
	m.active = true
	m.repo = repo
	m.path = path
	m.offset = 0
	m.status = ""
	m.statusErr = false

	out, err := BlameFile(repo, path)
	if err != nil {
		m.status = err.Error()
		m.statusErr = true
		m.lines = nil
		return
	}
	m.lines = strings.Split(out, "\n")
}

// ── Input ──────────────────────────────────────────────────────────────

func (m BlamePopup) Update(msg tea.Msg) (BlamePopup, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "esc":
		// Esc only — q is reserved for quitting tuiple, matching the
		// branches-popup convention.
		m.Stop()
	case "ctrl+d", "pgdown":
		m.offset = clampOffset(m.offset+scrollStep, len(m.lines))
	case "ctrl+u", "pgup":
		m.offset = clampOffset(m.offset-scrollStep, len(m.lines))
	case "home":
		m.offset = 0
	case "end":
		m.offset = clampOffset(len(m.lines), len(m.lines))
	}
	return m, nil
}

// ── Rendering ──────────────────────────────────────────────────────────

func (m BlamePopup) View() string {
	if !m.active {
		return ""
	}
	w := clamp(m.width-4, 60, 140)
	h := clamp(m.height-2, 12, m.height)

	title := fmt.Sprintf("  Blame: %s", m.path)
	header := lipgloss.NewStyle().
		Background(theme.AccentOrange).Foreground(theme.BgColor).Bold(true).
		Width(w).Render(title)

	bodyH := h - 4 // header(1) + borders(2) + footer(1)
	total := len(m.lines)
	visible := max(1, bodyH-1) // -1 for list header line
	start := clampStart(m.offset, total, visible)
	end := min(total, start+visible)

	var rangeText string
	switch {
	case total == 0:
		rangeText = ""
	case total <= visible:
		rangeText = fmt.Sprintf("  %d lines", total)
	default:
		rangeText = fmt.Sprintf("  %d–%d / %d", start+1, end, total)
	}
	listHdr := theme.ListHeader.Width(w).Render("  Blame" + rangeText)

	bodyLines := []string{listHdr}
	if total == 0 {
		if m.statusErr {
			bodyLines = append(bodyLines, theme.ErrorMsg.Render("  "+m.status))
		} else {
			bodyLines = append(bodyLines, theme.Dim.Render("  (empty file)"))
		}
	} else {
		for _, raw := range m.lines[start:end] {
			bodyLines = append(bodyLines, renderDiffLine(raw, w))
		}
	}
	body := padLines(bodyLines, w, bodyH)

	footer := lipgloss.NewStyle().
		Foreground(theme.FgDimColor).Background(theme.BgDarkColor).Width(w).
		Render(" Ctrl+D/U scroll · Home/End · Esc close")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(theme.AccentOrange).
		Width(w + 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, header, body, footer))
	return box
}
