// Package app wires together the sidebar, file list, and preview
// into a single three-panel Bubble Tea application.
package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/bookmarks"
	"tuiple/clipboard"
	"tuiple/components/filelist"
	"tuiple/components/preview"
	"tuiple/components/search"
	"tuiple/components/sidebar"
	"tuiple/filesystem"
	"tuiple/theme"
)

// ── Dialog / State ─────────────────────────────────────────────────────

type DialogMode int

const (
	DialogNone DialogMode = iota
	DialogDelete
	DialogRename
	DialogNewFile
	DialogNewDir
)

// ── Panel enum ─────────────────────────────────────────────────────────

type panel int

const (
	panelSidebar panel = iota
	panelFileList
	panelPreview
)

// ── Model ──────────────────────────────────────────────────────────────

// Model is the root application model.
type Model struct {
	sidebar  sidebar.Model
	filelist filelist.Model
	preview  preview.Model

	textInput  textinput.Model
	dialogMode DialogMode
	opEntries  []filesystem.FileEntry
	statusMsg  string

	searchOverlay    search.Model
	awaitingBookmark bool
	awaitingJump     bool
	awaitingSort     bool

	active      panel
	currentPath string

	width  int
	height int

	showHelp bool
	ready    bool
}

// New creates the application model.
func New(startDir string) Model {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.CharLimit = 255
	ti.Width = 40
	ti.PlaceholderStyle = theme.Dim
	ti.TextStyle = theme.Normal

	return Model{
		sidebar:       sidebar.New(),
		filelist:      filelist.New(startDir),
		preview:       preview.New(),
		textInput:     ti,
		searchOverlay: search.New(),
		active:        panelFileList,
		currentPath:   startDir,
	}
}

// ── Init ───────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	// Load preview for the first selected file
	if entry := m.filelist.SelectedEntry(); entry != nil {
		return m.preview.LoadFile(*entry)
	}
	return nil
}

// ── Update ─────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// ── Intercept Search Overlay ───────────────────────────────────
	if m.searchOverlay.IsActive() {
		var cmd tea.Cmd
		prevActive := m.searchOverlay.IsActive()
		m.searchOverlay, cmd = m.searchOverlay.Update(msg)

		if compMsg, ok := msg.(search.SearchCompletedMsg); ok {
			dir := filepath.Dir(compMsg.SelectedPath)
			m.currentPath = dir
			var listCmd tea.Cmd
			m.filelist, listCmd = m.filelist.NavigateTo(dir)
			return m, tea.Batch(cmd, listCmd)
		}

		if !m.searchOverlay.IsActive() && prevActive {
			return m, cmd
		}
		return m, cmd
	}

	// ── Intercept Dialog keys ──────────────────────────────────────
	if m.dialogMode != DialogNone {
		return m.updateDialog(msg)
	}

	// ── Bookmarks & Sorting ───────────────────────────────────────
	if m.awaitingBookmark {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			m.awaitingBookmark = false
			if len(keyMsg.Runes) > 0 {
				r := keyMsg.Runes[0]
				bookmarks.Set(r, m.currentPath)
				m.statusMsg = fmt.Sprintf("Saved mark '%c'", r)
			}
			return m, nil
		}
	}
	if m.awaitingJump {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			m.awaitingJump = false
			if len(keyMsg.Runes) > 0 {
				r := keyMsg.Runes[0]
				if path, ok := bookmarks.Get(r); ok {
					m.currentPath = path
					var cmd tea.Cmd
					m.filelist, cmd = m.filelist.NavigateTo(path)
					m.statusMsg = fmt.Sprintf("Jumped to mark '%c'", r)
					return m, cmd
				} else {
					m.statusMsg = fmt.Sprintf("Mark '%c' not set", r)
				}
			}
			return m, nil
		}
	}
	if m.awaitingSort {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			m.awaitingSort = false
			switch keyMsg.String() {
			case "n":
				m.filelist, _ = m.filelist.Update(filelist.SortListMsg{Mode: filesystem.SortByName})
				m.statusMsg = "Sorted by name"
			case "s":
				m.filelist, _ = m.filelist.Update(filelist.SortListMsg{Mode: filesystem.SortBySize})
				m.statusMsg = "Sorted by size"
			case "d":
				m.filelist, _ = m.filelist.Update(filelist.SortListMsg{Mode: filesystem.SortByDate})
				m.statusMsg = "Sorted by date"
			default:
				m.statusMsg = "Sort cancelled"
			}
			return m, nil
		}
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	// ── Mouse Router ───────────────────────────────────────────────
	case tea.MouseMsg:
		if msg.Type != tea.MouseWheelUp && msg.Type != tea.MouseWheelDown {
			// Ignore hover/motion events to prevent application freeze/flood
			return m, nil
		}
		sidebarW, filelistW, _ := m.panelWidths()
		if msg.X > sidebarW+filelistW+1 {
			// Hover over Preview
			m.preview, _ = m.preview.Update(msg)
		} else if msg.X > sidebarW {
			// Hover over Filelist
			prevCursor := m.filelist.CursorIdx()
			m.filelist, _ = m.filelist.Update(msg)
			if m.filelist.CursorIdx() != prevCursor {
				m.currentPath = m.filelist.CurrentPath()
				if entry := m.filelist.SelectedEntry(); entry != nil {
					cmds = append(cmds, m.preview.LoadFile(*entry))
				}
			}
		} else {
			// Hover over Sidebar
			m.sidebar, _ = m.sidebar.Update(msg)
		}
		return m, tea.Batch(cmds...)

	// ── Resize ─────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateSizes()
		m.ready = true

		if entry := m.filelist.SelectedEntry(); entry != nil {
			cmds = append(cmds, m.preview.LoadFile(*entry))
		}
		return m, tea.Batch(cmds...)

	// ── Global keys ────────────────────────────────────────────────
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "-", "−": // Catch both ASCII minus and Unicode minus just in case
			if m.preview.IsAudio() {
				return m, m.preview.SeekAudio(-5)
			}
		case "=", "+":
			if m.preview.IsAudio() {
				// if shifted, it's + which usually means plus, but we'll use + for +30 and = for +5
				if msg.String() == "+" {
					return m, m.preview.SeekAudio(30)
				}
				return m, m.preview.SeekAudio(5)
			}
		case "_":
			if m.preview.IsAudio() {
				return m, m.preview.SeekAudio(-30)
			}
		case "tab":
			m.cyclePanel(1)
			m.updateFocus()
			return m, nil
		case "shift+tab":
			m.cyclePanel(-1)
			m.updateFocus()
			return m, nil
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "f":
			return m, m.searchOverlay.Start(search.ModeNameSearch, m.currentPath)
		case "F":
			return m, m.searchOverlay.Start(search.ModeContentSearch, m.currentPath)
		case "m":
			m.awaitingBookmark = true
			m.statusMsg = "Press character to mark..."
			return m, nil
		case "'":
			m.awaitingJump = true
			m.statusMsg = "Press character to jump..."
			return m, nil
		case "o":
			m.awaitingSort = true
			m.statusMsg = "Sort by: (n)ame, (s)ize, (d)ate"
			return m, nil
		case "S":
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "sh"
			}
			cmd := exec.Command(shell)
			cmd.Dir = m.currentPath
			return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
				return filelist.RefreshListMsg{}
			})
		}

	// ── File Operations ────────────────────────────────────────────
	case filelist.CopyMsg:
		var items []clipboard.Item
		for _, e := range msg.Entries {
			items = append(items, clipboard.Item{Entry: e})
		}
		clipboard.Set(items, clipboard.OpCopy)
		m.statusMsg = fmt.Sprintf("Copied %d items", len(items))
		m.filelist, _ = m.filelist.Update(filelist.ClearSelectionMsg{})
		return m, nil
	case filelist.CutMsg:
		var items []clipboard.Item
		for _, e := range msg.Entries {
			items = append(items, clipboard.Item{Entry: e})
		}
		clipboard.Set(items, clipboard.OpCut)
		m.statusMsg = fmt.Sprintf("Cut %d items", len(items))
		m.filelist, _ = m.filelist.Update(filelist.ClearSelectionMsg{})
		return m, nil
	case filelist.PasteRequestMsg:
		items, op := clipboard.Get()
		if len(items) == 0 {
			m.statusMsg = "Clipboard is empty"
			return m, nil
		}
		return m.handlePaste(items, op)
	case filelist.DeleteRequestMsg:
		m.dialogMode = DialogDelete
		m.opEntries = msg.Entries
		return m, nil
	case filelist.RenameRequestMsg:
		m.dialogMode = DialogRename
		m.opEntries = []filesystem.FileEntry{msg.Entry}
		m.textInput.SetValue(msg.Entry.Name)
		m.textInput.Focus()
		return m, nil
	case filelist.OpenFileRequestMsg:
		// If it's an audio file and it's currently loaded in preview,
		// toggle playback instead of opening in an editor.
		if m.preview.IsAudio() && m.preview.Path() == msg.Path {
			cmd := m.preview.ToggleAudio()
			return m, cmd
		}

		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi" // Default fallback editor
		}
		cmd := exec.Command(editor, msg.Path)
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return filelist.RefreshListMsg{}
		})
	case filelist.CreateFileRequestMsg:
		m.dialogMode = DialogNewFile
		m.textInput.SetValue("")
		m.textInput.Focus()
		return m, nil
	case filelist.CreateDirRequestMsg:
		m.dialogMode = DialogNewDir
		m.textInput.SetValue("")
		m.textInput.Focus()
		return m, nil

	// ── Sidebar navigation ─────────────────────────────────────────
	case sidebar.NavigateMsg:
		m.currentPath = msg.Path
		var cmd tea.Cmd
		m.filelist, cmd = m.filelist.NavigateTo(msg.Path)
		cmds = append(cmds, cmd)
		if entry := m.filelist.SelectedEntry(); entry != nil {
			cmds = append(cmds, m.preview.LoadFile(*entry))
		}
		return m, tea.Batch(cmds...)

	// ── Dir changed in file list ───────────────────────────────────
	case filelist.DirChangedMsg:
		m.currentPath = msg.Path
		if entry := m.filelist.SelectedEntry(); entry != nil {
			cmds = append(cmds, m.preview.LoadFile(*entry))
		}
		return m, tea.Batch(cmds...)

	// ── Preview content loaded ─────────────────────────────────────
	case preview.ContentLoadedMsg:
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd

	// ── Audio tick / seek routed regardless of active panel ────────
	case preview.AudioTickMsg:
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd

	case preview.AudioSeekFinishedMsg:
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd
	}

	// ── Route to active panel ──────────────────────────────────────
	prevCursor := m.filelist.CursorIdx()
	prevPath := m.filelist.CurrentPath()

	var cmd tea.Cmd
	switch m.active {
	case panelSidebar:
		m.sidebar, cmd = m.sidebar.Update(msg)
		cmds = append(cmds, cmd)
	case panelFileList:
		m.filelist, cmd = m.filelist.Update(msg)
		cmds = append(cmds, cmd)
	case panelPreview:
		m.preview, cmd = m.preview.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Cursor or path changed → refresh preview
	if m.filelist.CursorIdx() != prevCursor || m.filelist.CurrentPath() != prevPath {
		m.currentPath = m.filelist.CurrentPath()
		if entry := m.filelist.SelectedEntry(); entry != nil {
			cmds = append(cmds, m.preview.LoadFile(*entry))
		}
	}

	return m, tea.Batch(cmds...)
}

// ── View ───────────────────────────────────────────────────────────────

func (m Model) View() string {
	if !m.ready {
		return "\n  Loading tuiple…"
	}
	if m.showHelp {
		return m.renderHelp()
	}

	breadcrumb := m.renderBreadcrumb()
	sep := theme.Separator.Render(strings.Repeat("─", m.width))

	// Panel dimensions
	sidebarW, filelistW, previewW := m.panelWidths()
	contentH := m.contentHeight()

	// Render panels into fixed-size boxes
	sidebarBox := lipgloss.Place(sidebarW, contentH, lipgloss.Left, lipgloss.Top, m.sidebar.View())
	filelistBox := lipgloss.Place(filelistW, contentH, lipgloss.Left, lipgloss.Top, m.filelist.View())
	previewBox := lipgloss.Place(previewW, contentH, lipgloss.Left, lipgloss.Top, m.preview.View())

	// Vertical separator column
	vsep := m.renderVSep(contentH)

	content := lipgloss.JoinHorizontal(lipgloss.Top,
		sidebarBox, vsep, filelistBox, vsep, previewBox,
	)

	// Search Overlay
	if m.searchOverlay.IsActive() {
		m.searchOverlay.SetWidth(m.width / 2)
		overlay := m.searchOverlay.View()
		content = lipgloss.Place(m.width, contentH, lipgloss.Center, lipgloss.Center, overlay)
	}

	statusBar := m.renderStatusBar()

	ui := lipgloss.JoinVertical(lipgloss.Left,
		breadcrumb,
		sep,
		content,
		sep,
		statusBar,
	)

	return ui
}

// ── Layout helpers ─────────────────────────────────────────────────────

func (m *Model) cyclePanel(dir int) {
	m.active = panel((int(m.active) + dir + 3) % 3)
}

func (m *Model) updateFocus() {
	m.sidebar.SetFocused(m.active == panelSidebar)
	m.filelist.SetFocused(m.active == panelFileList)
	m.preview.SetFocused(m.active == panelPreview)
}

func (m *Model) updateSizes() {
	sidebarW, filelistW, previewW := m.panelWidths()
	contentH := m.contentHeight()

	m.sidebar.SetSize(sidebarW, contentH)
	m.filelist.SetSize(filelistW, contentH)
	m.preview.SetSize(previewW, contentH)
	m.updateFocus()
}

func (m Model) panelWidths() (int, int, int) {
	available := m.width - 2 // 2 separator columns (│)

	sidebarW := clamp(available*20/100, 14, 28)
	previewW := clamp(available*30/100, 16, available/2)
	filelistW := available - sidebarW - previewW
	if filelistW < 20 {
		filelistW = 20
	}
	return sidebarW, filelistW, previewW
}

func (m Model) contentHeight() int {
	// breadcrumb(1) + sep(1) + content + sep(1) + statusbar(1) = content + 4
	return max(1, m.height-4)
}

// ── File operation handlers ──────────────────────────────────────────────

func (m Model) updateDialog(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.dialogMode = DialogNone
			m.textInput.Blur()
			return m, nil
		}

		if m.dialogMode == DialogDelete {
			if msg.String() == "y" || msg.String() == "Y" {
				var delErr error
				for _, entry := range m.opEntries {
					if err := filesystem.Remove(entry.Path, entry.IsDir); err != nil {
						delErr = err
						break
					}
				}
				m.dialogMode = DialogNone
				if delErr != nil {
					m.statusMsg = "Error: " + delErr.Error()
				} else {
					m.statusMsg = fmt.Sprintf("Deleted %d item(s)", len(m.opEntries))
					m.filelist, _ = m.filelist.Update(filelist.RefreshListMsg{})
					m.filelist, _ = m.filelist.Update(filelist.ClearSelectionMsg{})
				}
				return m, nil
			} else if msg.String() == "n" || msg.String() == "N" {
				m.dialogMode = DialogNone
				return m, nil
			}
			return m, nil
		}

		if msg.String() == "enter" {
			val := m.textInput.Value()
			if val != "" {
				var err error
				switch m.dialogMode {
				case DialogRename:
					err = filesystem.Move(m.opEntries[0].Path, filepath.Join(m.currentPath, val))
				case DialogNewFile:
					err = filesystem.CreateFile(filepath.Join(m.currentPath, val))
				case DialogNewDir:
					err = filesystem.CreateDir(filepath.Join(m.currentPath, val))
				}
				if err != nil {
					m.statusMsg = "Error: " + err.Error()
				} else {
					m.statusMsg = "Done."
					m.filelist, _ = m.filelist.Update(filelist.RefreshListMsg{})
				}
			}
			m.dialogMode = DialogNone
			m.textInput.Blur()
			return m, nil
		}

		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handlePaste(items []clipboard.Item, op clipboard.OpType) (tea.Model, tea.Cmd) {
	for _, item := range items {
		dst := filepath.Join(m.currentPath, item.Entry.Name)
		var err error
		if op == clipboard.OpCut {
			err = filesystem.Move(item.Entry.Path, dst)
		} else {
			if item.Entry.IsDir {
				err = filesystem.CopyDir(item.Entry.Path, dst)
			} else {
				err = filesystem.CopyFile(item.Entry.Path, dst)
			}
		}
		if err != nil {
			m.statusMsg = "Paste error: " + err.Error()
			return m, nil
		}
	}
	if op == clipboard.OpCut {
		clipboard.Clear()
	}
	m.statusMsg = "Pasted successfully"
	m.filelist, _ = m.filelist.Update(filelist.RefreshListMsg{})
	return m, nil
}

// ── Breadcrumb ─────────────────────────────────────────────────────────

func (m Model) renderBreadcrumb() string {
	short := filesystem.ShortenPath(m.currentPath)
	parts := strings.Split(short, "/")

	var rendered []string
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == len(parts)-1 {
			rendered = append(rendered, theme.BreadcrumbActive.Render(part))
		} else {
			rendered = append(rendered, theme.BreadcrumbNormal.Render(part))
		}
	}

	if len(rendered) == 0 {
		rendered = append(rendered, theme.BreadcrumbActive.Render("/"))
	}

	path := strings.Join(rendered, theme.BreadcrumbSep.Render(" / "))
	return " " + path
}

// ── Vertical separator ────────────────────────────────────────────────

func (m Model) renderVSep(h int) string {
	ch := theme.Separator.Render("│")
	lines := make([]string, h)
	for i := range lines {
		lines[i] = ch
	}
	return strings.Join(lines, "\n")
}

// ── Status bar ─────────────────────────────────────────────────────────

func (m Model) renderStatusBar() string {
	if m.dialogMode == DialogDelete {
		title := m.opEntries[0].Name
		if len(m.opEntries) > 1 {
			title = fmt.Sprintf("%d items", len(m.opEntries))
		}
		return theme.StatusBar.Width(m.width).Render(fmt.Sprintf(" Delete %s? (y/N) ", title))
	} else if m.dialogMode != DialogNone {
		prefix := " Rename: "
		if m.dialogMode == DialogNewFile {
			prefix = " New File: "
		} else if m.dialogMode == DialogNewDir {
			prefix = " New Dir: "
		}
		return theme.StatusBar.Width(m.width).Render(prefix + m.textInput.View())
	}

	leftStr := " " + filesystem.ShortenPath(m.currentPath)
	if m.statusMsg != "" {
		leftStr += "  [" + m.statusMsg + "]"
	}
	left := theme.StatusPath.Render(leftStr)

	entryCount := len(m.filelist.Entries())
	info := fmt.Sprintf("%d items", entryCount)

	if entry := m.filelist.SelectedEntry(); entry != nil {
		info += " · " + entry.Name
		if !entry.IsDir {
			info += " · " + filesystem.FormatSize(entry.Size)
		}
	}

	right := theme.StatusInfo.Render(info + " ")

	gap := max(0, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

// ── Help screen ────────────────────────────────────────────────────────

func (m Model) renderHelp() string {
	categories := []struct {
		title string
		items []struct{ key, desc string }
	}{
		{"Navigation", []struct{ key, desc string }{
			{"↑/k, ↓/j", "Move up / down"},
			{"Enter / l", "Enter dir / Open file"},
			{"Backspace / h", "Go back to parent"},
			{"g / G", "Go to top / bottom"},
			{"Ctrl+U / Ctrl+D", "Page up / down"},
			{"~", "Go to home directory"},
			{"Tab / Shift+Tab", "Switch active panel"},
		}},
		{"File Operations", []struct{ key, desc string }{
			{"Space", "Toggle selection (Multi-select)"},
			{"Esc", "Clear all selections"},
			{"c / x / p", "Copy / Cut / Paste"},
			{"d", "Delete"},
			{"r", "Rename"},
			{"n / N", "New File / New Directory"},
		}},
		{"Search & Bookmarks", []struct{ key, desc string }{
			{"f", "Search file by name (Fuzzy)"},
			{"F", "Search in file contents"},
			{"/", "Live list filter"},
			{"m + <char>", "Save bookmark to <char>"},
			{"' + <char>", "Jump to bookmark <char>"},
		}},
		{"System & Options", []struct{ key, desc string }{
			{".", "Toggle hidden files"},
			{"o n / o s / o d", "Sort by: Name / Size / Date"},
			{"S (Shift+s)", "Open Subshell here"},
			{"?", "Toggle this help screen"},
			{"q / Ctrl+C", "Quit"},
		}},
	}

	var lines []string
	lines = append(lines, "")
	lines = append(lines, theme.PreviewTitle.Render("  ⌨  Tuiple — Keyboard Shortcuts"))
	lines = append(lines, "")

	for _, cat := range categories {
		lines = append(lines, theme.ListHeader.Render("  "+cat.title))
		for _, it := range cat.items {
			key := theme.HelpKey.Width(24).Render("    " + it.key)
			desc := theme.HelpDesc.Render(it.desc)
			lines = append(lines, key+" "+desc)
		}
		lines = append(lines, "")
	}

	lines = append(lines, theme.Dim.Render("  Press ? to close"))

	return strings.Join(lines, "\n")
}

// ── Utilities ──────────────────────────────────────────────────────────

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
