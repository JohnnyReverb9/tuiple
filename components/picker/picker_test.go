package picker

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func sample() Options {
	return Options{
		Kind:   "test",
		Title:  "Actions",
		Footer: "Enter run",
		Items: []Item{
			{ID: "tab-new", Title: "New tab here", Detail: "Tabs", Hint: "t"},
			{ID: "new-file", Title: "New file", Detail: "File Operations", Hint: "n"},
			{ID: "git-blame", Title: "Blame the file", Detail: "Git", Hint: "L"},
			{ID: "wide", Title: "Заметки о проекте — 日本語のファイル名", Detail: "~/Documents/a/very/long/path/that/will/not/fit/anywhere"},
		},
	}
}

func typeText(m Model, text string) Model {
	for _, r := range text {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestFilterNarrowsAndEnterChooses(t *testing.T) {
	m := New()
	m.SetSize(100, 30)
	m.Open(sample())

	if len(m.rows) != 4 {
		t.Fatalf("an empty query shows %d rows, want all 4", len(m.rows))
	}

	m = typeText(m, "blame")
	if len(m.rows) != 1 || m.opts.Items[m.rows[0].item].ID != "git-blame" {
		t.Fatalf("query %q left %d rows", "blame", len(m.rows))
	}

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.IsActive() {
		t.Error("the picker stayed open after Enter")
	}
	chosen, ok := cmd().(ChosenMsg)
	if !ok || chosen.ID != "git-blame" || chosen.Kind != "test" || chosen.Alt {
		t.Errorf("chose %+v, want git-blame via Enter", chosen)
	}
}

// The detail is searched as well: typing the name of a group finds what
// is in it, whatever the rows are called.
func TestDetailIsSearched(t *testing.T) {
	m := New()
	m.SetSize(100, 30)
	m.Open(sample())

	m = typeText(m, "git")
	if len(m.rows) == 0 || m.opts.Items[m.rows[0].item].ID != "git-blame" {
		t.Fatalf("a query for the group did not find its action: %d rows", len(m.rows))
	}
}

func TestTabChoosesTheAlternative(t *testing.T) {
	m := New()
	m.SetSize(100, 30)
	m.Open(sample())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	chosen, ok := cmd().(ChosenMsg)
	if !ok || chosen.ID != "new-file" || !chosen.Alt {
		t.Errorf("chose %+v, want new-file with Alt set", chosen)
	}
}

func TestEscClosesWithoutChoosing(t *testing.T) {
	m := New()
	m.SetSize(100, 30)
	m.Open(sample())

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.IsActive() || cmd != nil {
		t.Error("Esc should close the picker and choose nothing")
	}
}

// Whatever is in a row — wide characters, a path far too long — every
// line of the box is the same width, or the border comes out ragged.
func TestEveryLineOfTheBoxIsTheSameWidth(t *testing.T) {
	for _, width := range []int{40, 80, 200} {
		m := New()
		m.SetSize(width, 20)
		m.Open(sample())
		for _, query := range []string{"", "new", "zzzz"} {
			q := typeText(m, query)
			lines := strings.Split(q.View(), "\n")
			want := lipgloss.Width(lines[0])
			for i, line := range lines {
				if w := lipgloss.Width(line); w != want {
					t.Errorf("width %d, query %q: line %d is %d cells, the first is %d",
						width, query, i, w, want)
				}
			}
			if want > width {
				t.Errorf("width %d: the box is %d cells wide", width, want)
			}
		}
	}
}
