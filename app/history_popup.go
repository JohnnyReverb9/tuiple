package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
type HistoryPopup struct {
	active bool

	undo []HistoryEvent // most-recent at the end (top of stack)
	redo []HistoryEvent // most-recent at the end (top of stack)

	// expanded == true reveals the full file list for the cursor row.
	expanded bool

	// rows is the flat view we navigate (undo entries top-down, then a
	// separator placeholder, then redo entries). Each row points back
	// into one of the two stacks plus a kind.
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
)

type histRow struct {
	kind histRowKind
	idx  int // index into the relevant stack, or -1 for separator
}

func NewHistoryPopup() HistoryPopup { return HistoryPopup{} }

func (h *HistoryPopup) SetSize(w, height int) { h.width = w; h.height = height }
func (h HistoryPopup) IsActive() bool         { return h.active }

// Start refreshes the snapshot from the global stacks and shows the popup.
func (h *HistoryPopup) Start() {
	h.active = true
	h.undo, h.redo = HistorySnapshot()
	h.rebuildRows()
	// Land the cursor on the next undo-able entry (top of undo stack) if
	// it exists; that's the row the user is most likely interested in.
	h.cursor = 0
	for i, r := range h.rows {
		if r.kind == rowUndo {
			h.cursor = i
		}
	}
}

func (h *HistoryPopup) Stop() { h.active = false }

func (h *HistoryPopup) rebuildRows() {
	h.rows = h.rows[:0]
	// Show undo stack top-down: index len-1 (next to undo) first.
	for i := len(h.undo) - 1; i >= 0; i-- {
		h.rows = append(h.rows, histRow{kind: rowUndo, idx: i})
	}
	if len(h.redo) > 0 {
		h.rows = append(h.rows, histRow{kind: rowSeparator, idx: -1})
		for i := len(h.redo) - 1; i >= 0; i-- {
			h.rows = append(h.rows, histRow{kind: rowRedo, idx: i})
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
	switch keyMsg.String() {
	case "esc":
		// Esc only — q is reserved for quitting tuiple from anywhere,
		// and space/ctrl-anything aren't part of the popup-close
		// convention used by the branches popup we are mirroring.
		h.Stop()
		return h, nil
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
	lines = append(lines, padLine(lipgloss.NewStyle().
		Foreground(theme.AccentBlue).Bold(true).Render(" History"), w))
	lines = append(lines, padLine(theme.Dim.Render(
		"   up/down navigate  |  Enter/l expand  |  Esc close   (u/U from main view to undo/redo)"), w))
	lines = append(lines, padLine("", w))

	if len(h.rows) == 0 {
		lines = append(lines, padLine(theme.Dim.Render("  (no actions recorded this session)"), w))
	} else {
		listH := contentH - len(lines)
		lines = append(lines, h.renderRows(w, listH)...)
	}

	// Pad to the full content height so the border closes at the right
	// place regardless of how many rows we actually rendered.
	blank := padLine("", w)
	for len(lines) < contentH {
		lines = append(lines, blank)
	}
	if len(lines) > contentH {
		lines = lines[:contentH]
	}

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
