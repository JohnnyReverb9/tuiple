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
	case "esc", "q", "ctrl+c":
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
	case "enter", " ":
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

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.AccentBlue).
		Padding(0, 1)

	innerW := h.width - 4 // padding + border
	innerH := h.height - 2

	title := lipgloss.NewStyle().
		Foreground(theme.AccentBlue).
		Bold(true).
		Render(" ⟲  History")
	hint := theme.Dim.Render(
		"   ↑↓ navigate · Enter/Space expand · Esc close   (u/U from main view to undo/redo)")

	var body []string
	if len(h.rows) == 0 {
		body = append(body, "")
		body = append(body, theme.Dim.Render("  (no actions recorded this session)"))
	} else {
		// Render each row, taking expansion of the cursor row into account.
		listH := innerH - 3 // title + hint + spacer
		body = h.renderRows(innerW, listH)
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		hint,
		"",
		strings.Join(body, "\n"),
	)

	return border.Width(innerW).Height(innerH).Render(content)
}

func (h HistoryPopup) renderRows(w, maxH int) []string {
	var lines []string

	// First pass: build pretty lines (collapsed or expanded).
	for i, r := range h.rows {
		cursor := i == h.cursor
		switch r.kind {
		case rowSeparator:
			lines = append(lines, "")
			lines = append(lines, theme.Dim.Render(strings.Repeat("─", min(w, 40))+"  redo stack ↓"))
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
	// Op label with colour.
	opColor := opTypeColor(ev.Op)
	op := lipgloss.NewStyle().Foreground(opColor).Bold(true).Width(11).Render(string(ev.Op))

	// Primary file + count.
	primary, extra := summariseItems(ev.Items)
	name := truncateName(primary, w-44)
	count := ""
	if extra > 0 {
		count = theme.Dim.Render(fmt.Sprintf(" +%d more", extra))
	}

	// Safety indicator.
	safety := h.safetyIndicator(ev)

	// Relative time.
	when := theme.Dim.Render(relTime(ev.When))

	// Compose.
	prefix := "  "
	if cursor {
		prefix = lipgloss.NewStyle().Foreground(theme.AccentYellow).Bold(true).Render("▸ ")
	}

	line := prefix + safety + " " + op + " " + name + count + "   " + when

	if cursor {
		line = lipgloss.NewStyle().Background(theme.BgSelected).Width(w).Render(line)
	}
	return line
}

func (h HistoryPopup) renderExpanded(ev HistoryEvent, w int) []string {
	if len(ev.Items) == 0 {
		return nil
	}
	out := make([]string, 0, len(ev.Items))
	for _, it := range ev.Items {
		src := it.Src
		dst := it.Dst
		arrow := " → "
		var body string
		switch ev.Op {
		case OpCreateFile, OpCreateDir:
			body = "create " + dst
		case OpDelete:
			rem := softDeleteRemaining(dst)
			tag := ""
			if rem > 0 {
				tag = theme.Dim.Render(fmt.Sprintf("  (purges in %ds)", int(rem.Seconds())))
			} else {
				tag = theme.Dim.Render("  (purged)")
			}
			body = "delete " + src + tag
		default:
			body = src + arrow + dst
		}
		out = append(out, "       "+theme.Dim.Render("• "+truncateName(body, w-9)))
	}
	return out
}

func (h HistoryPopup) safetyIndicator(ev HistoryEvent) string {
	ok := canRevert(ev)
	if ok {
		return lipgloss.NewStyle().Foreground(theme.AccentGreen).Render("✓")
	}
	return lipgloss.NewStyle().Foreground(theme.AccentRed).Render("✗")
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
	runes := []rune(s)
	for len(runes) > 0 {
		c := string(runes[:len(runes)-1]) + "…"
		if lipgloss.Width(c) <= w {
			return c
		}
		runes = runes[:len(runes)-1]
	}
	return "…"
}
