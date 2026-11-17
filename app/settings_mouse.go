package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Mouse on the settings screen, in box coordinates ((0, 0) is the border's
// top-left corner): row 1 holds the tab titles, rows from 4 the settings.
// A click on a row selects it; a click on its value — or a double click
// anywhere on it — changes it, the way Enter does.

// settingsValueX is where the value column starts: border, "> " prefix,
// the 30-cell label and a 2-cell gap (see renderRow).
const settingsValueX = 1 + 2 + 30 + 2

func (h SettingsPopup) handleMouse(msg tea.MouseMsg) (SettingsPopup, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		h.moveCursor(-1)
		return h, nil
	case tea.MouseButtonWheelDown:
		h.moveCursor(1)
		return h, nil
	}
	if h.editing || h.confirmReset {
		return h, nil
	}

	// " Settings" and four spaces come before the first tab title.
	if msg.Y == 1 {
		pos := 1 + len(" Settings") + 4
		for i, t := range h.tabTitles() {
			w := len(t) + 2
			if msg.X >= pos && msg.X < pos+w {
				h.setTab(settingsTab(i))
				return h, nil
			}
			pos += w + 1
		}
		return h, nil
	}

	// The same scroll window as View.
	rows := h.rows()
	listH := max(1, h.height-2-3-3)
	start := 0
	if len(rows) > listH {
		start = min(max(0, h.cursor-listH/2), len(rows)-listH)
	}
	i := start + msg.Y - 4
	if msg.Y < 4 || i >= len(rows) || i >= start+listH || !rows[i].selectable() {
		return h, nil
	}
	h.cursor = i
	now := time.Now()
	double := h.lastClick == i && now.Sub(h.lastClickAt) <= doubleClickWindow
	h.lastClick, h.lastClickAt = i, now
	if !double && msg.X < settingsValueX {
		return h, nil
	}
	h.lastClick = -1
	// Same as Enter on the row.
	return h.Update(tea.KeyMsg{Type: tea.KeyEnter})
}
