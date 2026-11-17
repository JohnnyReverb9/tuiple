package git

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Mouse support for the git panel and the branches popup.
//
// The app hands the popups mouse events in box coordinates: (0, 0) is the
// top-left corner of the border. Each handler below mirrors the layout of
// the matching View — the same widths, the same scroll windows — to find
// what is under the pointer. Only left presses and the wheel arrive.

// doubleClick is how close two presses on the same item must be.
const doubleClick = 450 * time.Millisecond

// clicks remembers the last press, to recognise a double click.
type clicks struct {
	key string
	at  time.Time
}

// double records a press on key and reports whether it completes a
// double click on the same key.
func (c *clicks) double(key string) bool {
	now := time.Now()
	hit := c.key == key && now.Sub(c.at) <= doubleClick
	c.key, c.at = key, now
	if hit {
		c.key = "" // a third press starts over
	}
	return hit
}

func isWheel(msg tea.MouseMsg) (bool, int) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return true, -1
	case tea.MouseButtonWheelDown:
		return true, 1
	}
	return false, 0
}

// rowAt maps a row of a list drawn with windowAround under a one-line
// header (row 0) to the item index on it.
func rowAt(row, cursor, total, visible int) (int, bool) {
	if row < 1 || total == 0 {
		return 0, false
	}
	start, end := windowAround(cursor, total, visible)
	idx := start + row - 1
	return idx, idx < end
}

// tabAt finds the tab title under column x of a " A · B · C" tab line
// that starts at column x0.
func tabAt(x, x0 int, titles []string) (int, bool) {
	pos := x0 + 1
	for i, t := range titles {
		w := len(t) + 2
		if x >= pos && x < pos+w {
			return i, true
		}
		pos += w + 1
	}
	return 0, false
}

// ── Git panel ──────────────────────────────────────────────────────────

// bodySize is the size of the area under the tabs, as View lays it out.
func (m Model) bodySize() (w, h int) {
	w = clamp(m.width-4, 60, 140)
	h = clamp(m.height-2, 12, m.height)
	return w, h - 5
}

// busy reports whether a prompt or confirmation owns the keyboard; clicks
// are ignored then, so a stray click cannot answer "y/n" for the user.
func (m Model) busy() bool {
	return m.commit.inputMode != commitInputNone ||
		m.log.inputMode != logInputNone || m.log.resetStep > 0 || m.log.smart != nil
}

func (m Model) handleMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	wheel, dir := isWheel(msg)
	if !wheel && m.busy() {
		return m, nil
	}
	// Row 2 holds the tab titles; the body starts on row 3, column 1.
	if !wheel && msg.Y == 2 {
		if i, ok := tabAt(msg.X, 1, tabTitles); ok {
			m.tab = Tab(i)
		}
		return m, nil
	}
	x, y := msg.X-1, msg.Y-3
	w, h := m.bodySize()
	if x < 0 || y < 0 || x >= w || y >= h || m.loadErr != nil {
		return m, nil
	}
	switch m.tab {
	case TabCommit:
		return m.mouseCommit(x, y, w, h, wheel, dir)
	case TabLog:
		return m.mouseLog(x, y, w, h, wheel, dir)
	case TabStashes:
		return m.mouseStashes(x, y, w, h, wheel, dir)
	}
	return m, nil
}

func (m Model) mouseCommit(x, y, w, h int, wheel bool, dir int) (Model, tea.Cmd) {
	const messageH = 4
	const statusH = 1
	topH := h - messageH - statusH - 1
	listW := w
	if topH < 3 {
		topH = max(1, h-1)
	} else {
		listW = clamp(w*40/100, 24, 60)
		if listW > w-10 {
			listW = w - 10
		}
	}

	switch {
	case y < topH && x < listW: // the file list
		if wheel {
			next := m.commit.cursor + dir
			if next >= 0 && next < len(m.commit.changes) {
				m.commit.cursor = next
				m.refreshDiff()
			}
			return m, nil
		}
		idx, ok := rowAt(y, m.commit.cursor, len(m.commit.changes), max(1, topH-1))
		if !ok {
			return m, nil
		}
		m.commit.focus = commitFocusList
		m.commit.message.Blur()
		m.commit.cursor = idx
		m.refreshDiff()
		// The [ ] box is columns 1–3; a click there, or a double click
		// anywhere on the row, stages or unstages the file.
		if (x >= 1 && x <= 3) || m.clicks.double("commit:"+m.commit.changes[idx].Path) {
			m.toggleStage()
		}
		return m, nil

	case y < topH: // the diff
		if wheel {
			m.commit.diffOff = clampOffset(m.commit.diffOff+3*dir, len(m.commit.diffLines))
		}
		return m, nil

	case y <= topH+messageH && !wheel: // the message box and its title
		m.commit.focus = commitFocusMessage
		return m, m.commit.message.Focus()
	}
	return m, nil
}

func (m Model) mouseLog(x, y, w, h int, wheel bool, dir int) (Model, tea.Cmd) {
	topH := h - 1
	if y >= topH {
		return m, nil
	}
	listW := w * 62 / 100
	switch {
	case m.log.menu != nil:
		listW = max(1, min(w*62/100, w-40))
	case m.log.hideDetail || w < 70:
		listW = w
	}

	if x < listW { // the commit list
		if wheel {
			next := m.log.cursor + dir
			if next >= 0 && next < m.log.visibleCount() {
				m.log.cursor = next
				m.refreshLogDetail()
			}
			return m, nil
		}
		idx, ok := rowAt(y, m.log.cursor, m.log.visibleCount(), max(1, topH-1))
		if !ok {
			return m, nil
		}
		m.log.menu = nil
		m.log.cursor = idx
		m.refreshLogDetail()
		if c, ok := m.logCurrent(); ok && m.clicks.double("log:"+c.Hash) {
			m.log.menu = m.buildLogMenu(c)
		}
		return m, nil
	}

	if menu := m.log.menu; menu != nil && x > listW { // the actions menu
		if wheel {
			for i := menu.cursor + dir; i >= 0 && i < len(menu.items); i += dir {
				if menu.items[i].run != nil {
					menu.cursor = i
					break
				}
			}
			return m, nil
		}
		i, ok := rowAt(y, menu.cursor, len(menu.items), max(1, topH-3))
		if !ok || menu.items[i].run == nil {
			return m, nil
		}
		m.log.menu = nil
		menu.items[i].run(&m)
		var cmd tea.Cmd = func() tea.Msg { return BranchesChangedMsg{} }
		if m.log.inputMode != logInputNone {
			cmd = tea.Batch(cmd, textinput.Blink)
		}
		return m, cmd
	}

	if wheel && x > listW { // the details
		m.log.detailOff = clampOffset(m.log.detailOff+3*dir, len(m.log.detailLines))
	}
	return m, nil
}

func (m Model) mouseStashes(x, y, w, h int, wheel bool, dir int) (Model, tea.Cmd) {
	topH := h - 1
	if y >= topH {
		return m, nil
	}
	listW := clamp(w*45/100, 30, 70)
	if listW > w-10 {
		listW = w - 10
	}
	if x < listW {
		if wheel {
			next := m.stash.cursor + dir
			if next >= 0 && next < len(m.stash.stashes) {
				m.stash.cursor = next
				m.refreshStashDetail()
			}
			return m, nil
		}
		if idx, ok := rowAt(y, m.stash.cursor, len(m.stash.stashes), max(1, topH-1)); ok {
			m.stash.cursor = idx
			m.refreshStashDetail()
		}
		return m, nil
	}
	if wheel {
		m.stash.detailOff = clampOffset(m.stash.detailOff+3*dir, len(m.stash.detailLines))
	}
	return m, nil
}

// ── Branches popup ─────────────────────────────────────────────────────

func (m BranchesPopup) handleMouse(msg tea.MouseMsg) (BranchesPopup, tea.Cmd) {
	wheel, dir := isWheel(msg)
	if !wheel && (m.inputMode != branchesInputNone || m.pending != pendingNone) {
		return m, nil
	}
	w := clamp(m.width-4, 50, 110)
	h := clamp(m.height-2, 10, m.height)

	// Row 2: "Branches 8 · Remotes 1 · Tags 0".
	if !wheel && msg.Y == 2 && m.compare == nil {
		counts := []int{len(m.branches), len(m.remotes), len(m.tags)}
		titles := make([]string, len(branchesViewTitles))
		for i, t := range branchesViewTitles {
			titles[i] = t + " " + itoa(counts[i])
		}
		if i, ok := tabAt(msg.X, 1, titles); ok {
			m.setView(branchesView(i))
		}
		return m, nil
	}

	// The list starts right under the tabs (row 3) and leaves the two
	// bottom lines of the body to the filter and status lines.
	bodyH := h - 4 - lipglossHeight(m.renderFooter(w))
	listH := max(1, bodyH-2)
	row := msg.Y - 3
	if row < 0 || row >= listH || msg.X < 1 || msg.X > w || m.loadErr != nil {
		return m, nil
	}

	if m.compare != nil {
		if wheel {
			m.compareOff = max(0, m.compareOff+3*dir)
		}
		return m, nil
	}
	if wheel {
		m.move(dir)
		return m, nil
	}

	start, end := windowAround(m.cursor(), m.visibleCount(), listH)
	i := start + row
	if i >= end || !m.selectable(i) {
		return m, nil
	}
	m.cursors[m.view] = i
	double := m.clicks.double(itoa(int(m.view)) + ":" + itoa(i))

	switch m.view {
	case viewBranches:
		if f, ok := m.selectedFolder(); ok {
			m.toggleFolder(f.key) // one click folds, like the arrow in a tree
			return m, nil
		}
		if double {
			m.doSwitch()
			return m, changed
		}
	case viewRemotes:
		if double {
			return m.updateRemotesKey("enter")
		}
	}
	return m, nil
}

func itoa(n int) string { return strconv.Itoa(n) }

func lipglossHeight(s string) int { return strings.Count(s, "\n") + 1 }
