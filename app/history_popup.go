package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/theme"
)

// HistoryPopup is a read-only viewer over the undo/redo stacks. It lists
// every action that took place this session — what it touched, when it
// happened, and whether undo would still succeed (the source files may
// have been moved or deleted out-of-band since).
//
// The popup itself never mutates state: undo and redo continue to be
// driven by `u` / `U` from the main view. This is deliberate — allowing
// random-access undo (jumping past unrelated steps) would silently break
// invariants other actions depend on.
// HistoryTab is which view the popup is currently showing.
type HistoryTab int

const (
	TabSession HistoryTab = iota // in-memory undo/redo stacks
	TabAllTime                   // persisted audit log
)

type HistoryPopup struct {
	active bool

	tab HistoryTab

	// Session-tab data (snapshots taken when the popup opens).
	undo []HistoryEvent // most-recent at the end (top of stack)
	redo []HistoryEvent // most-recent at the end (top of stack)

	// All-time-tab data (lazy-loaded from audit log on tab switch).
	audit       []AuditEntry
	auditLoaded bool

	// expanded == true reveals the full file list for the cursor row.
	expanded bool

	// Filter state. When `filter` is non-empty only rows matching it are
	// shown in rebuildRows.  `filtering` is true while the user is
	// actively typing into the input field at the bottom of the popup.
	filter    string
	filtering bool
	input     textinput.Model

	// rows is the flat view we navigate. For the session tab it
	// alternates undoStack/separator/redoStack; for the all-time tab
	// it's the audit log in newest-first order.
	rows   []histRow
	cursor int

	width  int
	height int
}

type histRowKind int

const (
	rowUndo histRowKind = iota
	rowRedo
	rowSeparator
	rowAudit // all-time tab: idx is into m.audit
)

type histRow struct {
	kind histRowKind
	idx  int // index into the relevant stack, or -1 for separator
}

func NewHistoryPopup() HistoryPopup {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.CharLimit = 200
	ti.Width = 40
	return HistoryPopup{input: ti}
}

func (h *HistoryPopup) SetSize(w, height int) { h.width = w; h.height = height }
func (h HistoryPopup) IsActive() bool         { return h.active }

// Start refreshes the snapshot from the global stacks and shows the popup.
func (h *HistoryPopup) Start() {
	h.active = true
	h.tab = TabSession
	h.undo, h.redo = HistorySnapshot()
	h.audit = nil
	h.auditLoaded = false
	h.filter = ""
	h.filtering = false
	h.input.SetValue("")
	h.input.Blur()
	h.expanded = false
	h.rebuildRows()
	h.placeCursorAtDefault()
}

func (h *HistoryPopup) Stop() { h.active = false }

func (h *HistoryPopup) setTab(t HistoryTab) {
	if h.tab == t {
		return
	}
	h.tab = t
	if t == TabAllTime && !h.auditLoaded {
		h.audit, _ = ReadAudit()
		h.auditLoaded = true
	}
	h.expanded = false
	h.rebuildRows()
	h.placeCursorAtDefault()
}

func (h *HistoryPopup) placeCursorAtDefault() {
	h.cursor = 0
	if h.tab == TabSession {
		// Land on the top of the undo stack — that's the next action
		// `u` would revert and the one users want to inspect first.
		for i, r := range h.rows {
			if r.kind == rowUndo {
				h.cursor = i
				break
			}
		}
	}
}

func (h *HistoryPopup) rebuildRows() {
	h.rows = h.rows[:0]
	needle := strings.ToLower(h.filter)

	// Strip $HOME from the head of paths before matching: otherwise a
	// query like "user" matches every single row because the absolute
	// path /Users/<name>/... contains it. After stripping we still match
	// basenames, parent directories, full sub-paths inside home, and
	// any absolute path outside home (e.g. /etc/foo).
	home := homePrefix()
	relPath := func(p string) string {
		return strings.ToLower(strings.TrimPrefix(p, home))
	}

	matchesEvent := func(ev HistoryEvent) bool {
		if needle == "" {
			return true
		}
		if strings.Contains(strings.ToLower(string(ev.Op)), needle) {
			return true
		}
		for _, it := range ev.Items {
			if strings.Contains(relPath(it.Src), needle) ||
				strings.Contains(relPath(it.Dst), needle) {
				return true
			}
		}
		return false
	}
	matchesAudit := func(e AuditEntry) bool {
		if needle == "" {
			return true
		}
		if strings.Contains(strings.ToLower(string(e.Op)), needle) {
			return true
		}
		// Source field gets exact-match semantics so typing "user"
		// reliably narrows to user-originated entries instead of being
		// drowned by every path that happens to contain those letters.
		if strings.ToLower(string(e.Source)) == needle {
			return true
		}
		for _, it := range e.Items {
			if strings.Contains(relPath(it.Src), needle) ||
				strings.Contains(relPath(it.Dst), needle) {
				return true
			}
		}
		return false
	}

	switch h.tab {
	case TabSession:
		// Show undo stack top-down: index len-1 (next to undo) first.
		for i := len(h.undo) - 1; i >= 0; i-- {
			if matchesEvent(h.undo[i]) {
				h.rows = append(h.rows, histRow{kind: rowUndo, idx: i})
			}
		}
		// Only emit the separator if at least one redo row will survive
		// the filter — otherwise the divider would dangle alone.
		anyRedo := false
		for _, r := range h.redo {
			if matchesEvent(r) {
				anyRedo = true
				break
			}
		}
		if anyRedo {
			h.rows = append(h.rows, histRow{kind: rowSeparator, idx: -1})
			for i := len(h.redo) - 1; i >= 0; i-- {
				if matchesEvent(h.redo[i]) {
					h.rows = append(h.rows, histRow{kind: rowRedo, idx: i})
				}
			}
		}
	case TabAllTime:
		for i := range h.audit {
			if matchesAudit(h.audit[i]) {
				h.rows = append(h.rows, histRow{kind: rowAudit, idx: i})
			}
		}
	}
}

// Update handles keys while the popup is open.
func (h HistoryPopup) Update(msg tea.Msg) (HistoryPopup, tea.Cmd) {
	if !h.active {
		return h, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return h, nil
	}

	// While the filter prompt is focused, all keys are routed to it
	// except Enter (commit current filter) and Esc (clear filter
	// outright). Live-update on every keystroke so the list narrows
	// while you type, matching the branches/log popup UX.
	if h.filtering {
		switch keyMsg.String() {
		case "esc":
			h.filter = ""
			h.input.SetValue("")
			h.input.Blur()
			h.filtering = false
			h.rebuildRows()
			h.placeCursorAtDefault()
			return h, nil
		case "enter":
			h.filter = h.input.Value()
			h.input.Blur()
			h.filtering = false
			h.rebuildRows()
			h.placeCursorAtDefault()
			return h, nil
		}
		var cmd tea.Cmd
		h.input, cmd = h.input.Update(msg)
		// Live-narrow as the user types.
		if v := h.input.Value(); v != h.filter {
			h.filter = v
			h.rebuildRows()
			h.cursor = 0
		}
		return h, cmd
	}

	switch keyMsg.String() {
	case "esc":
		// Esc clears an active filter first, only closes the popup
		// when there's nothing left to undo state-wise. This matches
		// the git log tab.
		if h.filter != "" {
			h.filter = ""
			h.input.SetValue("")
			h.rebuildRows()
			h.placeCursorAtDefault()
			return h, nil
		}
		h.Stop()
		return h, nil
	case "tab", "shift+tab", "right", "left":
		// Cycle through tabs in both directions equally — same as `?`
		// (help) and the git overlay. With two tabs this is a toggle,
		// so the user never has to think about direction.
		h.setTab(HistoryTab((int(h.tab) + 1) % 2))
		return h, nil
	case "1":
		h.setTab(TabSession)
		return h, nil
	case "2":
		h.setTab(TabAllTime)
		return h, nil
	case "/":
		h.filtering = true
		h.input.SetValue(h.filter)
		h.input.Placeholder = "filter by op / source / file name"
		h.input.CursorEnd()
		return h, h.input.Focus()
	case "up", "k":
		if h.cursor > 0 {
			h.cursor--
			if h.currentRow().kind == rowSeparator && h.cursor > 0 {
				h.cursor--
			}
		}
		return h, nil
	case "down", "j":
		if h.cursor < len(h.rows)-1 {
			h.cursor++
			if h.currentRow().kind == rowSeparator && h.cursor < len(h.rows)-1 {
				h.cursor++
			}
		}
		return h, nil
	case "enter", "l":
		// Same convention as branches popup: Enter or l opens / activates
		// the row under the cursor. Space is left out so users can scroll
		// through the list with the standard vim-style trio without ever
		// accidentally toggling expansion.
		h.expanded = !h.expanded
		return h, nil
	case "g":
		h.cursor = 0
		return h, nil
	case "G":
		h.cursor = len(h.rows) - 1
		return h, nil
	}
	return h, nil
}

func (h HistoryPopup) currentRow() histRow {
	if h.cursor < 0 || h.cursor >= len(h.rows) {
		return histRow{kind: rowSeparator, idx: -1}
	}
	return h.rows[h.cursor]
}

// ── Rendering ──────────────────────────────────────────────────────────

func (h HistoryPopup) View() string {
	if !h.active || h.width <= 0 || h.height <= 0 {
		return ""
	}

	// Match the branches-popup container layout exactly: border only,
	// no padding. lipgloss's interaction between Padding() and Width()
	// trims one cell off the content area in ways that interact poorly
	// with the per-segment background, so we skip padding and instead
	// add a single space at the start of each rendered line ourselves.
	//
	// `w` is the content width (what each row is sized to). Add +2 to
	// the Border style's Width so the rounded border lands cleanly on
	// the popup's outer dimensions.
	w := h.width - 2 // borders eat 1 cell on each side
	contentH := h.height - 2

	var lines []string
	lines = append(lines, padLine(h.renderTitleBar(), w))
	lines = append(lines, padLine(theme.Dim.Render(
		"   up/down navigate  |  Tab switch  |  / filter  |  Enter/l expand  |  Esc close"), w))
	lines = append(lines, padLine("", w))

	// Reserve the last row for the filter status / input line so the
	// list area shrinks by 1 instead of overrunning it.
	const filterLines = 1
	listH := contentH - len(lines) - filterLines
	if listH < 1 {
		listH = 1
	}

	emptyMsg := "  (no actions recorded this session)"
	if h.tab == TabAllTime {
		emptyMsg = "  (audit log empty — no actions recorded across all sessions yet)"
	}
	if h.filter != "" && len(h.rows) == 0 {
		emptyMsg = fmt.Sprintf("  (no matches for %q)", h.filter)
	}
	if len(h.rows) == 0 {
		lines = append(lines, padLine(theme.Dim.Render(emptyMsg), w))
	} else {
		lines = append(lines, h.renderRows(w, listH)...)
	}

	// Pad to the full content height (minus the reserved filter row)
	// so the filter line always sits at the bottom regardless of how
	// many list rows we actually rendered.
	blank := padLine("", w)
	for len(lines) < contentH-filterLines {
		lines = append(lines, blank)
	}
	if len(lines) > contentH-filterLines {
		lines = lines[:contentH-filterLines]
	}

	// Bottom row: the active filter prompt while typing, otherwise a
	// dim hint with the current filter value (or nothing).
	lines = append(lines, padLine(h.renderFilterLine(), w))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.AccentBlue).
		Render(strings.Join(lines, "\n"))
}

// padLine forces a pre-rendered ANSI string to exactly w visible cells —
// padding short lines with spaces and truncating long lines with
// lipgloss.MaxWidth. Centralising this here means every line sent to the
// bordered container is the same width, which is what lipgloss needs to
// keep the right-hand border aligned without word-wrapping.
func padLine(s string, w int) string {
	vw := lipgloss.Width(s)
	if vw == w {
		return s
	}
	if vw > w {
		return lipgloss.NewStyle().MaxWidth(w).Render(s)
	}
	return s + strings.Repeat(" ", w-vw)
}

// renderFilterLine draws the bottom row of the popup. It plays three
// roles depending on state:
//   - while the filter prompt is focused: yellow label + textinput
//     with hint about Enter/Esc
//   - while a filter is set but the prompt isn't focused: dim
//     reminder of what's filtering plus how to clear it
//   - idle: a blank line so the border closes flush
func (h HistoryPopup) renderFilterLine() string {
	if h.filtering {
		label := lipgloss.NewStyle().
			Foreground(theme.AccentYellow).Bold(true).Render(" Filter: ")
		hint := theme.Dim.Render("    Enter to keep · Esc to clear")
		return label + h.input.View() + hint
	}
	if h.filter != "" {
		return theme.Dim.Render(fmt.Sprintf(
			" filter: %q  (press / to edit · Esc to clear)", h.filter))
	}
	return ""
}

// renderTitleBar prints the popup title with a two-tab strip the user
// switches between with Tab / 1 / 2. The non-active tab is dimmed.
func (h HistoryPopup) renderTitleBar() string {
	title := lipgloss.NewStyle().
		Foreground(theme.AccentBlue).Bold(true).Render(" History")

	activeStyle := lipgloss.NewStyle().Foreground(theme.AccentGreen).Bold(true).Underline(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(theme.FgDimColor)
	tab := func(label string, t HistoryTab) string {
		if h.tab == t {
			return activeStyle.Render(label)
		}
		return inactiveStyle.Render(label)
	}
	return title + "    " + tab(" Session ", TabSession) +
		theme.Dim.Render("·") + tab(" All time ", TabAllTime)
}

func (h HistoryPopup) renderRows(w, maxH int) []string {
	var lines []string

	// First pass: build pretty lines (collapsed or expanded).
	for i, r := range h.rows {
		cursor := i == h.cursor
		switch r.kind {
		case rowSeparator:
			lines = append(lines, clampRowWidth("", w, false))
			sep := theme.Dim.Render(strings.Repeat("-", min(w, 40)) + "  redo stack")
			lines = append(lines, clampRowWidth(sep, w, false))
		case rowUndo:
			lines = append(lines, h.renderEvent(h.undo[r.idx], rowUndo, cursor, w))
			if cursor && h.expanded {
				lines = append(lines, h.renderExpanded(h.undo[r.idx], w)...)
			}
		case rowRedo:
			lines = append(lines, h.renderEvent(h.redo[r.idx], rowRedo, cursor, w))
			if cursor && h.expanded {
				lines = append(lines, h.renderExpanded(h.redo[r.idx], w)...)
			}
		case rowAudit:
			lines = append(lines, h.renderAudit(h.audit[r.idx], cursor, w))
			if cursor && h.expanded {
				// Build a synthetic event so renderExpanded can re-use
				// the same per-item formatting it does in session view.
				ev := HistoryEvent{
					Op:    h.audit[r.idx].Op,
					Items: h.audit[r.idx].Items,
					When:  h.audit[r.idx].When,
				}
				lines = append(lines, h.renderExpanded(ev, w)...)
			}
		}
	}

	// Crude scroll: just clip to maxH for now.
	if len(lines) > maxH {
		// Try to keep cursor visible (approximate — each row is 1 line
		// unless expanded; we centre on the cursor).
		start := h.cursor - maxH/2
		if start < 0 {
			start = 0
		}
		end := start + maxH
		if end > len(lines) {
			end = len(lines)
			start = end - maxH
			if start < 0 {
				start = 0
			}
		}
		lines = lines[start:end]
	}
	return lines
}

func (h HistoryPopup) renderEvent(ev HistoryEvent, kind histRowKind, cursor bool, w int) string {
	// ── Fixed-width slot budget ────────────────────────────────────────
	//   prefix(2) + safety(1) + gap(1) + op(11) + gap(1) + name(nameW)
	//   + gap(2) + when(whenW) = w
	const (
		prefixW = 2
		safetyW = 1
		opW     = 11
		whenW   = 10
		gaps    = 1 + 1 + 2 // gap after safety, after op, before when
	)
	fixed := prefixW + safetyW + opW + whenW + gaps
	nameW := w - fixed
	if nameW < 8 {
		nameW = 8
	}

	// applyBg ensures every visible cell of the row gets the selection
	// background; otherwise lipgloss strips the bg at each inner reset
	// and the highlight ends up patchy.
	applyBg := func(s lipgloss.Style) lipgloss.Style {
		if cursor {
			return s.Background(theme.BgSelected)
		}
		return s
	}

	// Prefix (cursor caret or two-space placeholder). We deliberately use
	// ASCII rather than ▸/▶/→ here: those triangles all have ambiguous
	// East Asian Width and some terminals (incl. iTerm2 with default
	// settings) render them as 2 cells, which would make every cursor
	// row overflow the popup by one cell and visibly wrap.
	var prefix string
	if cursor {
		prefix = applyBg(lipgloss.NewStyle().Foreground(theme.AccentYellow).Bold(true)).Render("> ")
	} else {
		prefix = applyBg(lipgloss.NewStyle()).Render("  ")
	}

	// Safety indicator — green plus or red cross. ASCII for the same
	// reason as the prefix above (✓ and ✗ are also ambiguous-width).
	safeOK := canRevert(ev)
	safetyCh := "+"
	safetyCol := theme.AccentGreen
	if !safeOK {
		safetyCh = "!"
		safetyCol = theme.AccentRed
	}
	safety := applyBg(lipgloss.NewStyle().Foreground(safetyCol).Bold(true)).Render(safetyCh)

	gap1 := applyBg(lipgloss.NewStyle()).Render(" ")
	op := applyBg(lipgloss.NewStyle().Foreground(opTypeColor(ev.Op)).Bold(true).
		Width(opW).MaxWidth(opW)).Render(string(ev.Op))
	gap2 := applyBg(lipgloss.NewStyle()).Render(" ")

	// Primary file + "+N more" suffix, jointly fitted into nameW cells.
	primary, extra := summariseItems(ev.Items)
	display := primary
	if extra > 0 {
		display = fmt.Sprintf("%s  +%d more", primary, extra)
	}
	name := applyBg(lipgloss.NewStyle()).Width(nameW).MaxWidth(nameW).
		Render(truncateName(display, nameW))

	gap3 := applyBg(lipgloss.NewStyle()).Render("  ")
	when := applyBg(lipgloss.NewStyle().Foreground(theme.FgDimColor)).
		Width(whenW).MaxWidth(whenW).Render(relTime(ev.When))

	line := prefix + safety + gap1 + op + gap2 + name + gap3 + when

	// Final safety net: enforce exactly w visible cells. Without this,
	// any character whose terminal-rendered cell count differs from
	// lipgloss's expectation (e.g. ▸ or ✓ rendered as 2 cells by some
	// terminals' ambiguous-width handling) makes the highlighted line
	// overflow the popup's right border and wrap onto the next row.
	return clampRowWidth(line, w, cursor)
}

// renderAudit renders one line of the audit (all-time) tab. Layout
// matches renderEvent so the two tabs feel like the same list, but the
// safety indicator is replaced with a source tag (user/undo/redo) and
// the timestamp slot becomes absolute clock-time rather than "5m ago".
func (h HistoryPopup) renderAudit(e AuditEntry, cursor bool, w int) string {
	const (
		prefixW = 2
		srcW    = 6  // "[user]" / "[undo]" / "[redo]"
		opW     = 11
		whenW   = 16 // absolute date+time for cross-session entries
		gaps    = 1 + 1 + 2
	)
	fixed := prefixW + srcW + opW + whenW + gaps
	nameW := w - fixed
	if nameW < 8 {
		nameW = 8
	}

	applyBg := func(s lipgloss.Style) lipgloss.Style {
		if cursor {
			return s.Background(theme.BgSelected)
		}
		return s
	}

	var prefix string
	if cursor {
		prefix = applyBg(lipgloss.NewStyle().Foreground(theme.AccentYellow).Bold(true)).Render("> ")
	} else {
		prefix = applyBg(lipgloss.NewStyle()).Render("  ")
	}

	// Source tag, colour-coded so undo/redo stand out from user actions.
	var srcCol lipgloss.TerminalColor
	switch e.Source {
	case AuditSourceUndo:
		srcCol = theme.AccentYellow
	case AuditSourceRedo:
		srcCol = theme.AccentCyan
	default:
		srcCol = theme.FgDimColor
	}
	srcLabel := fmt.Sprintf("[%s]", string(e.Source))
	src := applyBg(lipgloss.NewStyle().Foreground(srcCol).Bold(true).
		Width(srcW).MaxWidth(srcW)).Render(srcLabel)

	gap1 := applyBg(lipgloss.NewStyle()).Render(" ")
	op := applyBg(lipgloss.NewStyle().Foreground(opTypeColor(e.Op)).Bold(true).
		Width(opW).MaxWidth(opW)).Render(string(e.Op))
	gap2 := applyBg(lipgloss.NewStyle()).Render(" ")

	primary, extra := summariseItems(e.Items)
	display := primary
	if extra > 0 {
		display = fmt.Sprintf("%s  +%d more", primary, extra)
	}
	name := applyBg(lipgloss.NewStyle()).Width(nameW).MaxWidth(nameW).
		Render(truncateName(display, nameW))

	gap3 := applyBg(lipgloss.NewStyle()).Render("  ")
	whenStr := ""
	if !e.When.IsZero() {
		whenStr = e.When.Local().Format("Jan 02 15:04")
	}
	when := applyBg(lipgloss.NewStyle().Foreground(theme.FgDimColor)).
		Width(whenW).MaxWidth(whenW).Render(whenStr)

	line := prefix + src + gap1 + op + gap2 + name + gap3 + when
	return clampRowWidth(line, w, cursor)
}

// clampRowWidth pads or truncates a pre-rendered ANSI string so its
// visible width is exactly w cells. The cursor flag tells us whether
// the row is highlighted, in which case padding spaces also need the
// selection background so the cursor stripe reaches the right edge.
func clampRowWidth(line string, w int, cursor bool) string {
	vw := lipgloss.Width(line)
	if vw == w {
		return line
	}
	if vw > w {
		return lipgloss.NewStyle().MaxWidth(w).Render(line)
	}
	pad := strings.Repeat(" ", w-vw)
	if cursor {
		pad = lipgloss.NewStyle().Background(theme.BgSelected).Render(pad)
	}
	return line + pad
}

func (h HistoryPopup) renderExpanded(ev HistoryEvent, w int) []string {
	if len(ev.Items) == 0 {
		return nil
	}
	// Indent so the bullet aligns with the file-name column of the row
	// above (prefix(2) + safety(1) + gap(1) + op(11) + gap(1) = 16).
	const indent = "                "
	out := make([]string, 0, len(ev.Items))
	bodyW := w - len(indent) - 2 // 2 for "• "
	if bodyW < 8 {
		bodyW = 8
	}
	for _, it := range ev.Items {
		src := it.Src
		dst := it.Dst
		var body string
		switch ev.Op {
		case OpCreateFile, OpCreateDir:
			body = "create " + dst
		case OpDelete:
			rem := softDeleteRemaining(dst)
			tag := ""
			if rem > 0 {
				tag = fmt.Sprintf("  (purges in %ds)", int(rem.Seconds()))
			} else {
				tag = "  (purged)"
			}
			body = "delete " + src + tag
		default:
			body = src + " -> " + dst
		}
		// ASCII bullet — • (U+2022) is ambiguous-width and breaks our
		// strict cell budget on terminals that render it as 2 cells.
		line := indent + theme.Dim.Render("- "+truncateName(body, bodyW))
		out = append(out, clampRowWidth(line, w, false))
	}
	return out
}

// homePrefix returns "$HOME/" so callers can cheaply strip the home
// directory off absolute paths before filter matching. The trailing
// slash matters: without it "/Users/stepanrulev" would also match
// "/Users/stepanrulev2/..." which we don't want.
func homePrefix() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return ""
	}
	if !strings.HasSuffix(h, "/") {
		h += "/"
	}
	return h
}

// canRevert checks whether revertEvent would currently succeed: the
// sources / destinations the revert needs must still exist on disk.
func canRevert(ev HistoryEvent) bool {
	for _, it := range ev.Items {
		switch ev.Op {
		case OpRename, OpMove:
			if !pathExists(it.Dst) {
				return false
			}
		case OpCreateFile, OpCreateDir, OpCopy:
			if !pathExists(it.Dst) {
				return false
			}
		case OpDelete:
			if !pathExists(it.Dst) {
				return false // trash already purged
			}
		}
	}
	return true
}

func pathExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Lstat(p)
	return err == nil
}

// softDeleteRemaining returns how long until the given trash path is
// purged by the background timer, or 0 when it's already gone.
func softDeleteRemaining(trashPath string) time.Duration {
	pendingDeletesMu.Lock()
	defer pendingDeletesMu.Unlock()
	pd, ok := pendingDeletes[trashPath]
	if !ok {
		return 0
	}
	rem := time.Until(pd.expiresAt)
	if rem < 0 {
		return 0
	}
	return rem
}

func opTypeColor(op OpType) lipgloss.TerminalColor {
	switch op {
	case OpRename:
		return theme.AccentMagenta
	case OpCreateFile, OpCreateDir:
		return theme.AccentGreen
	case OpCopy:
		return theme.AccentBlue
	case OpMove:
		return theme.AccentCyan
	case OpDelete:
		return theme.AccentRed
	}
	return theme.FgColor
}

func summariseItems(items []HistoryItem) (primary string, extra int) {
	if len(items) == 0 {
		return "(no items)", 0
	}
	first := items[0]
	name := first.Dst
	if name == "" {
		name = first.Src
	}
	return filepath.Base(name), len(items) - 1
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < 5*time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return t.Format("Jan 02 15:04")
}

func truncateName(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	// ASCII "..." instead of … so the truncation marker has a predictable
	// 3-cell footprint on every terminal.
	const marker = "..."
	runes := []rune(s)
	for len(runes) > 0 {
		c := string(runes[:len(runes)-1]) + marker
		if lipgloss.Width(c) <= w {
			return c
		}
		runes = runes[:len(runes)-1]
	}
	return marker
}
