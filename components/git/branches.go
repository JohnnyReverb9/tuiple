package git

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/theme"
)

// BranchesChangedMsg is emitted by the popup after an operation that
// might have changed the current branch (e.g. switch, merge), so the
// outer app can refresh its working-directory state if needed.
type BranchesChangedMsg struct{}

type branchesInputMode int

const (
	branchesInputNone branchesInputMode = iota
	branchesInputCreate
	branchesInputRename
	branchesInputFilter
)

// pendingConfirm describes an operation that needs a y/n confirmation.
type pendingConfirm int

const (
	pendingNone pendingConfirm = iota
	pendingMerge
	pendingRebase
	pendingForceDelete
)

// BranchesPopup is a standalone overlay listing all branches with
// checkout / create / delete / rename / merge / rebase actions.
type BranchesPopup struct {
	active bool

	repo     string
	branches []Branch
	filter   string
	filtered []int // indexes into `branches` when filter is active; nil otherwise

	cursor int

	loadErr error

	input     textinput.Model
	inputMode branchesInputMode

	pending      pendingConfirm
	pendingName  string // branch the pending op targets

	status    string
	statusErr bool

	width  int
	height int
}

// NewBranches creates a fresh BranchesPopup.
func NewBranches() BranchesPopup {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.CharLimit = 200
	ti.Width = 40
	return BranchesPopup{input: ti}
}

// SetSize sets the rendering viewport for the popup.
func (m *BranchesPopup) SetSize(w, h int) { m.width = w; m.height = h }

// IsActive reports whether the popup is currently visible.
func (m BranchesPopup) IsActive() bool { return m.active }

// Start opens the popup at the given working directory. The repository
// root is detected from this path.
func (m *BranchesPopup) Start(path string) tea.Cmd {
	m.active = true
	m.filter = ""
	m.filtered = nil
	m.cursor = 0
	m.status = ""
	m.statusErr = false
	m.pending = pendingNone
	m.pendingName = ""
	m.inputMode = branchesInputNone
	m.input.Blur()

	root, err := FindRoot(path)
	if err != nil {
		m.loadErr = err
		m.repo = ""
		m.branches = nil
		return nil
	}
	m.loadErr = nil
	m.repo = root
	m.reload()
	return nil
}

// Stop hides the popup.
func (m *BranchesPopup) Stop() {
	m.active = false
	m.input.Blur()
}

func (m *BranchesPopup) reload() {
	if m.repo == "" {
		return
	}
	branches, err := ListBranches(m.repo)
	if err != nil {
		m.loadErr = err
		return
	}
	m.loadErr = nil
	m.branches = branches
	m.applyFilter()
	if m.cursor >= m.visibleCount() {
		m.cursor = max(0, m.visibleCount()-1)
	}
}

func (m *BranchesPopup) applyFilter() {
	if m.filter == "" {
		m.filtered = nil
		return
	}
	needle := strings.ToLower(m.filter)
	filtered := make([]int, 0, len(m.branches))
	for i, b := range m.branches {
		if strings.Contains(strings.ToLower(b.Name), needle) {
			filtered = append(filtered, i)
		}
	}
	m.filtered = filtered
	if m.cursor >= len(filtered) {
		m.cursor = max(0, len(filtered)-1)
	}
}

func (m BranchesPopup) visibleCount() int {
	if m.filtered != nil {
		return len(m.filtered)
	}
	return len(m.branches)
}

func (m BranchesPopup) at(i int) (Branch, bool) {
	if m.filtered != nil {
		if i < 0 || i >= len(m.filtered) {
			return Branch{}, false
		}
		return m.branches[m.filtered[i]], true
	}
	if i < 0 || i >= len(m.branches) {
		return Branch{}, false
	}
	return m.branches[i], true
}

func (m BranchesPopup) selected() (Branch, bool) { return m.at(m.cursor) }

// ── Operations ────────────────────────────────────────────────────────

func (m *BranchesPopup) doSwitch() {
	b, ok := m.selected()
	if !ok {
		return
	}
	target := b.LocalName()
	if err := Switch(m.repo, target); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.setStatus("switched to "+target, false)
	m.reload()
}

func (m *BranchesPopup) doCreate(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		m.setStatus("empty branch name", true)
		return
	}
	if err := CreateAndSwitch(m.repo, name); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.setStatus("created and switched to "+name, false)
	m.reload()
}

func (m *BranchesPopup) doRename(newName string) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		m.setStatus("empty new name", true)
		return
	}
	b, ok := m.selected()
	if !ok {
		return
	}
	if b.IsRemote {
		m.setStatus("cannot rename a remote branch", true)
		return
	}
	from := ""
	if !b.IsCurrent {
		from = b.Name
	}
	if err := RenameBranch(m.repo, from, newName); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.setStatus("renamed to "+newName, false)
	m.reload()
}

func (m *BranchesPopup) doDelete(force bool) {
	b, ok := m.selected()
	if !ok {
		return
	}
	if b.IsRemote {
		m.setStatus("use a remote tool to delete remote branches", true)
		return
	}
	if b.IsCurrent {
		m.setStatus("cannot delete the current branch", true)
		return
	}
	if err := DeleteBranch(m.repo, b.Name, force); err != nil {
		if !force && strings.Contains(err.Error(), "not fully merged") {
			m.pending = pendingForceDelete
			m.pendingName = b.Name
			m.setStatus(fmt.Sprintf("%q is not fully merged. Press D to force-delete, n to cancel.", b.Name), true)
			return
		}
		m.setStatus(err.Error(), true)
		return
	}
	m.setStatus("deleted "+b.Name, false)
	m.reload()
}

func (m *BranchesPopup) doMerge() {
	b, ok := m.selected()
	if !ok {
		return
	}
	if err := MergeBranch(m.repo, b.Name); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.setStatus("merged "+b.Name+" into current", false)
	m.reload()
}

func (m *BranchesPopup) doRebase() {
	b, ok := m.selected()
	if !ok {
		return
	}
	if err := RebaseOnto(m.repo, b.Name); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.setStatus("rebased current onto "+b.Name, false)
	m.reload()
}

func (m *BranchesPopup) doPush() {
	out, err := Push(m.repo)
	if err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.setStatus(parsePushOutput(out), false)
	m.reload()
}

func (m *BranchesPopup) doPull() {
	out, err := Pull(m.repo)
	if err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.setStatus(parsePullOutput(out), false)
	m.reload()
}

func (m *BranchesPopup) doFetch() {
	out, err := Fetch(m.repo)
	if err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.setStatus(parseFetchOutput(out), false)
	m.reload()
}

// ── Output parsers ────────────────────────────────────────────────────

// parsePushOutput distils `git push` combined output into one short line.
func parsePushOutput(out string) string {
	lower := strings.ToLower(out)
	if strings.Contains(lower, "everything up-to-date") ||
		strings.Contains(lower, "up to date") {
		return "nothing to push – already up-to-date"
	}
	// "  branch1 -> origin/branch1" or similar ref-update lines
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "->") {
			return "pushed: " + line
		}
	}
	if out != "" {
		return firstLine(strings.TrimSpace(out))
	}
	return "pushed"
}

// parsePullOutput distils `git pull` combined output into one short line.
func parsePullOutput(out string) string {
	lower := strings.ToLower(out)
	if strings.Contains(lower, "already up to date") {
		return "already up to date"
	}
	// "Updating abc1234..def5678" → show that line directly
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Updating ") {
			return strings.TrimSpace(line)
		}
	}
	// "N files changed, M insertions(+), K deletions(-)"
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "changed") || strings.Contains(line, "insertion") {
			return strings.TrimSpace(line)
		}
	}
	if out != "" {
		return firstLine(strings.TrimSpace(out))
	}
	return "pulled"
}

// parseFetchOutput distils `git fetch` combined output into one short line.
func parseFetchOutput(out string) string {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return "nothing new – already up to date"
	}
	// Each fetched ref prints a line like " a1b2c3..d4e5f6  main -> origin/main"
	// Count how many ref-update lines there are.
	var updates int
	for _, line := range strings.Split(trimmed, "\n") {
		if strings.Contains(line, "->") {
			updates++
		}
	}
	if updates == 1 {
		for _, line := range strings.Split(trimmed, "\n") {
			if strings.Contains(line, "->") {
				return "fetched: " + strings.TrimSpace(line)
			}
		}
	}
	if updates > 1 {
		return fmt.Sprintf("fetched %d refs", updates)
	}
	return firstLine(trimmed)
}

func (m *BranchesPopup) setStatus(s string, isErr bool) {
	m.status = s
	m.statusErr = isErr
}

// ── Input ─────────────────────────────────────────────────────────────

// Update handles keyboard input for the popup. Returns the new model and
// any commands the runtime should execute (e.g. textinput blink).
func (m BranchesPopup) Update(msg tea.Msg) (BranchesPopup, tea.Cmd) {
	if !m.active {
		return m, nil
	}

	if m.inputMode != branchesInputNone {
		return m.updateInput(msg)
	}
	if m.pending != pendingNone {
		return m.updatePending(msg)
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "esc", "ctrl+c", "ctrl+b":
		m.Stop()
		return m, nil
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down", "j":
		if m.cursor < m.visibleCount()-1 {
			m.cursor++
		}
		return m, nil
	case "enter", "l":
		m.doSwitch()
		return m, func() tea.Msg { return BranchesChangedMsg{} }
	case "n":
		m.inputMode = branchesInputCreate
		m.input.SetValue("")
		m.input.Placeholder = "new branch name (created from HEAD)"
		return m, m.input.Focus()
	case "r":
		if b, ok := m.selected(); ok {
			m.inputMode = branchesInputRename
			m.input.SetValue(b.Name)
			m.input.Placeholder = "new name"
			return m, m.input.Focus()
		}
		return m, nil
	case "d":
		m.doDelete(false)
		return m, nil
	case "D":
		// Direct force-delete, no confirmation
		m.doDelete(true)
		return m, nil
	case "m":
		if b, ok := m.selected(); ok {
			m.pending = pendingMerge
			m.pendingName = b.Name
			m.setStatus(fmt.Sprintf("Merge %q into current branch? (y/n)", b.Name), false)
		}
		return m, nil
	case "R":
		if b, ok := m.selected(); ok {
			m.pending = pendingRebase
			m.pendingName = b.Name
			m.setStatus(fmt.Sprintf("Rebase current branch onto %q? (y/n)", b.Name), false)
		}
		return m, nil
	case "/":
		m.inputMode = branchesInputFilter
		m.input.SetValue(m.filter)
		m.input.Placeholder = "filter branches"
		return m, m.input.Focus()
	case "ctrl+r":
		m.reload()
		m.setStatus("reloaded", false)
		return m, nil
	case "P":
		m.doPush()
		return m, func() tea.Msg { return BranchesChangedMsg{} }
	case "p":
		m.doPull()
		return m, func() tea.Msg { return BranchesChangedMsg{} }
	case "F":
		m.doFetch()
		return m, func() tea.Msg { return BranchesChangedMsg{} }
	}
	return m, nil
}

func (m BranchesPopup) updateInput(msg tea.Msg) (BranchesPopup, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			mode := m.inputMode
			m.inputMode = branchesInputNone
			m.input.Blur()
			if mode == branchesInputFilter {
				m.filter = ""
				m.applyFilter()
			}
			return m, nil
		case "enter":
			val := m.input.Value()
			mode := m.inputMode
			m.inputMode = branchesInputNone
			m.input.Blur()
			switch mode {
			case branchesInputCreate:
				m.doCreate(val)
			case branchesInputRename:
				m.doRename(val)
			case branchesInputFilter:
				// already applied live; keep the filter
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	prev := m.input.Value()
	m.input, cmd = m.input.Update(msg)
	if m.inputMode == branchesInputFilter && m.input.Value() != prev {
		m.filter = m.input.Value()
		m.applyFilter()
	}
	return m, cmd
}

func (m BranchesPopup) updatePending(msg tea.Msg) (BranchesPopup, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	pending := m.pending
	switch keyMsg.String() {
	case "y", "Y":
		m.pending = pendingNone
		switch pending {
		case pendingMerge:
			m.doMerge()
			return m, func() tea.Msg { return BranchesChangedMsg{} }
		case pendingRebase:
			m.doRebase()
			return m, func() tea.Msg { return BranchesChangedMsg{} }
		case pendingForceDelete:
			m.doDelete(true)
		}
		return m, nil
	case "D":
		if pending == pendingForceDelete {
			m.pending = pendingNone
			m.doDelete(true)
		}
		return m, nil
	case "n", "N", "esc":
		m.pending = pendingNone
		m.pendingName = ""
		m.setStatus("cancelled", false)
		return m, nil
	}
	return m, nil
}

// ── Rendering ─────────────────────────────────────────────────────────

// View renders the popup. The caller is expected to overlay this on top
// of its main UI (similar to other overlays in the app).
func (m BranchesPopup) View() string {
	if !m.active {
		return ""
	}

	w := clamp(m.width-4, 50, 100)
	h := clamp(m.height-2, 10, m.height)

	header := m.renderHeader(w)
	body := m.renderBody(w, h-4) // header(1) + box top/bottom(2) + footer(1)
	footer := m.renderFooter(w)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.AccentBlue).
		Width(w + 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, header, body, footer))
	return box
}

func (m BranchesPopup) renderHeader(w int) string {
	headerStyle := lipgloss.NewStyle().
		Background(theme.AccentBlue).
		Foreground(theme.BgColor).
		Bold(true).
		Width(w)

	title := "  Branches"
	for _, b := range m.branches {
		if b.IsCurrent {
			title += "   " + b.Name
			break
		}
	}
	return headerStyle.Render(title)
}

func (m BranchesPopup) renderBody(w, h int) string {
	if m.loadErr != nil {
		var msg string
		if m.loadErr == ErrNotARepo {
			msg = theme.ErrorMsg.Render("  Not inside a git repository.")
		} else {
			msg = theme.ErrorMsg.Render("  " + m.loadErr.Error())
		}
		return padLines([]string{"", msg}, w, h)
	}

	// Reserve 1 line for status / input (rendered after the list).
	listH := h - 2
	if listH < 1 {
		listH = 1
	}

	lines := []string{m.renderListHeader(w)}
	total := m.visibleCount()
	if total == 0 {
		lines = append(lines, theme.Dim.Render("  (no branches match)"))
	} else {
		visible := max(1, listH-1)
		start, end := windowAround(m.cursor, total, visible)
		for i := start; i < end; i++ {
			lines = append(lines, m.renderItem(i, w))
		}
	}

	for len(lines) < listH {
		lines = append(lines, strings.Repeat(" ", w))
	}
	if len(lines) > listH {
		lines = lines[:listH]
	}

	statusLine := m.renderStatus(w)
	filterLine := m.renderFilter(w)
	return strings.Join(append(lines, filterLine, statusLine), "\n")
}

func (m BranchesPopup) renderListHeader(w int) string {
	return theme.ListHeader.Width(w).Render("  Local & Remote")
}

func (m BranchesPopup) renderItem(i, w int) string {
	b, _ := m.at(i)
	isCursor := i == m.cursor

	apply := func(s lipgloss.Style) lipgloss.Style {
		if isCursor {
			return s.Background(theme.BgSelected).Bold(true)
		}
		return s
	}

	mark := "  "
	if b.IsCurrent {
		mark = " *"
	}

	nameFg := theme.FgColor
	if b.IsCurrent {
		nameFg = theme.AccentGreen
	} else if b.IsRemote {
		nameFg = theme.AccentCyan
	}

	const (
		spaceLead  = 1
		markW      = 2
		sep1       = 1
		spaceTrail = 1
	)
	rightInfo := ""
	if b.Upstream != "" {
		rightInfo = "→ " + b.Upstream
	}
	rightW := lipgloss.Width(rightInfo)
	if rightW > w/3 {
		rightW = w / 3
	}
	sep2 := 0
	if rightInfo != "" {
		sep2 = 2
	}
	nameW := w - (spaceLead + markW + sep1 + sep2 + rightW + spaceTrail)
	if nameW < 1 {
		nameW = 1
	}

	cells := []string{
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", spaceLead)),
		apply(lipgloss.NewStyle().Foreground(theme.AccentGreen).Bold(true)).
			Width(markW).Render(mark),
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", sep1)),
		apply(lipgloss.NewStyle().Foreground(nameFg)).
			Width(nameW).Render(truncatePlain(b.Name, nameW)),
	}
	if rightInfo != "" {
		cells = append(cells,
			apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", sep2)),
			apply(lipgloss.NewStyle().Foreground(theme.FgDimColor)).
				Width(rightW).Render(truncatePlain(rightInfo, rightW)),
		)
	}
	cells = append(cells, apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", spaceTrail)))
	return strings.Join(cells, "")
}

func (m BranchesPopup) renderFilter(w int) string {
	if m.inputMode == branchesInputFilter {
		return lipgloss.NewStyle().
			Foreground(theme.AccentYellow).
			Bold(true).
			Width(w).
			Render(" /" + m.input.View())
	}
	if m.filter != "" {
		return lipgloss.NewStyle().
			Foreground(theme.AccentYellow).
			Width(w).
			Render(" filter: " + m.filter)
	}
	return strings.Repeat(" ", w)
}

func (m BranchesPopup) renderStatus(w int) string {
	if m.inputMode == branchesInputCreate || m.inputMode == branchesInputRename {
		label := " New name: "
		if m.inputMode == branchesInputCreate {
			label = " Create branch: "
		}
		return lipgloss.NewStyle().
			Foreground(theme.AccentYellow).
			Bold(true).
			Width(w).
			Render(label + m.input.View() + theme.Dim.Render("    Enter · Esc"))
	}
	if m.status == "" {
		return strings.Repeat(" ", w)
	}
	style := lipgloss.NewStyle().Foreground(theme.AccentGreen)
	if m.statusErr {
		style = lipgloss.NewStyle().Foreground(theme.AccentRed)
	}
	return style.Width(w).Render(" " + m.status)
}

func (m BranchesPopup) renderFooter(w int) string {
	hint := " Enter·checkout n·new r·rename d/D·del m·merge R·rebase P·push p·pull F·fetch Esc·close"
	return lipgloss.NewStyle().
		Foreground(theme.FgDimColor).
		Background(theme.BgDarkColor).
		Width(w).
		Render(hint)
}
