package git

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JohnnyReverb9/tuiple/theme"
)

// stashTab holds all state for the Stashes tab.
type stashTab struct {
	stashes []Stash
	cursor  int

	detail      string
	detailLines []string
	detailRef   string
	detailOff   int

	status    string
	statusErr bool
}

func newStashTab() stashTab { return stashTab{} }

// ── Data loading & mutation ────────────────────────────────────────────

func (m *Model) reloadStashes() {
	if m.root == "" {
		return
	}
	stashes, err := Stashes(m.root)
	if err != nil {
		m.stash.status = "stash list: " + err.Error()
		m.stash.statusErr = true
		return
	}
	m.stash.stashes = stashes
	if m.stash.cursor >= len(stashes) {
		m.stash.cursor = max(0, len(stashes)-1)
	}
	m.stash.detailRef = ""
	m.refreshStashDetail()
}

func (m *Model) refreshStashDetail() {
	if len(m.stash.stashes) == 0 {
		m.stash.detail = ""
		m.stash.detailLines = nil
		m.stash.detailRef = ""
		m.stash.detailOff = 0
		return
	}
	s := m.stash.stashes[m.stash.cursor]
	if s.Ref == m.stash.detailRef {
		return
	}
	m.stash.detailRef = s.Ref
	m.stash.detailOff = 0
	out, err := StashShow(m.root, s.Ref)
	if err != nil {
		m.stash.detail = "stash show error: " + err.Error()
	} else {
		m.stash.detail = out
	}
	m.stash.detailLines = strings.Split(m.stash.detail, "\n")
}

func (m *Model) stashCurrent() (Stash, bool) {
	if len(m.stash.stashes) == 0 || m.stash.cursor >= len(m.stash.stashes) {
		return Stash{}, false
	}
	return m.stash.stashes[m.stash.cursor], true
}

func (m *Model) stashApplySel() {
	s, ok := m.stashCurrent()
	if !ok {
		return
	}
	if err := StashApply(m.root, s.Ref); err != nil {
		m.stash.status = err.Error()
		m.stash.statusErr = true
		return
	}
	m.stash.status = "applied " + s.Ref
	m.stash.statusErr = false
	m.reloadCommit()
}

func (m *Model) stashPopSel() {
	s, ok := m.stashCurrent()
	if !ok {
		return
	}
	if err := StashPop(m.root, s.Ref); err != nil {
		m.stash.status = err.Error()
		m.stash.statusErr = true
		return
	}
	m.stash.status = "popped " + s.Ref
	m.stash.statusErr = false
	m.reloadStashes()
	m.reloadCommit()
}

func (m *Model) stashDropSel() {
	s, ok := m.stashCurrent()
	if !ok {
		return
	}
	subject := s.Subject
	if err := StashDrop(m.root, s.Ref); err != nil {
		m.stash.status = err.Error()
		m.stash.statusErr = true
		return
	}
	m.stash.status = fmt.Sprintf("dropped %s (%s)", s.Ref, firstLine(subject))
	m.stash.statusErr = false
	m.reloadStashes()
}

// ── Input ──────────────────────────────────────────────────────────────

func (m Model) updateStashes(msg tea.Msg) (Model, tea.Cmd, bool) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil, false
	}
	switch keyMsg.String() {
	case "up", "k":
		if m.stash.cursor > 0 {
			m.stash.cursor--
			m.refreshStashDetail()
		}
		return m, nil, true
	case "down", "j":
		if m.stash.cursor < len(m.stash.stashes)-1 {
			m.stash.cursor++
			m.refreshStashDetail()
		}
		return m, nil, true
	case "ctrl+d", "pgdown":
		m.stash.detailOff = clampOffset(m.stash.detailOff+scrollStep, len(m.stash.detailLines))
		return m, nil, true
	case "ctrl+u", "pgup":
		m.stash.detailOff = clampOffset(m.stash.detailOff-scrollStep, len(m.stash.detailLines))
		return m, nil, true
	case "home":
		m.stash.detailOff = 0
		return m, nil, true
	case "end":
		m.stash.detailOff = clampOffset(len(m.stash.detailLines), len(m.stash.detailLines))
		return m, nil, true
	case "r":
		m.reloadStashes()
		m.stash.status = "reloaded"
		m.stash.statusErr = false
		return m, nil, true
	case "a":
		m.stashApplySel()
		return m, nil, true
	case "p":
		m.stashPopSel()
		return m, nil, true
	case "D":
		m.stashDropSel()
		return m, nil, true
	}
	return m, nil, false
}

// ── Rendering ──────────────────────────────────────────────────────────

func (m Model) renderStashesTab(w, h int) string {
	const statusH = 1
	topH := h - statusH
	if topH < 3 {
		return m.renderStashList(w, max(1, h))
	}

	listW := clamp(w*45/100, 30, 70)
	if listW > w-10 {
		listW = w - 10
	}
	detailW := w - listW - 1
	if detailW < 1 {
		detailW = 1
	}

	listView := m.renderStashList(listW, topH)
	detailView := m.renderStashDetail(detailW, topH)
	sep := renderVSep(topH)

	top := lipgloss.JoinHorizontal(lipgloss.Top, listView, sep, detailView)
	status := m.renderStashStatus(w)
	return lipgloss.JoinVertical(lipgloss.Left, top, status)
}

func (m Model) renderStashList(w, h int) string {
	header := theme.ListHeader.Width(w).Render("  Stashes")
	lines := []string{header}
	if len(m.stash.stashes) == 0 {
		lines = append(lines, theme.Dim.Render("  (no stashes)"))
	} else {
		visible := max(1, h-1)
		start, end := windowAround(m.stash.cursor, len(m.stash.stashes), visible)
		for i := start; i < end; i++ {
			lines = append(lines, m.renderStashItem(i, w))
		}
	}
	return padLines(lines, w, h)
}

func (m Model) renderStashItem(i, w int) string {
	s := m.stash.stashes[i]
	isCursor := i == m.stash.cursor

	apply := func(st lipgloss.Style) lipgloss.Style {
		if isCursor {
			return st.Background(theme.BgSelected).Bold(true)
		}
		return st
	}

	const (
		refW       = 12
		spaceLead  = 1
		sep        = 2
		spaceTrail = 1
	)
	subjectW := w - (spaceLead + refW + sep + spaceTrail)
	if subjectW < 1 {
		subjectW = 1
	}

	cells := []string{
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", spaceLead)),
		apply(lipgloss.NewStyle().Foreground(theme.AccentYellow)).
			Width(refW).Render(truncatePlain(s.Ref, refW)),
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", sep)),
		apply(lipgloss.NewStyle().Foreground(theme.FgColor)).
			Width(subjectW).Render(truncatePlain(s.Subject, subjectW)),
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", spaceTrail)),
	}
	return strings.Join(cells, "")
}

func (m Model) renderStashDetail(w, h int) string {
	total := len(m.stash.detailLines)
	visible := max(1, h-1)
	start := clampStart(m.stash.detailOff, total, visible)
	end := min(total, start+visible)

	var headerText string
	switch {
	case total == 0:
		headerText = "  Stash"
	case total <= visible:
		headerText = fmt.Sprintf("  Stash  %d lines", total)
	default:
		headerText = fmt.Sprintf("  Stash  %d–%d / %d", start+1, end, total)
	}
	header := theme.ListHeader.Width(w).Render(headerText)

	lines := []string{header}
	if total == 0 {
		lines = append(lines, theme.Dim.Render("  (no stash selected)"))
	} else {
		for _, raw := range m.stash.detailLines[start:end] {
			lines = append(lines, renderDiffLine(raw, w))
		}
	}
	return padLines(lines, w, h)
}

func (m Model) renderStashStatus(w int) string {
	if m.stash.status == "" {
		return strings.Repeat(" ", w)
	}
	style := lipgloss.NewStyle().Foreground(theme.AccentGreen)
	if m.stash.statusErr {
		style = lipgloss.NewStyle().Foreground(theme.AccentRed)
	}
	return style.Width(w).Render(" " + m.stash.status)
}
