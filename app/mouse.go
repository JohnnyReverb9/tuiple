package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/JohnnyReverb9/tuiple/components/filelist"
	"github.com/JohnnyReverb9/tuiple/components/sidebar"
	"github.com/JohnnyReverb9/tuiple/filesystem"
	"github.com/JohnnyReverb9/tuiple/keys"
)

// Mouse.
//
// The wheel scrolls whatever it is over. A click does what it does in a
// file manager: on a row it moves the cursor, a second click opens; on a
// sidebar entry, a tab or a breadcrumb segment it goes there; on a column
// title it sorts by that column. The middle button is the browser's:
// close the tab it is on, open the folder it is on in a new one.
//
// Only presses are acted on. Motion and release events are dropped,
// which also keeps a drag across the window from flooding the loop.

// doubleClickWindow is how close two clicks on a row must be to open it.
const doubleClickWindow = 450 * time.Millisecond

// click remembers the last press on a list row, to recognise the second
// half of a double click.
type click struct {
	path string
	at   time.Time
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	wheel := msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown
	press := msg.Action == tea.MouseActionPress &&
		(msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonMiddle)
	if !wheel && !press {
		return m, nil
	}

	sidebarW, filelistW, previewW := m.panelWidths()
	listX0 := 0
	if sidebarW > 0 {
		listX0 = sidebarW + 1
	}
	previewX0 := listX0 + filelistW + 1
	top := m.chromeTop()
	row := msg.Y - top
	inContent := row >= 0 && row < m.contentHeight()

	overSidebar := sidebarW > 0 && msg.X < sidebarW
	overPreview := previewW > 0 && msg.X >= previewX0
	overList := !overSidebar && !overPreview && msg.X >= listX0 && msg.X < listX0+filelistW

	// ── Wheel ──────────────────────────────────────────────────────
	if wheel {
		switch {
		case overPreview:
			m.preview, _ = m.preview.Update(msg)
		case overSidebar:
			// The sidebar is short enough never to scroll.
		default:
			// The list ignores input while it is out of focus; the wheel
			// is the exception, it scrolls what it points at.
			prevCursor, prevPath := m.filelist.CursorIdx(), m.filelist.CurrentPath()
			m.filelist.SetFocused(true)
			m.filelist, _ = m.filelist.Update(msg)
			m.filelist.SetFocused(m.active == panelFileList)
			return m.afterListInput(prevCursor, prevPath, nil)
		}
		return m, nil
	}

	middle := msg.Button == tea.MouseButtonMiddle

	// ── Tab strip ──────────────────────────────────────────────────
	if m.tabBarVisible() && msg.Y == 0 {
		idx, ok := m.tabAt(msg.X)
		if !ok {
			return m, nil
		}
		if middle {
			return m.closeTab(idx)
		}
		return m.switchTab(idx)
	}

	// ── Breadcrumb ─────────────────────────────────────────────────
	if msg.Y == top-2 && !middle {
		switch target := m.crumbAt(msg.X); target {
		case "":
			return m, nil
		case crumbBack:
			next, cmd, _ := m.runGlobal(keys.NavBack, "")
			return next, cmd
		case crumbForward:
			next, cmd, _ := m.runGlobal(keys.NavForward, "")
			return next, cmd
		default:
			if target == m.currentPath {
				return m, nil
			}
			// Clicking an ancestor is going up several levels at once;
			// land on the branch that was just left, as going up does.
			return m.goTo(target, childOf(target, m.currentPath))
		}
	}

	if !inContent {
		return m, nil
	}

	switch {
	// ── Sidebar ────────────────────────────────────────────────────
	case overSidebar:
		idx, path, ok := m.sidebar.ItemAt(row)
		if !ok {
			return m, nil
		}
		m.sidebar.SetCursor(idx)
		if middle {
			return m.openTab(path, "")
		}
		return m, func() tea.Msg { return sidebar.NavigateMsg{Path: path} }

	// ── Preview ────────────────────────────────────────────────────
	case overPreview:
		if !middle {
			m.active = panelPreview
			m.updateFocus()
		}
		return m, nil

	// ── File list ──────────────────────────────────────────────────
	case overList:
		m.active = panelFileList
		m.updateFocus()

		if row == 0 {
			if middle {
				return m, nil
			}
			mode := m.filelist.SortAt(msg.X - listX0)
			m.statusMsg = "Sorted by " + sortName(mode)
			return m, m.listCmd(filelist.SortListMsg{Mode: mode})
		}

		idx, ok := m.filelist.EntryAt(row)
		if !ok {
			return m, nil
		}
		prevCursor, prevPath := m.filelist.CursorIdx(), m.filelist.CurrentPath()
		entry := m.filelist.Entries()[idx]

		if middle {
			if entry.IsDir {
				return m.openTab(entry.Path, "")
			}
			return m, nil
		}
		// Alt- or Ctrl-click marks, the terminal's stand-in for Cmd-click.
		if msg.Alt || msg.Ctrl {
			m.filelist = m.filelist.ToggleMarkAt(idx)
			m.lastClick = click{}
			return m.afterListInput(prevCursor, prevPath, nil)
		}

		m.filelist = m.filelist.WithCursor(idx)
		now := time.Now()
		double := m.lastClick.path == entry.Path && now.Sub(m.lastClick.at) <= doubleClickWindow
		m.lastClick = click{path: entry.Path, at: now}
		var cmd tea.Cmd
		if double {
			m.lastClick = click{}
			m.filelist, cmd = m.filelist.OpenSelected()
		}
		return m.afterListInput(prevCursor, prevPath, []tea.Cmd{cmd})
	}
	return m, nil
}

// chromeTop is the number of rows above the panels: the tab strip when
// there is one, the breadcrumb, and the rule under it.
func (m Model) chromeTop() int {
	if m.tabBarVisible() {
		return 3
	}
	return 2
}

func sortName(mode filesystem.SortMode) string {
	switch mode {
	case filesystem.SortBySize:
		return "size"
	case filesystem.SortByDate:
		return "date"
	case filesystem.SortByType:
		return "type"
	}
	return "name"
}
