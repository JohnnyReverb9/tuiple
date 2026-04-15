// Package app wires together the sidebar, file list, and preview
// into a single three-panel Bubble Tea application.
package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/components/filelist"
	"tuiple/components/preview"
	"tuiple/components/sidebar"
	"tuiple/filesystem"
	"tuiple/theme"
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

	active      panel
	currentPath string

	width  int
	height int

	showHelp bool
	ready    bool
}

// New creates the application model.
func New(startDir string) Model {
	return Model{
		sidebar:     sidebar.New(),
		filelist:    filelist.New(startDir),
		preview:     preview.New(),
		active:      panelFileList,
		currentPath: startDir,
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
	var cmds []tea.Cmd

	switch msg := msg.(type) {

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
		}

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
		m.preview, _ = m.preview.Update(msg)
		return m, nil
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

	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left,
		breadcrumb,
		sep,
		content,
		sep,
		statusBar,
	)
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
	left := theme.StatusPath.Render(" " + filesystem.ShortenPath(m.currentPath))

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
	items := []struct{ key, desc string }{
		{"↑ / k", "Move up"},
		{"↓ / j", "Move down"},
		{"Enter / → / l", "Enter directory"},
		{"Backspace / ← / h", "Go back"},
		{"~", "Go to home"},
		{".", "Toggle hidden files"},
		{"/", "Filter files"},
		{"Tab / Shift+Tab", "Switch panel"},
		{"g / G", "Go to top / bottom"},
		{"Ctrl+U / Ctrl+D", "Page up / down"},
		{"s", "Sort by name"},
		{"S", "Sort by size"},
		{"?", "Toggle this help"},
		{"q", "Quit"},
	}

	var lines []string
	lines = append(lines, "")
	lines = append(lines, theme.PreviewTitle.Render("  ⌨  Keyboard Shortcuts"))
	lines = append(lines, "")

	for _, it := range items {
		key := theme.HelpKey.Width(24).Render("  " + it.key)
		desc := theme.HelpDesc.Render(it.desc)
		lines = append(lines, key+" "+desc)
	}

	lines = append(lines, "")
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
