package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/JohnnyReverb9/tuiple/bookmarks"
	"github.com/JohnnyReverb9/tuiple/components/filelist"
	"github.com/JohnnyReverb9/tuiple/components/picker"
	"github.com/JohnnyReverb9/tuiple/components/search"
	"github.com/JohnnyReverb9/tuiple/components/sidebar"
	"github.com/JohnnyReverb9/tuiple/config"
	"github.com/JohnnyReverb9/tuiple/filesystem"
	"github.com/JohnnyReverb9/tuiple/icons"
	"github.com/JohnnyReverb9/tuiple/keys"
)

// Actions.
//
// An action can be asked for in three ways — a key from the keymap, a
// Cmd shortcut arriving as an escape sequence, a row picked in the
// command palette — and all three end up here. That is the point of the
// file: what "new tab" does is written once, and how it was requested is
// somebody else's business.

// runGlobal carries out an action of the global scope. key is the raw
// keystroke that asked for it, for the one action (jump to tab N) that
// needs to know which of its keys was pressed.
//
// handled is false when the action has nothing to act on right now, in
// which case a keystroke falls through to the focused panel: - and = are
// audio seeks only while something is playing.
func (m Model) runGlobal(action keys.Action, key string) (Model, tea.Cmd, bool) {
	switch action {
	case keys.Quit:
		CleanupPendingDeletes()
		m.writeExitCwd()
		m.saveSession()
		return m, tea.Quit, true

	case keys.SeekBack:
		return m.seek(-5)
	case keys.SeekForward:
		return m.seek(5)
	case keys.SeekBackFar:
		return m.seek(-30)
	case keys.SeekFwdFar:
		return m.seek(30)

	case keys.PanelNext:
		m.cyclePanel(1)
		m.updateFocus()
		return m, nil, true
	case keys.PanelPrev:
		m.cyclePanel(-1)
		m.updateFocus()
		return m, nil, true

	case keys.Help:
		m.showHelp = !m.showHelp
		return m, nil, true

	case keys.GoToPath:
		m.dialogMode = DialogGoToPath
		m.textInput.SetValue("")
		m.textInput.Placeholder = "path — Tab completes"
		m.textInput.Focus()
		return m, nil, true

	case keys.NavBack:
		if !m.filelist.CanGoBack() {
			m.statusMsg = "Nowhere to go back to"
			return m, nil, true
		}
		var cmd tea.Cmd
		m.filelist, cmd = m.filelist.Back()
		m.currentPath = m.filelist.CurrentPath()
		return m, cmd, true
	case keys.NavForward:
		if !m.filelist.CanGoForward() {
			m.statusMsg = "Nowhere to go forward to"
			return m, nil, true
		}
		var cmd tea.Cmd
		m.filelist, cmd = m.filelist.Forward()
		m.currentPath = m.filelist.CurrentPath()
		return m, cmd, true

	case keys.TabNew:
		// A second view of where you already are, cursor and all — the
		// usual reason for a new tab is to keep this place while going
		// to look at another.
		name := ""
		if e := m.filelist.SelectedEntry(); e != nil {
			name = e.Name
		}
		next, cmd := m.openTab(m.currentPath, name)
		return next, cmd, true
	case keys.TabClose:
		next, cmd := m.closeTab(m.tabIdx)
		return next, cmd, true
	case keys.TabReopen:
		next, cmd := m.reopenTab()
		return next, cmd, true
	case keys.TabNext:
		next, cmd := m.cycleTab(1)
		return next, cmd, true
	case keys.TabPrev:
		next, cmd := m.cycleTab(-1)
		return next, cmd, true
	case keys.TabJump:
		if len(key) != 1 || key[0] < '1' || key[0] > '9' {
			return m, nil, true
		}
		idx := int(key[0] - '1')
		if idx >= len(m.tabs) {
			m.statusMsg = fmt.Sprintf("There is no tab %d", idx+1)
			return m, nil, true
		}
		next, cmd := m.switchTab(idx)
		return next, cmd, true

	case keys.Palette:
		return m, m.openPalette(), true
	case keys.Recent:
		return m, m.openRecent(), true

	case keys.ToggleSidebar, keys.TogglePreview:
		err := config.Update(func(c *config.Config) {
			if action == keys.ToggleSidebar {
				c.UI.Sidebar = !c.UI.Sidebar
			} else {
				c.UI.Preview = !c.UI.Preview
			}
		})
		if err != nil {
			m.statusMsg = "Could not save settings: " + err.Error()
		}
		// Focus must not be left behind on a panel that just vanished.
		ui := config.Get().UI
		if (m.active == panelSidebar && !ui.Sidebar) || (m.active == panelPreview && !ui.Preview) {
			m.active = panelFileList
		}
		m.updateSizes()
		// The preview renders media at the panel's size, so a panel that
		// changed width needs its picture drawn again.
		return m, m.reloadPreview(), true

	case keys.History:
		m.historyPopup.Start()
		return m, nil, true
	case keys.Settings:
		m.settingsPopup.Start()
		return m, nil, true
	case keys.SearchName:
		return m, m.searchOverlay.Start(search.ModeNameSearch, m.currentPath), true
	case keys.SearchContent:
		return m, m.searchOverlay.Start(search.ModeContentSearch, m.currentPath), true

	case keys.GitPanel:
		if !config.Get().Git.Enabled {
			m.statusMsg = "git integration is off — press , to turn it back on"
			return m, nil, true
		}
		return m, m.gitOverlay.Start(m.currentPath), true
	case keys.Branches:
		if !config.Get().Git.Enabled {
			m.statusMsg = "git integration is off — press , to turn it back on"
			return m, nil, true
		}
		return m, m.branchesPopup.Start(m.currentPath), true

	case keys.Bookmark:
		added, err := bookmarks.Toggle(m.currentPath)
		switch {
		case err != nil:
			m.statusMsg = "Error saving bookmark: " + err.Error()
		case added:
			m.statusMsg = "Added to favorites"
		default:
			m.statusMsg = "Removed from favorites"
		}
		if err == nil {
			// Rebuild the sidebar to pick the change up, keeping the
			// cursor and focus it had.
			m.sidebar = sidebar.New().WithStateOf(m.sidebar)
			m.updateSizes()
		}
		return m, nil, true

	case keys.SortMenu:
		m.awaitingSort = true
		m.statusMsg = "Sort by: (n)ame, (s)ize, (d)ate"
		return m, nil, true

	case keys.Shell:
		cmd := exec.Command(config.Get().Shell())
		cmd.Dir = m.currentPath
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return filelist.RefreshListMsg{}
		}), true

	case keys.Undo, keys.Redo:
		var (
			status string
			err    error
		)
		if action == keys.Undo {
			status, err = Undo()
		} else {
			status, err = Redo()
		}
		var cmds []tea.Cmd
		if err != nil {
			m.statusMsg = "Error: " + err.Error()
		} else {
			m.statusMsg = status
			cmds = append(cmds, m.listCmd(filelist.RefreshListMsg{}))
		}
		if ActiveDeletesCount() > 0 && !m.deletesTicking {
			m.deletesTicking = true
			cmds = append(cmds, deleteTick())
		}
		return m, tea.Batch(cmds...), true
	}
	return m, nil, false
}

func (m Model) seek(seconds float64) (Model, tea.Cmd, bool) {
	if !m.preview.IsAudio() {
		return m, nil, false
	}
	return m, m.preview.SeekAudio(seconds), true
}

// runAction carries out an action whichever scope it belongs to. This is
// the entry point for requests that did not come through the focused
// panel's own key handling.
func (m Model) runAction(action keys.Action, key string) (tea.Model, tea.Cmd) {
	binding, ok := keys.Find(action)
	if !ok {
		return m, nil
	}
	if binding.Scope == keys.ScopeGlobal {
		next, cmd, _ := m.runGlobal(action, key)
		return next, cmd
	}
	return m.runList(action)
}

// runList carries out a file-list action and then does what the main
// loop does after any input to the list.
func (m Model) runList(action keys.Action) (tea.Model, tea.Cmd) {
	// An action on the list puts the keyboard there. It is where the eye
	// goes next, and for the filter it is a necessity: the list only
	// reads the keys typed into it while it has focus.
	m.active = panelFileList
	m.updateFocus()

	prevCursor, prevPath := m.filelist.CursorIdx(), m.filelist.CurrentPath()
	var cmd tea.Cmd
	m.filelist, cmd = m.filelist.Do(action)
	return m.afterListInput(prevCursor, prevPath, []tea.Cmd{cmd})
}

// afterListInput is the tail every route into the file list shares: if
// the cursor or the directory moved, the preview follows it.
func (m Model) afterListInput(prevCursor int, prevPath string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	if m.filelist.CursorIdx() != prevCursor || m.filelist.CurrentPath() != prevPath {
		m.currentPath = m.filelist.CurrentPath()
		if entry := m.filelist.SelectedEntry(); entry != nil {
			cmds = append(cmds, m.preview.LoadFile(*entry))
			m.stampPreview(entry)
		}
	}
	if ActiveDeletesCount() > 0 && !m.deletesTicking {
		m.deletesTicking = true
		cmds = append(cmds, deleteTick())
	}
	return m, tea.Batch(cmds...)
}

// reloadPreview re-reads whatever the cursor is on.
func (m *Model) reloadPreview() tea.Cmd {
	entry := m.filelist.SelectedEntry()
	if entry == nil {
		return nil
	}
	m.stampPreview(entry)
	return m.preview.LoadFile(*entry)
}

// goTo navigates the active tab to a directory, optionally landing on a
// named entry — the common tail of every "jump somewhere" feature.
func (m Model) goTo(dir, selectName string) (Model, tea.Cmd) {
	m.currentPath = dir
	m.updateLastDirMod(dir)
	var cmd tea.Cmd
	m.filelist, cmd = m.filelist.NavigateTo(dir)
	if selectName != "" {
		m.filelist = m.filelist.SelectByName(selectName)
	}
	m.refreshGit()
	cmds := []tea.Cmd{cmd}
	if entry := m.filelist.SelectedEntry(); entry != nil {
		cmds = append(cmds, m.preview.LoadFile(*entry))
		m.stampPreview(entry)
	}
	return m, tea.Batch(cmds...)
}

// ── Command palette ────────────────────────────────────────────────────

const (
	pickActions = "actions"
	pickRecent  = "recent"
)

// notInPalette lists the actions a palette row cannot sensibly stand
// for: moving the cursor one step, or the palette itself.
var notInPalette = map[keys.Action]bool{
	keys.CursorUp: true, keys.CursorDown: true, keys.Open: true,
	keys.HalfPageUp: true, keys.HalfPageDown: true,
	keys.PanelNext: true, keys.PanelPrev: true,
	keys.Mark: true, keys.ClearMarks: true,
	keys.TabJump: true, keys.Palette: true,
	keys.SeekBack: true, keys.SeekForward: true,
	keys.SeekBackFar: true, keys.SeekFwdFar: true,
}

// openPalette shows every action with the key it is bound to — the way
// to find a feature without knowing its shortcut, and to learn the
// shortcut while doing so.
func (m *Model) openPalette() tea.Cmd {
	var items []picker.Item
	for _, b := range keys.All() {
		if notInPalette[b.Action] {
			continue
		}
		items = append(items, picker.Item{
			ID:     string(b.Action),
			Title:  b.Desc,
			Detail: b.Category,
			Hint:   b.Short(),
		})
	}
	return m.picker.Open(picker.Options{
		Kind:        pickActions,
		Title:       "Actions",
		Placeholder: "type to find an action…",
		Footer:      "Enter run  ·  Esc close",
		Items:       items,
	})
}

// openRecent shows recently opened files and visited places.
func (m *Model) openRecent() tea.Cmd {
	entries := RecentEntries()
	items := make([]picker.Item, 0, len(entries))
	for _, e := range entries {
		icon := icons.GetIcon(filepath.Base(e.Path),
			strings.ToLower(filepath.Ext(e.Path)), e.IsDir, false, false)
		items = append(items, picker.Item{
			ID:        e.Path,
			Title:     filepath.Base(e.Path),
			Detail:    filesystem.ShortenPath(filepath.Dir(e.Path)),
			Hint:      relTime(e.At),
			Icon:      icon.Symbol,
			IconColor: icon.Color,
		})
	}
	return m.picker.Open(picker.Options{
		Kind:        pickRecent,
		Title:       "Recent",
		Placeholder: "files you opened, places you went…",
		Footer:      "Enter open  ·  Tab folder in a new tab  ·  Esc close",
		Items:       items,
	})
}

// picked acts on a row chosen in the picker.
func (m Model) picked(msg picker.ChosenMsg) (tea.Model, tea.Cmd) {
	switch msg.Kind {
	case pickActions:
		return m.runAction(keys.Action(msg.ID), "")

	case pickRecent:
		info, err := os.Stat(msg.ID)
		if err != nil {
			m.statusMsg = "No longer there: " + filesystem.ShortenPath(msg.ID)
			return m, nil
		}
		if info.IsDir() {
			TouchRecent(msg.ID, true)
			if msg.Alt {
				return m.openTab(msg.ID, "")
			}
			return m.goTo(msg.ID, "")
		}
		// A file: go to where it lives, put the cursor on it and open it,
		// which is what Enter on that row would have done. Tab shows it
		// in a new tab instead and leaves it unopened.
		dir, name := filepath.Dir(msg.ID), filepath.Base(msg.ID)
		if msg.Alt {
			return m.openTab(dir, name)
		}
		next, cmd := m.goTo(dir, name)
		path := msg.ID
		open := func() tea.Msg { return filelist.OpenFileRequestMsg{Path: path} }
		return next, tea.Sequence(cmd, open)
	}
	return m, nil
}

// ── Small file operations ──────────────────────────────────────────────

// duplicate copies entries next to themselves, under the first free
// name. It goes on the undo stack as a copy, which is what it is.
func (m Model) duplicate(entries []filesystem.FileEntry) (tea.Model, tea.Cmd) {
	var done []HistoryItem
	var failed error
	for _, e := range entries {
		dst := uniqueDst(e.Path, e.IsDir)
		var err error
		if e.IsDir {
			err = filesystem.CopyDir(e.Path, dst)
		} else {
			err = filesystem.CopyFile(e.Path, dst)
		}
		if err != nil {
			failed = err
			break
		}
		done = append(done, HistoryItem{Src: e.Path, Dst: dst, IsDir: e.IsDir})
	}
	if len(done) > 0 {
		PushHistory(HistoryEvent{Op: OpCopy, Items: done})
	}
	switch {
	case failed != nil:
		m.statusMsg = "Error: " + failed.Error()
	case len(done) == 1:
		m.statusMsg = "Duplicated as " + filepath.Base(done[0].Dst)
	default:
		m.statusMsg = fmt.Sprintf("Duplicated %d items", len(done))
	}
	m.filelist, _ = m.filelist.Update(filelist.ClearSelectionMsg{})
	cmd := m.listCmd(filelist.RefreshListMsg{})
	if len(done) > 0 {
		// Land on the copy: it is what the user is about to rename.
		m.filelist = m.filelist.SelectByName(filepath.Base(done[len(done)-1].Dst))
	}
	return m, cmd
}

// copyPaths puts paths on the macOS clipboard, one per line.
func (m Model) copyPaths(paths []string) (tea.Model, tea.Cmd) {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\n"))
	if err := cmd.Run(); err != nil {
		m.statusMsg = "Error: could not reach the clipboard: " + err.Error()
		return m, nil
	}
	if len(paths) == 1 {
		m.statusMsg = "Copied path: " + filesystem.ShortenPath(paths[0])
	} else {
		m.statusMsg = fmt.Sprintf("Copied %d paths", len(paths))
	}
	m.filelist, _ = m.filelist.Update(filelist.ClearSelectionMsg{})
	return m, nil
}

// openExternal hands a path to macOS — its default application, or a
// Finder window with it selected. `open` returns at once and inherits no
// terminal, so nothing it prints can reach the screen.
func (m Model) openExternal(path string, reveal bool) (tea.Model, tea.Cmd) {
	args := []string{path}
	if reveal {
		args = []string{"-R", path}
		m.statusMsg = "Revealed in Finder: " + filepath.Base(path)
	} else {
		m.statusMsg = "Opened " + filepath.Base(path)
	}
	return m, func() tea.Msg {
		_ = exec.Command("open", args...).Start()
		return nil
	}
}

// statusIsError decides the colour of the status line. Messages are free
// text set in dozens of places; the ones reporting a failure all start
// the same few ways.
func statusIsError(s string) bool {
	lower := strings.ToLower(s)
	for _, prefix := range []string{"error", "cannot", "could not", "paste error", "config:", "no longer"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
