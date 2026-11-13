// Package picker is the quick-open popup: a query field over a list that
// narrows as you type. The command palette, the recent-places list and
// the tab switcher are all this one component with different rows.
//
// It knows nothing about what the rows mean. The app hands it items,
// each with an opaque ID, and gets that ID back in a ChosenMsg — so
// adding another "search a list and pick one" feature costs a slice
// literal, not a popup.
package picker

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/JohnnyReverb9/tuiple/theme"
)

// Item is one row.
type Item struct {
	// ID comes back in ChosenMsg; the picker never looks inside it.
	ID string
	// Title is the text that is matched and highlighted.
	Title string
	// Detail is dim context after the title — a parent directory, the
	// group an action belongs to. It is searched too, so typing "git"
	// finds every git action whatever it is called.
	Detail string
	// Hint is right-aligned: a keyboard shortcut, an age.
	Hint string
	// Icon is a single cell in front of the title; empty leaves it out.
	Icon      string
	IconColor lipgloss.TerminalColor
}

// ChosenMsg reports the row the user picked. Alt is true when it was
// picked with Tab rather than Enter — the "other thing to do with it",
// whose meaning belongs to whoever opened the picker.
type ChosenMsg struct {
	Kind string
	ID   string
	Alt  bool
}

// Options describes one opening of the picker.
type Options struct {
	Kind        string // echoed back in ChosenMsg
	Title       string
	Placeholder string
	Footer      string // key hints under the list
	Items       []Item
}

// row is an item that survived the filter, with the bytes of its title
// that matched the query.
type row struct {
	item    int
	matched map[int]bool
}

// Model is the picker's Bubble Tea model.
type Model struct {
	active bool
	opts   Options
	input  textinput.Model
	rows   []row
	cursor int
	offset int
	width  int
	height int
}

// New creates a closed picker.
func New() Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 120
	ti.PlaceholderStyle = theme.Dim
	ti.TextStyle = theme.Normal
	return Model{input: ti}
}

func (m *Model) SetSize(w, h int) { m.width = w; m.height = h }
func (m Model) IsActive() bool    { return m.active }

// AcceptsText is true whenever the picker is open: every printable key
// goes into the query, so the app must not translate keyboard layouts.
func (m Model) AcceptsText() bool { return m.active }

// Open shows the picker with a fresh query.
func (m *Model) Open(opts Options) tea.Cmd {
	m.active = true
	m.opts = opts
	m.input.SetValue("")
	m.input.Placeholder = opts.Placeholder
	m.cursor = 0
	m.offset = 0
	m.refilter()
	return m.input.Focus()
}

// Close hides the picker.
func (m *Model) Close() {
	m.active = false
	m.input.Blur()
}

// ── Filtering ──────────────────────────────────────────────────────────

// haystack implements fuzzy.Source over the items: the title first, so
// its byte offsets line up with what gets highlighted, then the detail.
type haystack []Item

func (h haystack) Len() int { return len(h) }
func (h haystack) String(i int) string {
	if h[i].Detail == "" {
		return h[i].Title
	}
	return h[i].Title + " " + h[i].Detail
}

func (m *Model) refilter() {
	query := strings.TrimSpace(m.input.Value())
	m.rows = m.rows[:0]
	if query == "" {
		for i := range m.opts.Items {
			m.rows = append(m.rows, row{item: i})
		}
	} else {
		for _, match := range fuzzy.FindFrom(query, haystack(m.opts.Items)) {
			r := row{item: match.Index, matched: make(map[int]bool, len(match.MatchedIndexes))}
			for _, idx := range match.MatchedIndexes {
				r.matched[idx] = true
			}
			m.rows = append(m.rows, r)
		}
	}
	m.cursor = 0
	m.offset = 0
}

// ── Update ─────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.move(-1)
		case tea.MouseButtonWheelDown:
			m.move(1)
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			m.Close()
			return m, nil
		case "up", "ctrl+k", "ctrl+p":
			m.move(-1)
			return m, nil
		case "down", "ctrl+j", "ctrl+n":
			m.move(1)
			return m, nil
		case "pgup", "ctrl+u":
			m.move(-m.listHeight())
			return m, nil
		case "pgdown", "ctrl+d":
			m.move(m.listHeight())
			return m, nil
		case "enter", "tab":
			if m.cursor < 0 || m.cursor >= len(m.rows) {
				if msg.String() == "enter" {
					m.Close()
				}
				return m, nil
			}
			chosen := ChosenMsg{
				Kind: m.opts.Kind,
				ID:   m.opts.Items[m.rows[m.cursor].item].ID,
				Alt:  msg.String() == "tab",
			}
			m.Close()
			return m, func() tea.Msg { return chosen }
		}
	}

	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.refilter()
	}
	return m, cmd
}

func (m *Model) move(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.rows)-1)
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
}

// ── View ───────────────────────────────────────────────────────────────

// innerWidth is the width inside the border.
func (m Model) innerWidth() int {
	return min(max(m.width-6, 30), 76)
}

// listHeight is how many rows the list shows. The box keeps one height
// however few rows match, so it does not jump about while typing.
func (m Model) listHeight() int {
	// border(2) + query(1) + rule(1) + rule(1) + footer(1)
	return min(max(m.height-6, 3), 14)
}

func (m Model) View() string {
	if !m.active {
		return ""
	}
	w := m.innerWidth()
	listH := m.listHeight()

	// Query line: title, the field, and how much of the list is left.
	title := lipgloss.NewStyle().Foreground(theme.AccentBlue).Bold(true).Render(" " + m.opts.Title + " ")
	count := theme.Muted.Render(fmt.Sprintf("%d/%d ", len(m.rows), len(m.opts.Items)))
	fieldW := max(w-lipgloss.Width(title)-lipgloss.Width(count)-1, 8)
	m.input.Width = fieldW - 1
	field := lipgloss.NewStyle().Width(fieldW).MaxWidth(fieldW).Render(m.input.View())
	lines := []string{
		pad(title+" "+field+count, w),
		theme.Separator.Render(strings.Repeat("─", w)),
	}

	if len(m.rows) == 0 {
		lines = append(lines, pad(theme.Dim.Render("  Nothing matches"), w))
	}
	end := min(m.offset+listH, len(m.rows))
	for i := m.offset; i < end; i++ {
		lines = append(lines, m.renderRow(m.rows[i], i == m.cursor, w))
	}
	for len(lines) < 2+listH {
		lines = append(lines, strings.Repeat(" ", w))
	}

	lines = append(lines, theme.Separator.Render(strings.Repeat("─", w)))
	lines = append(lines, pad(theme.Muted.Render(" "+m.opts.Footer), w))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.BorderActive).
		Render(strings.Join(lines, "\n"))
}

// renderRow lays a row out as: icon, title, detail … hint. Every segment
// carries the row background itself, so the highlight is unbroken and
// the row is exactly w cells whatever it contains.
func (m Model) renderRow(r row, cursor bool, w int) string {
	it := m.opts.Items[r.item]

	base := lipgloss.NewStyle()
	if cursor {
		base = base.Background(theme.BgSelected)
	}
	titleStyle := base.Foreground(theme.FgColor)
	if cursor {
		titleStyle = titleStyle.Bold(true)
	}
	matchStyle := titleStyle.Foreground(theme.AccentBlue).Bold(true)
	detailStyle := base.Foreground(theme.FgMutedColor)
	hintStyle := base.Foreground(theme.FgDimColor)

	hintW := lipgloss.Width(it.Hint)
	// " " icon " " … " " hint " "
	room := w - 3 - hintW - 2
	if it.Icon == "" {
		room += 2
	}
	room = max(room, 4)

	// The title gets the room first; the detail takes what is left.
	// Measured in cells, not runes: a CJK name is twice as wide as it
	// is long.
	title := []rune(it.Title)
	cut := false
	if lipgloss.Width(it.Title) > room {
		cut = true
		for len(title) > 1 && lipgloss.Width(string(title)) > room-1 {
			title = title[:len(title)-1]
		}
	}
	var b strings.Builder
	b.WriteString(base.Render(" "))
	if it.Icon != "" {
		iconStyle := base
		if it.IconColor != nil {
			iconStyle = iconStyle.Foreground(it.IconColor)
		}
		b.WriteString(iconStyle.Render(it.Icon))
		b.WriteString(base.Render(" "))
	}
	// Highlight by byte offset — that is what the matcher reports.
	offset := 0
	for _, ch := range title {
		if r.matched[offset] {
			b.WriteString(matchStyle.Render(string(ch)))
		} else {
			b.WriteString(titleStyle.Render(string(ch)))
		}
		offset += len(string(ch))
	}
	used := lipgloss.Width(string(title))
	if cut {
		b.WriteString(titleStyle.Render("…"))
		used++
	}

	if rest := room - used - 2; rest > 3 && it.Detail != "" {
		detail := []rune(it.Detail)
		if lipgloss.Width(it.Detail) > rest {
			// Keep the tail: for a path that is the part that tells
			// two entries apart.
			for len(detail) > 1 && lipgloss.Width(string(detail)) > rest-1 {
				detail = detail[1:]
			}
			detail = append([]rune("…"), detail...)
		}
		b.WriteString(base.Render("  "))
		b.WriteString(detailStyle.Render(string(detail)))
		used += 2 + lipgloss.Width(string(detail))
	}

	b.WriteString(base.Render(strings.Repeat(" ", max(room-used, 0)+1)))
	b.WriteString(hintStyle.Render(it.Hint))
	b.WriteString(base.Render(" "))

	line := b.String()
	if lw := lipgloss.Width(line); lw < w {
		line += base.Render(strings.Repeat(" ", w-lw))
	} else if lw > w {
		line = lipgloss.NewStyle().MaxWidth(w).Render(line)
	}
	return line
}

func pad(s string, w int) string {
	lw := lipgloss.Width(s)
	switch {
	case lw < w:
		return s + strings.Repeat(" ", w-lw)
	case lw > w:
		return lipgloss.NewStyle().MaxWidth(w).Render(s)
	}
	return s
}
