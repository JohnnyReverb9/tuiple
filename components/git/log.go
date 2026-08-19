package git

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JohnnyReverb9/tuiple/theme"
)

// logInputMode is the kind of single-line prompt currently shown (if any)
// in the Log tab.
type logInputMode int

const (
	logInputNone logInputMode = iota
	logInputBranchName
	logInputFilter // `/` filter over subject / author / SHA
)

// logTab holds all state for the Log tab.
type logTab struct {
	commits     []Commit
	cursor      int // index into the *visible* list (filtered when filter is active)
	detail      string
	detailLines []string
	detailSHA   string
	detailOff   int

	// Filter state — when filter != "" only commits matching it are
	// shown; filtered holds their indexes into `commits`.
	filter   string
	filtered []int

	status    string
	statusErr bool

	input     textinput.Model
	inputMode logInputMode

	// reset flow: 0=none, 1=pick mode (s/m/h), 2=confirm (y/n)
	resetStep int
	resetMode string // "soft", "mixed", "hard"
}

// visibleCount returns the number of currently visible commits in the
// list (the full commit list when no filter is active, or the count of
// matches when filtering).
func (l *logTab) visibleCount() int {
	if l.filter != "" {
		return len(l.filtered)
	}
	return len(l.commits)
}

// commitAt translates a visible-list index to the underlying commit. The
// returned bool is false when the index is out of range.
func (l *logTab) commitAt(i int) (Commit, bool) {
	if i < 0 {
		return Commit{}, false
	}
	if l.filter != "" {
		if i >= len(l.filtered) {
			return Commit{}, false
		}
		return l.commits[l.filtered[i]], true
	}
	if i >= len(l.commits) {
		return Commit{}, false
	}
	return l.commits[i], true
}

// applyLogFilter rebuilds the `filtered` slice from the current filter
// string.  Matching is case-insensitive against subject, author and the
// short SHA so the user can recall a commit by any of those.
func (l *logTab) applyLogFilter() {
	if l.filter == "" {
		l.filtered = nil
		return
	}
	needle := strings.ToLower(l.filter)
	out := make([]int, 0, len(l.commits))
	for i, c := range l.commits {
		if strings.Contains(strings.ToLower(c.Subject), needle) ||
			strings.Contains(strings.ToLower(c.Author), needle) ||
			strings.Contains(strings.ToLower(c.ShortSHA), needle) ||
			strings.Contains(strings.ToLower(c.Hash), needle) {
			out = append(out, i)
		}
	}
	l.filtered = out
	if l.cursor >= len(out) {
		l.cursor = max(0, len(out)-1)
	}
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
	commits, err := Log(m.root, 500)
	if err != nil {
		m.log.status = "log: " + err.Error()
		m.log.statusErr = true
		return
	}
	m.log.commits = commits
	m.log.applyLogFilter()
	if m.log.cursor >= m.log.visibleCount() {
		m.log.cursor = max(0, m.log.visibleCount()-1)
	}
	m.log.detailSHA = ""
	m.refreshLogDetail()
}

func (m *Model) refreshLogDetail() {
	c, ok := m.log.commitAt(m.log.cursor)
	if !ok {
		m.log.detail = ""
		m.log.detailLines = nil
		m.log.detailSHA = ""
		m.log.detailOff = 0
		return
	}
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
	return m.log.commitAt(m.log.cursor)
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
	if m.log.resetStep > 0 {
		return m.updateLogReset(msg)
	}
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
		if m.log.cursor < m.log.visibleCount()-1 {
			m.log.cursor++
			m.refreshLogDetail()
		}
		return m, nil, true
	case "/":
		// Enter filter mode — type to narrow the commit list by
		// subject, author or SHA.
		m.log.inputMode = logInputFilter
		m.log.input.SetValue(m.log.filter)
		m.log.input.Placeholder = "filter subject / author / sha"
		m.log.input.CursorEnd()
		return m, m.log.input.Focus(), true
	case "esc":
		// Esc outside of input mode clears an active filter.
		if m.log.filter != "" {
			m.log.filter = ""
			m.log.applyLogFilter()
			m.log.cursor = 0
			m.refreshLogDetail()
			m.log.status = ""
			m.log.statusErr = false
			return m, nil, true
		}
		return m, nil, false
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
		// Enter reset flow: pick mode first
		c, ok := m.logCurrent()
		if !ok {
			return m, nil, true
		}
		m.log.resetStep = 1
		m.log.status = fmt.Sprintf("Reset to %s: (s)oft  (m)ixed  (h)ard  Esc=cancel", c.ShortSHA)
		m.log.statusErr = false
		return m, nil, true
	case "ctrl+r":
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

func (m Model) updateLogReset(msg tea.Msg) (Model, tea.Cmd, bool) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil, true
	}

	if m.log.resetStep == 1 { // pick soft / mixed / hard
		switch keyMsg.String() {
		case "s", "m", "h":
			modes := map[string]string{"s": "soft", "m": "mixed", "h": "hard"}
			m.log.resetMode = modes[keyMsg.String()]
			m.log.resetStep = 2
			c, _ := m.logCurrent()
			m.log.status = fmt.Sprintf("%s reset to %s? (y/n)", m.log.resetMode, c.ShortSHA)
			m.log.statusErr = m.log.resetMode == "hard" // red for hard
		case "esc", "q":
			m.log.resetStep = 0
			m.log.resetMode = ""
			m.log.status = "cancelled"
			m.log.statusErr = false
		}
		return m, nil, true
	}

	if m.log.resetStep == 2 { // confirm y/n
		switch keyMsg.String() {
		case "y", "Y":
			m.log.resetStep = 0
			c, ok := m.logCurrent()
			if ok {
				if err := ResetTo(m.root, c.Hash, m.log.resetMode); err != nil {
					m.log.status = err.Error()
					m.log.statusErr = true
				} else {
					m.log.status = fmt.Sprintf("%s reset to %s", m.log.resetMode, c.ShortSHA)
					m.log.statusErr = false
					m.reloadLog()
					m.reloadCommit()
					m.tracking, _ = Tracking(m.root)
				}
			}
			m.log.resetMode = ""
		case "n", "N", "esc":
			m.log.resetStep = 0
			m.log.resetMode = ""
			m.log.status = "cancelled"
			m.log.statusErr = false
		}
		return m, nil, true
	}

	return m, nil, false
}

func (m Model) updateLogInput(msg tea.Msg) (Model, tea.Cmd, bool) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			// In filter mode Esc clears the filter outright; in other
			// modes it just dismisses the prompt without committing.
			if m.log.inputMode == logInputFilter {
				m.log.filter = ""
				m.log.applyLogFilter()
				m.log.cursor = 0
				m.refreshLogDetail()
			}
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
			case logInputFilter:
				// Filter is already live-applied on each keystroke;
				// Enter just dismisses the prompt and keeps it.
				m.log.filter = val
				m.log.applyLogFilter()
				m.refreshLogDetail()
			}
			return m, nil, true
		}
	}
	// Live-update the filter list on every keystroke so the result
	// shrinks while the user is typing — matches filelist's `/` UX.
	var cmd tea.Cmd
	m.log.input, cmd = m.log.input.Update(msg)
	if m.log.inputMode == logInputFilter {
		newFilter := m.log.input.Value()
		if newFilter != m.log.filter {
			m.log.filter = newFilter
			m.log.applyLogFilter()
			m.log.cursor = 0
			m.refreshLogDetail()
		}
	}
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
	// Header shows the match count when a filter is active so the user
	// always sees how many commits the current query catches.
	headerText := "  Commits"
	if m.log.filter != "" {
		headerText = fmt.Sprintf("  Commits  %d / %d match", m.log.visibleCount(), len(m.log.commits))
	}
	header := theme.ListHeader.Width(w).Render(headerText)

	lines := []string{header}
	total := m.log.visibleCount()
	switch {
	case len(m.log.commits) == 0:
		lines = append(lines, theme.Dim.Render("  (no commits)"))
	case total == 0:
		lines = append(lines, theme.Dim.Render("  (no matches)"))
	default:
		visible := max(1, h-1)
		start, end := windowAround(m.log.cursor, total, visible)
		for i := start; i < end; i++ {
			lines = append(lines, m.renderLogItem(i, w))
		}
	}
	return padLines(lines, w, h)
}

func (m Model) renderLogItem(i, w int) string {
	c, ok := m.log.commitAt(i)
	if !ok {
		return strings.Repeat(" ", w)
	}
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
		var label, hint string
		switch m.log.inputMode {
		case logInputFilter:
			label = " Filter: "
			hint = "    Enter to keep · Esc to clear"
		default:
			label = " Branch name: "
			hint = "    Enter to create · Esc to cancel"
		}
		return lipgloss.NewStyle().
			Foreground(theme.AccentYellow).
			Bold(true).
			Width(w).
			Render(label + m.log.input.View() + theme.Dim.Render(hint))
	}
	// When a filter is active but the prompt is dismissed, surface the
	// active query so the user remembers what's filtering the list.
	if m.log.filter != "" && m.log.status == "" {
		return theme.Dim.Width(w).Render(fmt.Sprintf(" filter: %q  (press / to edit · Esc to clear)", m.log.filter))
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
