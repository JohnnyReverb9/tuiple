// Package sidebar implements the left panel with favorites and locations.
package sidebar

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/bookmarks"
	"tuiple/filesystem"
	"tuiple/theme"
)

// NavigateMsg is sent when the user selects a sidebar destination.
type NavigateMsg struct {
	Path string
}

// ── Internal types ─────────────────────────────────────────────────────

type item struct {
	name string
	path string
}

type section struct {
	title string
	items []item
}

// ── Model ──────────────────────────────────────────────────────────────

// Model is the sidebar's Bubble Tea model.
type Model struct {
	sections []section
	cursor   int
	focused  bool
	width    int
	height   int
}

// New creates a sidebar populated with system locations.
func New() Model {
	home := filesystem.HomeDir()

	favItems := filterExisting([]item{
		{"Home", home},
		{"Desktop", filepath.Join(home, "Desktop")},
		{"Documents", filepath.Join(home, "Documents")},
		{"Downloads", filepath.Join(home, "Downloads")},
	})

	// Add dynamic favorites
	for _, f := range bookmarks.GetFavorites() {
		// Only add if it still exists
		if _, err := os.Stat(f); err == nil {
			favItems = append(favItems, item{
				name: filepath.Base(f),
				path: f,
			})
		}
	}

	locItems := []item{
		{"Macintosh HD", "/"},
	}
	// Discover mounted volumes
	if volumes, err := os.ReadDir("/Volumes"); err == nil {
		for _, v := range volumes {
			if v.Name() != "Macintosh HD" {
				locItems = append(locItems, item{
					name: v.Name(),
					path: filepath.Join("/Volumes", v.Name()),
				})
			}
		}
	}

	return Model{
		sections: []section{
			{title: "FAVORITES", items: favItems},
			{title: "LOCATIONS", items: locItems},
		},
	}
}

func filterExisting(items []item) []item {
	var result []item
	for _, it := range items {
		if _, err := os.Stat(it.path); err == nil {
			result = append(result, it)
		}
	}
	return result
}

// ── Size / focus setters ───────────────────────────────────────────────

func (m *Model) SetSize(w, h int) { m.width = w; m.height = h }
func (m *Model) SetFocused(f bool) { m.focused = f }

// ── Helpers ────────────────────────────────────────────────────────────

func (m Model) totalItems() int {
	n := 0
	for _, s := range m.sections {
		n += len(s.items)
	}
	return n
}

func (m Model) getItem(idx int) *item {
	i := 0
	for si := range m.sections {
		for j := range m.sections[si].items {
			if i == idx {
				return &m.sections[si].items[j]
			}
			i++
		}
	}
	return nil
}

// ── Update ─────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < m.totalItems()-1 {
				m.cursor++
			}
		case "enter", "right", "l":
			if it := m.getItem(m.cursor); it != nil {
				return m, func() tea.Msg {
					return NavigateMsg{Path: it.path}
				}
			}
		}
	}

	return m, nil
}

// ── View ───────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	var lines []string
	flatIdx := 0

	for si, s := range m.sections {
		if si > 0 {
			lines = append(lines, "") // spacing between sections
		}
		lines = append(lines, theme.SidebarSection.Render(s.title))

		for _, it := range s.items {
			name := it.name

			// Truncate if needed. The "..." marker is 3 cells (it used to
			// be a single-cell "…" but we standardised on ASCII), so we
			// have to reserve 3 cells for it, not 1.
			maxW := m.width - 2
			if maxW > 3 && lipgloss.Width(name) > maxW {
				name = name[:maxW-3] + "..."
			}

			// Pad to fill row
			padded := name + strings.Repeat(" ", max(0, m.width-2-lipgloss.Width(name)))

			var style lipgloss.Style
			switch {
			case flatIdx == m.cursor && m.focused:
				style = theme.SidebarActive
			case flatIdx == m.cursor:
				style = theme.SidebarSelected
			default:
				style = theme.SidebarItem
			}

			lines = append(lines, " "+style.Render(padded))
			flatIdx++
		}
	}

	// Normalise every line to exactly m.width cells and pad the block to
	// m.height rows. This guarantees that every terminal cell within the
	// sidebar panel gets written on every frame; otherwise Bubble Tea's
	// differential renderer leaves stale characters from previous frames
	// (visible as "oks" instead of "Bookmarks" after navigation).
	for i, line := range lines {
		if strings.ContainsAny(line, "\r\n") {
			line = strings.ReplaceAll(line, "\r", "")
			line = strings.ReplaceAll(line, "\n", " ")
		}
		w := lipgloss.Width(line)
		switch {
		case w < m.width:
			line += strings.Repeat(" ", m.width-w)
		case w > m.width:
			line = lipgloss.NewStyle().MaxWidth(m.width).Render(line)
		}
		lines[i] = line
	}
	if m.height > 0 {
		blank := strings.Repeat(" ", m.width)
		for len(lines) < m.height {
			lines = append(lines, blank)
		}
		if len(lines) > m.height {
			lines = lines[:m.height]
		}
	}
	return strings.Join(lines, "\n")
}
