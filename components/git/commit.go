package git

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/theme"
)

// commitFocus distinguishes whether keystrokes are interpreted as list
// commands or routed into the multi-line message editor.
type commitFocus int

const (
	commitFocusList commitFocus = iota
	commitFocusMessage
)

// commitInputMode is the kind of single-line prompt currently active in
// the Commit tab (if any).
type commitInputMode int

const (
	commitInputNone commitInputMode = iota
	commitInputStashMessage
	commitInputDiscard // y/n prompt, not a text field
)

// commitTab holds all state for the Commit tab. Embedded into Model.
type commitTab struct {
	changes   []FileChange
	cursor    int
	focus     commitFocus
	message   textarea.Model
	diff      string   // cached diff source (kept for legacy reference)
	diffLines []string // pre-split diff lines for scrolling
	diffPath  string   // path whose diff is cached in `diff`
	diffOff   int      // scroll offset into diffLines
	status    string   // last operation result (success or error)
	statusErr bool

	input     textinput.Model
	inputMode commitInputMode
}

func newCommitTab() commitTab {
	ta := textarea.New()
	ta.Placeholder = "Commit message — press i to type, Esc to leave, Ctrl+S to commit"
	ta.Prompt = "│ "
	ta.CharLimit = 4000
	ta.ShowLineNumbers = false
	ta.SetWidth(40)
	ta.SetHeight(3)

	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.CharLimit = 200
	ti.Width = 40

	return commitTab{message: ta, input: ti}
}

// ── Data loading & mutation ────────────────────────────────────────────

func (m *Model) reloadCommit() {
	if m.root == "" {
		return
	}
	changes, err := Status(m.root)
	if err != nil {
		m.commit.status = "status: " + err.Error()
		m.commit.statusErr = true
		return
	}
	m.commit.changes = changes
	if m.commit.cursor >= len(changes) {
		m.commit.cursor = max(0, len(changes)-1)
	}
	m.commit.diffPath = "" // force refresh
	m.refreshDiff()
}

func (m *Model) refreshDiff() {
	if len(m.commit.changes) == 0 {
		m.commit.diff = ""
		m.commit.diffLines = nil
		m.commit.diffPath = ""
		m.commit.diffOff = 0
		return
	}
	c := m.commit.changes[m.commit.cursor]
	if c.Path == m.commit.diffPath {
		return
	}
	m.commit.diffPath = c.Path
	m.commit.diffOff = 0

	var (
		out string
		err error
	)
	switch {
	case c.Untracked():
		out, err = DiffUntracked(m.root, c.Path)
	case c.Staged() && !c.Unstaged():
		out, err = Diff(m.root, c.Path, true)
	default:
		out, err = Diff(m.root, c.Path, false)
		if c.Staged() {
			if cached, _ := Diff(m.root, c.Path, true); cached != "" {
				out = "── staged ──\n" + cached + "\n── working tree ──\n" + out
			}
		}
	}
	if err != nil {
		m.commit.diff = "diff error: " + err.Error()
	} else {
		m.commit.diff = out
	}
	m.commit.diffLines = strings.Split(m.commit.diff, "\n")
}

func (m *Model) stageCurrentFile() {
	if len(m.commit.changes) == 0 {
		return
	}
	c := m.commit.changes[m.commit.cursor]
	if err := StageFile(m.root, c.Path); err != nil {
		m.commit.status = err.Error()
		m.commit.statusErr = true
		return
	}
	m.commit.status = "staged " + c.Path
	m.commit.statusErr = false
	prev := m.commit.cursor
	m.reloadCommit()
	if prev < len(m.commit.changes) {
		m.commit.cursor = prev
	}
}

func (m *Model) unstageCurrentFile() {
	if len(m.commit.changes) == 0 {
		return
	}
	c := m.commit.changes[m.commit.cursor]
	if !c.Staged() {
		m.commit.status = c.Path + " is not staged"
		m.commit.statusErr = true
		return
	}
	if err := UnstageFile(m.root, c.Path); err != nil {
		m.commit.status = err.Error()
		m.commit.statusErr = true
		return
	}
	m.commit.status = "unstaged " + c.Path
	m.commit.statusErr = false
	prev := m.commit.cursor
	m.reloadCommit()
	if prev < len(m.commit.changes) {
		m.commit.cursor = prev
	}
}

func (m *Model) discardCurrentFile() {
	if len(m.commit.changes) == 0 {
		return
	}
	c := m.commit.changes[m.commit.cursor]
	if c.Untracked() {
		m.commit.status = "untracked file — delete it from the filelist instead"
		m.commit.statusErr = true
		return
	}
	if err := DiscardFile(m.root, c.Path); err != nil {
		m.commit.status = err.Error()
		m.commit.statusErr = true
		return
	}
	m.commit.status = "discarded " + c.Path
	m.commit.statusErr = false
	prev := m.commit.cursor
	m.reloadCommit()
	if prev < len(m.commit.changes) {
		m.commit.cursor = prev
	}
}

func (m *Model) addCurrentToGitIgnore() {
	if len(m.commit.changes) == 0 {
		return
	}
	c := m.commit.changes[m.commit.cursor]
	added, err := AddToGitIgnore(m.root, c.Path)
	if err != nil {
		m.commit.status = err.Error()
		m.commit.statusErr = true
		return
	}
	if added {
		m.commit.status = "added to .gitignore: " + c.Path
	} else {
		m.commit.status = "removed from .gitignore: " + c.Path
	}
	m.commit.statusErr = false
	m.reloadCommit()
}

func (m *Model) toggleStage() {
	if len(m.commit.changes) == 0 {
		return
	}
	c := m.commit.changes[m.commit.cursor]
	var err error
	if c.Staged() && !c.Unstaged() {
		_, err = run(m.root, "restore", "--staged", "--", c.Path)
	} else {
		_, err = run(m.root, "add", "--", c.Path)
	}
	if err != nil {
		m.commit.status = err.Error()
		m.commit.statusErr = true
		return
	}
	prevCursor := m.commit.cursor
	m.reloadCommit()
	if prevCursor < len(m.commit.changes) {
		m.commit.cursor = prevCursor
	}
	m.commit.status = ""
}

func (m *Model) stageAll() {
	if _, err := run(m.root, "add", "-A"); err != nil {
		m.commit.status = err.Error()
		m.commit.statusErr = true
		return
	}
	m.reloadCommit()
	m.commit.status = "staged all"
	m.commit.statusErr = false
}

func (m *Model) unstageAll() {
	if _, err := run(m.root, "reset"); err != nil {
		m.commit.status = err.Error()
		m.commit.statusErr = true
		return
	}
	m.reloadCommit()
	m.commit.status = "unstaged all"
	m.commit.statusErr = false
}

func (m *Model) runStash(msg string) {
	if err := StashCreate(m.root, msg); err != nil {
		m.commit.status = err.Error()
		m.commit.statusErr = true
		return
	}
	if strings.TrimSpace(msg) == "" {
		m.commit.status = "stashed working tree"
	} else {
		m.commit.status = "stashed: " + firstLine(msg)
	}
	m.commit.statusErr = false
	m.reloadCommit()
	m.reloadStashes()
}

func (m *Model) runCommit() {
	msg := strings.TrimSpace(m.commit.message.Value())
	if msg == "" {
		m.commit.status = "empty commit message"
		m.commit.statusErr = true
		return
	}
	hasStaged := false
	for _, c := range m.commit.changes {
		if c.Staged() {
			hasStaged = true
			break
		}
	}
	if !hasStaged {
		m.commit.status = "nothing staged"
		m.commit.statusErr = true
		return
	}
	if _, err := runWithStdin(m.root, msg, "commit", "-F", "-"); err != nil {
		m.commit.status = err.Error()
		m.commit.statusErr = true
		return
	}
	m.commit.status = "committed: " + firstLine(msg)
	m.commit.statusErr = false
	m.commit.message.SetValue("")
	m.commit.focus = commitFocusList
	m.commit.message.Blur()
	m.reloadCommit()
	m.reloadLog()
	m.tracking, _ = Tracking(m.root)
}

// ── Input ──────────────────────────────────────────────────────────────

// updateCommit handles input for the Commit tab. The boolean return tells
// the outer overlay whether the message has been fully handled (true) or
// should fall through to overlay-level handling such as tab cycling.
func (m Model) updateCommit(msg tea.Msg) (Model, tea.Cmd, bool) {
	if m.commit.inputMode == commitInputDiscard {
		return m.updateDiscardConfirm(msg)
	}
	if m.commit.inputMode != commitInputNone {
		return m.updateCommitInput(msg)
	}
	if m.commit.focus == commitFocusMessage {
		return m.updateCommitMessage(msg)
	}
	return m.updateCommitList(msg)
}

func (m Model) updateDiscardConfirm(msg tea.Msg) (Model, tea.Cmd, bool) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil, true
	}
	switch keyMsg.String() {
	case "y", "Y":
		m.commit.inputMode = commitInputNone
		m.discardCurrentFile()
	case "n", "N", "esc":
		m.commit.inputMode = commitInputNone
		m.commit.status = "cancelled"
		m.commit.statusErr = false
	}
	return m, nil, true
}

func (m Model) updateCommitList(msg tea.Msg) (Model, tea.Cmd, bool) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil, false
	}
	switch keyMsg.String() {
	case "up", "k":
		if m.commit.cursor > 0 {
			m.commit.cursor--
			m.refreshDiff()
		}
		return m, nil, true
	case "down", "j":
		if m.commit.cursor < len(m.commit.changes)-1 {
			m.commit.cursor++
			m.refreshDiff()
		}
		return m, nil, true
	case "ctrl+d", "pgdown":
		m.commit.diffOff = clampOffset(m.commit.diffOff+scrollStep, len(m.commit.diffLines))
		return m, nil, true
	case "ctrl+u", "pgup":
		m.commit.diffOff = clampOffset(m.commit.diffOff-scrollStep, len(m.commit.diffLines))
		return m, nil, true
	case "home":
		m.commit.diffOff = 0
		return m, nil, true
	case "end":
		m.commit.diffOff = clampOffset(len(m.commit.diffLines), len(m.commit.diffLines))
		return m, nil, true
	case " ":
		m.toggleStage()
		return m, nil, true
	case "a":
		m.stageCurrentFile()
		return m, nil, true
	case "A":
		m.stageAll()
		return m, nil, true
	case "R":
		m.unstageCurrentFile()
		return m, nil, true
	case "D":
		if len(m.commit.changes) > 0 {
			c := m.commit.changes[m.commit.cursor]
			m.commit.inputMode = commitInputDiscard
			m.commit.status = fmt.Sprintf("Discard changes to %q? (y/n)", c.Path)
			m.commit.statusErr = false
		}
		return m, nil, true
	case "I":
		m.addCurrentToGitIgnore()
		return m, nil, true
	case "H":
		if len(m.commit.changes) > 0 {
			c := m.commit.changes[m.commit.cursor]
			repo, path := m.root, c.Path
			return m, func() tea.Msg { return OpenFileHistoryMsg{Repo: repo, Path: path} }, true
		}
		return m, nil, true
	case "L":
		if len(m.commit.changes) > 0 {
			c := m.commit.changes[m.commit.cursor]
			repo, path := m.root, c.Path
			return m, func() tea.Msg { return OpenBlameMsg{Repo: repo, Path: path} }, true
		}
		return m, nil, true
	case "r":
		m.reloadCommit()
		m.commit.status = "reloaded"
		m.commit.statusErr = false
		return m, nil, true
	case "i":
		m.commit.focus = commitFocusMessage
		return m, m.commit.message.Focus(), true
	case "s":
		m.commit.inputMode = commitInputStashMessage
		m.commit.input.SetValue("")
		m.commit.input.Placeholder = "stash message (optional)"
		return m, m.commit.input.Focus(), true
	case "ctrl+s":
		m.runCommit()
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) updateCommitInput(msg tea.Msg) (Model, tea.Cmd, bool) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			m.commit.inputMode = commitInputNone
			m.commit.input.Blur()
			return m, nil, true
		case "enter":
			val := m.commit.input.Value()
			mode := m.commit.inputMode
			m.commit.inputMode = commitInputNone
			m.commit.input.Blur()
			switch mode {
			case commitInputStashMessage:
				m.runStash(val)
			}
			return m, nil, true
		}
	}
	var cmd tea.Cmd
	m.commit.input, cmd = m.commit.input.Update(msg)
	return m, cmd, true
}

func (m Model) updateCommitMessage(msg tea.Msg) (Model, tea.Cmd, bool) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			m.commit.focus = commitFocusList
			m.commit.message.Blur()
			return m, nil, true
		case "ctrl+s":
			m.runCommit()
			return m, nil, true
		}
	}
	var cmd tea.Cmd
	m.commit.message, cmd = m.commit.message.Update(msg)
	return m, cmd, true
}

// ── Rendering ──────────────────────────────────────────────────────────

func (m Model) renderCommitTab(w, h int) string {
	const messageH = 4
	const statusH = 1
	topH := h - messageH - statusH - 1 // -1 for message title

	if topH < 3 {
		// Fall back to a single column if there's no room.
		topH = max(1, h-1)
		return m.renderCommitList(w, topH)
	}

	listW := clamp(w*40/100, 24, 60)
	if listW > w-10 {
		listW = w - 10
	}
	diffW := w - listW - 1
	if diffW < 1 {
		diffW = 1
	}

	listView := m.renderCommitList(listW, topH)
	diffView := m.renderCommitDiff(diffW, topH)
	sep := renderVSep(topH)

	top := lipgloss.JoinHorizontal(lipgloss.Top, listView, sep, diffView)

	m.commit.message.SetWidth(w - 2)
	m.commit.message.SetHeight(messageH)
	msgView := m.renderCommitMessage(w)
	status := m.renderCommitStatus(w)

	return lipgloss.JoinVertical(lipgloss.Left, top, msgView, status)
}

func (m Model) renderCommitList(w, h int) string {
	header := theme.ListHeader.Width(w).Render("  Changes")

	lines := []string{header}

	if len(m.commit.changes) == 0 {
		lines = append(lines, theme.Dim.Render("  (clean working tree)"))
	} else {
		visible := max(1, h-1)
		start, end := windowAround(m.commit.cursor, len(m.commit.changes), visible)
		for i := start; i < end; i++ {
			lines = append(lines, m.renderCommitListItem(i, w))
		}
	}

	return padLines(lines, w, h)
}

func (m Model) renderCommitListItem(i, w int) string {
	c := m.commit.changes[i]
	isCursor := i == m.commit.cursor && m.commit.focus == commitFocusList

	apply := func(s lipgloss.Style) lipgloss.Style {
		if isCursor {
			return s.Background(theme.BgSelected).Bold(true)
		}
		return s
	}

	mark := "[ ]"
	switch {
	case c.Untracked():
		mark = "[?]"
	case c.Staged() && !c.Unstaged():
		mark = "[x]"
	case c.Staged():
		mark = "[~]"
	}

	idx, wt := c.IndexStatus, c.WorktreeStatus
	if idx == ' ' {
		idx = '·'
	}
	if wt == ' ' {
		wt = '·'
	}
	statusStr := string(idx) + string(wt)

	var statusFg lipgloss.TerminalColor
	switch {
	case c.Untracked():
		statusFg = theme.FgDimColor
	case c.IndexStatus == 'A':
		statusFg = theme.AccentGreen
	case c.IndexStatus == 'D' || c.WorktreeStatus == 'D':
		statusFg = theme.AccentRed
	case c.IndexStatus == 'R':
		statusFg = theme.AccentMagenta
	case c.IndexStatus == 'M' || c.WorktreeStatus == 'M':
		statusFg = theme.AccentBlue
	default:
		statusFg = theme.FgDimColor
	}

	const (
		markW      = 3
		statusW    = 2
		spaceLead  = 1
		sep1       = 1
		sep2       = 1
		spaceTrail = 1
	)
	pathW := w - (spaceLead + markW + sep1 + statusW + sep2 + spaceTrail)
	if pathW < 1 {
		pathW = 1
	}

	cells := []string{
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", spaceLead)),
		apply(lipgloss.NewStyle()).Width(markW).Render(mark),
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", sep1)),
		apply(lipgloss.NewStyle().Foreground(statusFg)).Width(statusW).Render(statusStr),
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", sep2)),
		apply(lipgloss.NewStyle().Foreground(theme.FgColor)).
			Width(pathW).Render(truncatePlain(c.Path, pathW)),
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", spaceTrail)),
	}
	return strings.Join(cells, "")
}

func (m Model) renderCommitDiff(w, h int) string {
	total := len(m.commit.diffLines)
	visible := max(1, h-1)
	start := clampStart(m.commit.diffOff, total, visible)
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
		lines = append(lines, theme.Dim.Render("  (no diff)"))
	} else {
		for _, raw := range m.commit.diffLines[start:end] {
			lines = append(lines, renderDiffLine(raw, w))
		}
	}
	return padLines(lines, w, h)
}

func (m Model) renderCommitMessage(w int) string {
	titleStyle := lipgloss.NewStyle().Foreground(theme.AccentYellow).Bold(true)
	title := titleStyle.Render(" Message")
	if m.commit.focus == commitFocusMessage {
		title = titleStyle.Underline(true).Render(" Message (Esc to leave, Ctrl+S to commit)")
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, m.commit.message.View())
}

func (m Model) renderCommitStatus(w int) string {
	if m.commit.inputMode == commitInputDiscard {
		return lipgloss.NewStyle().
			Foreground(theme.AccentYellow).Bold(true).Width(w).
			Render(" " + m.commit.status)
	}
	if m.commit.inputMode != commitInputNone {
		var label string
		if m.commit.inputMode == commitInputStashMessage {
			label = " Stash message: "
		}
		return lipgloss.NewStyle().
			Foreground(theme.AccentYellow).
			Bold(true).
			Width(w).
			Render(label + m.commit.input.View() + theme.Dim.Render("    Enter to confirm · Esc to cancel"))
	}
	if m.commit.status == "" {
		return strings.Repeat(" ", w)
	}
	style := lipgloss.NewStyle().Foreground(theme.AccentGreen)
	if m.commit.statusErr {
		style = lipgloss.NewStyle().Foreground(theme.AccentRed)
	}
	return style.Width(w).Render(" " + m.commit.status)
}

// ── Helpers ────────────────────────────────────────────────────────────

var (
	diffAddStyle  = lipgloss.NewStyle().Foreground(theme.AccentGreen)
	diffDelStyle  = lipgloss.NewStyle().Foreground(theme.AccentRed)
	diffHunkStyle = lipgloss.NewStyle().Foreground(theme.AccentMagenta)
	diffMetaStyle = lipgloss.NewStyle().Foreground(theme.FgDimColor)
)

func colorizeDiffLine(line string) string {
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		return diffMetaStyle.Render(line)
	case strings.HasPrefix(line, "+"):
		return diffAddStyle.Render(line)
	case strings.HasPrefix(line, "-"):
		return diffDelStyle.Render(line)
	case strings.HasPrefix(line, "@@"):
		return diffHunkStyle.Render(line)
	case strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "index "), strings.HasPrefix(line, "── "):
		return diffMetaStyle.Render(line)
	}
	return line
}

// scrollStep is the per-press half-page scroll size used inside diff/log
// detail panes. Half a typical small terminal pane is roughly 10 lines.
const scrollStep = 10

// clampOffset constrains a desired offset to [0, total-1].
func clampOffset(offset, total int) int {
	if offset < 0 {
		return 0
	}
	if offset > total-1 {
		if total == 0 {
			return 0
		}
		return total - 1
	}
	return offset
}

// clampStart constrains a start index so that a window of `visible` rows
// stays inside [0, total].
func clampStart(start, total, visible int) int {
	if total <= visible {
		return 0
	}
	maxStart := total - visible
	if start < 0 {
		return 0
	}
	if start > maxStart {
		return maxStart
	}
	return start
}

// renderDiffLine prepares a single diff/show line for display in a
// right-pane viewport that is `w` cells wide.
//
// The plain raw line is truncated FIRST, then colorized, then padded —
// truncating after colorization is unsafe because the line contains ANSI
// escape sequences and naïve rune-based truncation can chop them in
// half, leaving the terminal in an unbounded state and bleeding the
// remainder onto subsequent visible rows.
func renderDiffLine(raw string, w int) string {
	if w <= 0 {
		return ""
	}
	// Expand tabs to 4 spaces so lipgloss.Width() correctly measures the
	// visible line length before truncation. Without this, a \t counts as
	// 1 cell but renders as 4-8, causing the line to overflow the pane and
	// bleed into the adjacent panel.
	expanded := strings.ReplaceAll(raw, "\t", "    ")
	truncated := truncatePlain(expanded, w)
	colored := colorizeDiffLine(truncated)
	pad := w - lipgloss.Width(colored)
	if pad < 0 {
		pad = 0
	}
	return colored + strings.Repeat(" ", pad)
}

// truncatePlain shortens a plain string with "..." to fit `w` cells, without
// padding to the right. It is intended for use inside cells whose width
// is enforced by a lipgloss style (so the padding is themed). Returns ""
// for w<=0.
func truncatePlain(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 {
		candidate := string(runes[:len(runes)-1]) + "..."
		if lipgloss.Width(candidate) <= w {
			return candidate
		}
		runes = runes[:len(runes)-1]
	}
	return ""
}

func truncateLine(s string, w int) string {
	width := lipgloss.Width(s)
	if width <= w {
		return s + strings.Repeat(" ", w-width)
	}
	runes := []rune(s)
	for len(runes) > 0 {
		c := string(runes[:len(runes)-1]) + "..."
		if lipgloss.Width(c) <= w {
			return c + strings.Repeat(" ", w-lipgloss.Width(c))
		}
		runes = runes[:len(runes)-1]
	}
	return strings.Repeat(" ", w)
}

func padLines(lines []string, w, h int) string {
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return strings.Join(lines, "\n")
}

func renderVSep(h int) string {
	ch := theme.Separator.Render("│")
	lines := make([]string, h)
	for i := range lines {
		lines[i] = ch
	}
	return strings.Join(lines, "\n")
}

func windowAround(cursor, total, visible int) (int, int) {
	if total <= visible {
		return 0, total
	}
	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > total {
		end = total
		start = end - visible
	}
	return start, end
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
