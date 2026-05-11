package git

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/theme"
)

// logInputMode is the kind of single-line prompt currently shown (if any)
// in the Log tab.
type logInputMode int

const (
	logInputNone logInputMode = iota
	logInputBranchName
)

// logTab holds all state for the Log tab.
type logTab struct {
	commits     []Commit
	cursor      int
	detail      string
	detailLines []string
	detailSHA   string
	detailOff   int

	status    string
	statusErr bool

	input     textinput.Model
	inputMode logInputMode
}

func newLogTab() logTab {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.CharLimit = 100
	ti.Width = 40
	return logTab{input: ti}
}

// ── Data loading & mutation ────────────────────────────────────────────

func (m *Model) reloadLog() {
	if m.root == "" {
		return
	}
	commits, err := Log(m.root, 200)
	if err != nil {
		m.log.status = "log: " + err.Error()
		m.log.statusErr = true
		return
	}
	m.log.commits = commits
	if m.log.cursor >= len(commits) {
		m.log.cursor = max(0, len(commits)-1)
	}
	m.log.detailSHA = ""
	m.refreshLogDetail()
}

func (m *Model) refreshLogDetail() {
	if len(m.log.commits) == 0 {
		m.log.detail = ""
		m.log.detailLines = nil
		m.log.detailSHA = ""
		m.log.detailOff = 0
		return
	}
	c := m.log.commits[m.log.cursor]
	if c.Hash == m.log.detailSHA {
		return
	}
	m.log.detailSHA = c.Hash
	m.log.detailOff = 0
	out, err := CommitDetail(m.root, c.Hash)
	if err != nil {
		m.log.detail = "show error: " + err.Error()
	} else {
		m.log.detail = out
	}
	m.log.detailLines = strings.Split(m.log.detail, "\n")
}

func (m *Model) logCurrent() (Commit, bool) {
	if len(m.log.commits) == 0 || m.log.cursor >= len(m.log.commits) {
		return Commit{}, false
	}
	return m.log.commits[m.log.cursor], true
}

func (m *Model) logCherryPick() {
	c, ok := m.logCurrent()
	if !ok {
		return
	}
	if _, err := run(m.root, "cherry-pick", c.Hash); err != nil {
		m.log.status = err.Error()
		m.log.statusErr = true
		return
	}
	m.log.status = "cherry-picked " + c.ShortSHA
	m.log.statusErr = false
	m.reloadLog()
	m.tracking, _ = Tracking(m.root)
}

func (m *Model) logRevert() {
	c, ok := m.logCurrent()
	if !ok {
		return
	}
	if _, err := run(m.root, "revert", "--no-edit", c.Hash); err != nil {
		m.log.status = err.Error()
		m.log.statusErr = true
		return
	}
	m.log.status = "reverted " + c.ShortSHA
	m.log.statusErr = false
	m.reloadLog()
	m.tracking, _ = Tracking(m.root)
}

func (m *Model) logCreateBranch(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		m.log.status = "empty branch name"
		m.log.statusErr = true
		return
	}
	c, ok := m.logCurrent()
	if !ok {
		return
	}
	if _, err := run(m.root, "branch", name, c.Hash); err != nil {
		m.log.status = err.Error()
		m.log.statusErr = true
		return
	}
	m.log.status = fmt.Sprintf("branch %q created at %s", name, c.ShortSHA)
	m.log.statusErr = false
}

// ── Input ──────────────────────────────────────────────────────────────

// updateLog handles input for the Log tab. The boolean return tells the
// outer overlay whether to skip overlay-level fallback handling.
func (m Model) updateLog(msg tea.Msg) (Model, tea.Cmd, bool) {
	if m.log.inputMode != logInputNone {
		return m.updateLogInput(msg)
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil, false
	}
	switch keyMsg.String() {
	case "up", "k":
		if m.log.cursor > 0 {
			m.log.cursor--
			m.refreshLogDetail()
		}
		return m, nil, true
	case "down", "j":
		if m.log.cursor < len(m.log.commits)-1 {
			m.log.cursor++
			m.refreshLogDetail()
		}
		return m, nil, true
	case "ctrl+d", "pgdown":
		m.log.detailOff = clampOffset(m.log.detailOff+scrollStep, len(m.log.detailLines))
		return m, nil, true
	case "ctrl+u", "pgup":
		m.log.detailOff = clampOffset(m.log.detailOff-scrollStep, len(m.log.detailLines))
		return m, nil, true
	case "home":
		m.log.detailOff = 0
		return m, nil, true
	case "end":
		m.log.detailOff = clampOffset(len(m.log.detailLines), len(m.log.detailLines))
		return m, nil, true
	case "r":
		m.reloadLog()
		m.log.status = "reloaded"
		m.log.statusErr = false
		return m, nil, true
	case "c":
		m.logCherryPick()
		return m, nil, true
	case "v":
		m.logRevert()
		return m, nil, true
	case "b":
		m.log.inputMode = logInputBranchName
		m.log.input.SetValue("")
		m.log.input.Placeholder = "new branch name"
		return m, m.log.input.Focus(), true
	}
	return m, nil, false
}

func (m Model) updateLogInput(msg tea.Msg) (Model, tea.Cmd, bool) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			m.log.inputMode = logInputNone
			m.log.input.Blur()
			return m, nil, true
		case "enter":
			val := m.log.input.Value()
			mode := m.log.inputMode
			m.log.inputMode = logInputNone
			m.log.input.Blur()
			switch mode {
			case logInputBranchName:
				m.logCreateBranch(val)
			}
			return m, nil, true
		}
	}
	var cmd tea.Cmd
	m.log.input, cmd = m.log.input.Update(msg)
	return m, cmd, true
}

// ── Rendering ──────────────────────────────────────────────────────────

func (m Model) renderLogTab(w, h int) string {
	const statusH = 1
	topH := h - statusH
	if topH < 3 {
		return m.renderLogList(w, max(1, h))
	}

	listW := clamp(w*45/100, 30, 70)
	if listW > w-10 {
		listW = w - 10
	}
	detailW := w - listW - 1
	if detailW < 1 {
		detailW = 1
	}

	listView := m.renderLogList(listW, topH)
	detailView := m.renderLogDetail(detailW, topH)
	sep := renderVSep(topH)

	top := lipgloss.JoinHorizontal(lipgloss.Top, listView, sep, detailView)
	status := m.renderLogStatus(w)

	return lipgloss.JoinVertical(lipgloss.Left, top, status)
}

func (m Model) renderLogList(w, h int) string {
	header := theme.ListHeader.Width(w).Render("  Commits")

	lines := []string{header}
	if len(m.log.commits) == 0 {
		lines = append(lines, theme.Dim.Render("  (no commits)"))
	} else {
		visible := max(1, h-1)
		start, end := windowAround(m.log.cursor, len(m.log.commits), visible)
		for i := start; i < end; i++ {
			lines = append(lines, m.renderLogItem(i, w))
		}
	}
	return padLines(lines, w, h)
}

func (m Model) renderLogItem(i, w int) string {
	c := m.log.commits[i]
	isCursor := i == m.log.cursor

	apply := func(s lipgloss.Style) lipgloss.Style {
		if isCursor {
			return s.Background(theme.BgSelected).Bold(true)
		}
		return s
	}

	// Author moves to the right-hand Details pane where the full
	// `git show` output already prints it; the list shows SHA + subject
	// + relative date so every row is one visual line.
	const (
		shaW       = 8
		dateW      = 14
		spaceLead  = 1
		sepSHASubj = 1
		sepSubjDate = 2
		spaceTrail = 1
	)
	subjectW := w - (spaceLead + shaW + sepSHASubj + sepSubjDate + dateW + spaceTrail)
	includeDate := subjectW >= 5
	if !includeDate {
		subjectW = w - (spaceLead + shaW + sepSHASubj + spaceTrail)
	}
	if subjectW < 1 {
		subjectW = 1
	}

	cells := []string{
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", spaceLead)),
		apply(lipgloss.NewStyle().Foreground(theme.AccentYellow)).
			Width(shaW).Render(truncatePlain(c.ShortSHA, shaW)),
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", sepSHASubj)),
		apply(lipgloss.NewStyle().Foreground(theme.FgColor)).
			Width(subjectW).Render(truncatePlain(c.Subject, subjectW)),
	}
	if includeDate {
		cells = append(cells,
			apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", sepSubjDate)),
			apply(lipgloss.NewStyle().Foreground(theme.FgDimColor)).
				Width(dateW).Render(truncatePlain(c.RelDate, dateW)),
		)
	}
	cells = append(cells, apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", spaceTrail)))

	return strings.Join(cells, "")
}

func (m Model) renderLogDetail(w, h int) string {
	total := len(m.log.detailLines)
	visible := max(1, h-1)
	start := clampStart(m.log.detailOff, total, visible)
	end := min(total, start+visible)

	var headerText string
	switch {
	case total == 0:
		headerText = "  Details"
	case total <= visible:
		headerText = fmt.Sprintf("  Details  %d lines", total)
	default:
		headerText = fmt.Sprintf("  Details  %d–%d / %d", start+1, end, total)
	}
	header := theme.ListHeader.Width(w).Render(headerText)

	lines := []string{header}
	if total == 0 {
		lines = append(lines, theme.Dim.Render("  (no commit selected)"))
	} else {
		for _, raw := range m.log.detailLines[start:end] {
			lines = append(lines, renderDiffLine(raw, w))
		}
	}
	return padLines(lines, w, h)
}

func (m Model) renderLogStatus(w int) string {
	if m.log.inputMode != logInputNone {
		label := " Branch name: "
		return lipgloss.NewStyle().
			Foreground(theme.AccentYellow).
			Bold(true).
			Width(w).
			Render(label + m.log.input.View() + theme.Dim.Render("    Enter to create · Esc to cancel"))
	}
	if m.log.status == "" {
		return strings.Repeat(" ", w)
	}
	style := lipgloss.NewStyle().Foreground(theme.AccentGreen)
	if m.log.statusErr {
		style = lipgloss.NewStyle().Foreground(theme.AccentRed)
	}
	return style.Width(w).Render(" " + m.log.status)
}
