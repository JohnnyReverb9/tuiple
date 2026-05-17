package git

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/theme"
)

// OpenFileHistoryMsg asks the app to open the file-history popup.
type OpenFileHistoryMsg struct {
	Repo string
	Path string // repo-relative path
}

// FileHistoryPopup shows the commit history for a single file.
type FileHistoryPopup struct {
	active bool
	repo   string
	path   string

	commits     []Commit
	cursor      int
	detailLines []string
	detailSHA   string
	detailOff   int

	status    string
	statusErr bool

	width  int
	height int
}

func NewFileHistory() FileHistoryPopup { return FileHistoryPopup{} }

func (m *FileHistoryPopup) SetSize(w, h int) { m.width = w; m.height = h }
func (m FileHistoryPopup) IsActive() bool    { return m.active }
func (m *FileHistoryPopup) Stop()            { m.active = false }

func (m *FileHistoryPopup) Start(repo, path string) {
	m.active = true
	m.repo = repo
	m.path = path
	m.cursor = 0
	m.detailSHA = ""
	m.detailOff = 0
	m.status = ""
	m.statusErr = false

	commits, err := LogFile(repo, path, 200)
	if err != nil {
		m.status = err.Error()
		m.statusErr = true
		m.commits = nil
		return
	}
	m.commits = commits
	m.refreshDetail()
}

func (m *FileHistoryPopup) refreshDetail() {
	if len(m.commits) == 0 {
		m.detailLines = nil
		m.detailSHA = ""
		m.detailOff = 0
		return
	}
	c := m.commits[m.cursor]
	if c.Hash == m.detailSHA {
		return
	}
	m.detailSHA = c.Hash
	m.detailOff = 0
	out, err := CommitFileDetail(m.repo, c.Hash, m.path)
	if err != nil {
		m.detailLines = []string{"error: " + err.Error()}
	} else {
		m.detailLines = strings.Split(out, "\n")
	}
}

// ── Input ──────────────────────────────────────────────────────────────

func (m FileHistoryPopup) Update(msg tea.Msg) (FileHistoryPopup, tea.Cmd) {
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
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.refreshDetail()
		}
	case "down", "j":
		if m.cursor < len(m.commits)-1 {
			m.cursor++
			m.refreshDetail()
		}
	case "ctrl+d", "pgdown":
		m.detailOff = clampOffset(m.detailOff+scrollStep, len(m.detailLines))
	case "ctrl+u", "pgup":
		m.detailOff = clampOffset(m.detailOff-scrollStep, len(m.detailLines))
	case "home":
		m.detailOff = 0
	case "end":
		m.detailOff = clampOffset(len(m.detailLines), len(m.detailLines))
	}
	return m, nil
}

// ── Rendering ──────────────────────────────────────────────────────────

func (m FileHistoryPopup) View() string {
	if !m.active {
		return ""
	}
	w := clamp(m.width-4, 60, 140)
	h := clamp(m.height-2, 12, m.height)

	title := fmt.Sprintf("  File History: %s", m.path)
	header := lipgloss.NewStyle().
		Background(theme.AccentMagenta).Foreground(theme.BgColor).Bold(true).
		Width(w).Render(title)

	bodyH := h - 4 // header(1) + borders(2) + footer(1)

	listW := clamp(w*45/100, 30, 70)
	if listW > w-10 {
		listW = w - 10
	}
	detailW := w - listW - 1
	if detailW < 1 {
		detailW = 1
	}

	listView := m.renderList(listW, bodyH)
	detailView := m.renderDetail(detailW, bodyH)
	sep := renderVSep(bodyH)
	body := lipgloss.JoinHorizontal(lipgloss.Top, listView, sep, detailView)

	status := m.renderStatus(w)

	footer := lipgloss.NewStyle().
		Foreground(theme.FgDimColor).Background(theme.BgDarkColor).Width(w).
		Render(" j/k navigate · Ctrl+D/U scroll diff · Esc close")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(theme.AccentMagenta).
		Width(w + 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, header, body, status, footer))
	return box
}

func (m FileHistoryPopup) renderList(w, h int) string {
	header := theme.ListHeader.Width(w).Render("  Commits")
	lines := []string{header}

	if len(m.commits) == 0 {
		lines = append(lines, theme.Dim.Render("  (no history for this file)"))
	} else {
		visible := max(1, h-1)
		start, end := windowAround(m.cursor, len(m.commits), visible)
		for i := start; i < end; i++ {
			lines = append(lines, m.renderItem(i, w))
		}
	}
	return padLines(lines, w, h)
}

func (m FileHistoryPopup) renderItem(i, w int) string {
	c := m.commits[i]
	isCursor := i == m.cursor

	apply := func(s lipgloss.Style) lipgloss.Style {
		if isCursor {
			return s.Background(theme.BgSelected).Bold(true)
		}
		return s
	}

	const (
		shaW        = 8
		dateW       = 14
		spaceLead   = 1
		sepSHASubj  = 1
		sepSubjDate = 2
		spaceTrail  = 1
	)
	subjectW := w - (spaceLead + shaW + sepSHASubj + sepSubjDate + dateW + spaceTrail)
	if subjectW < 1 {
		subjectW = 1
	}

	cells := []string{
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", spaceLead)),
		apply(lipgloss.NewStyle().Foreground(theme.AccentYellow)).Width(shaW).Render(truncatePlain(c.ShortSHA, shaW)),
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", sepSHASubj)),
		apply(lipgloss.NewStyle().Foreground(theme.FgColor)).Width(subjectW).Render(truncatePlain(c.Subject, subjectW)),
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", sepSubjDate)),
		apply(lipgloss.NewStyle().Foreground(theme.FgDimColor)).Width(dateW).Render(truncatePlain(c.RelDate, dateW)),
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", spaceTrail)),
	}
	return strings.Join(cells, "")
}

func (m FileHistoryPopup) renderDetail(w, h int) string {
	total := len(m.detailLines)
	visible := max(1, h-1)
	start := clampStart(m.detailOff, total, visible)
	end := min(total, start+visible)

	var headerText string
	switch {
	case total == 0:
		headerText = "  Diff"
	case total <= visible:
		headerText = fmt.Sprintf("  Diff  %d lines", total)
	default:
		headerText = fmt.Sprintf("  Diff  %d–%d / %d", start+1, end, total)
	}
	header := theme.ListHeader.Width(w).Render(headerText)

	lines := []string{header}
	if total == 0 {
		lines = append(lines, theme.Dim.Render("  (select a commit)"))
	} else {
		for _, raw := range m.detailLines[start:end] {
			lines = append(lines, renderDiffLine(raw, w))
		}
	}
	return padLines(lines, w, h)
}

func (m FileHistoryPopup) renderStatus(w int) string {
	if m.status == "" {
		return strings.Repeat(" ", w)
	}
	style := lipgloss.NewStyle().Foreground(theme.AccentGreen)
	if m.statusErr {
		style = lipgloss.NewStyle().Foreground(theme.AccentRed)
	}
	return style.Width(w).Render(" " + m.status)
}
