package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{
		"main.go":                "package main\n\nfunc main() {\n\tstartServer()\n}\n",
		"internal/server/run.go": "package server\n\n// startServer boots it.\nfunc startServer() {}\n",
		"docs/notes.md":          "nothing here\n",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// run delivers a command's messages back to the model until it settles.
// Only the popup's own messages are fed back: the text field's cursor
// blink re-arms itself for ever and would never let this return.
func run(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = run(m, c)
		}
	case filesLoadedMsg, debounceMsg, contentFound:
		var next tea.Cmd
		m, next = m.Update(msg)
		m = run(m, next)
	}
	return m
}

// typeIn puts text in the query: all but the last character directly,
// the last as a keystroke, so the popup reacts once. (Typing every
// character would also wait out a cursor blink for each.)
func typeIn(m Model, text string) Model {
	r := []rune(text)
	m.input.SetValue(string(r[:len(r)-1]))
	m.input.CursorEnd()
	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: r[len(r)-1:]})
	return run(m, cmd)
}

func TestGoToFileFindsNestedFilesAndOpens(t *testing.T) {
	root := tree(t)
	m := New()
	m.SetSize(100, 30)
	m = run(m, m.Start(ModeNameSearch, root))
	if len(m.files) != 3 {
		t.Fatalf("listed %d files, want 3: %v", len(m.files), m.files)
	}

	m = typeIn(m, "run")
	if len(m.results) == 0 || m.results[0].Name != filepath.Join("internal", "server", "run.go") {
		t.Fatalf("results = %+v, want run.go first", m.results)
	}
	if len(m.preview) == 0 || m.preview[0] != "package server" {
		t.Errorf("preview = %q, want the top of the file", m.preview)
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	done, ok := cmd().(SearchCompletedMsg)
	if !ok || done.SelectedPath != filepath.Join(root, "internal", "server", "run.go") || done.Reveal {
		t.Errorf("Enter produced %+v", done)
	}
}

func TestFindInFilesShowsTheMatchInContext(t *testing.T) {
	root := tree(t)
	m := New()
	m.SetSize(100, 30)
	m = run(m, m.Start(ModeContentSearch, root))
	m = typeIn(m, "startServer")

	if len(m.results) != 3 {
		t.Fatalf("%d matches, want 3: %+v", len(m.results), m.results)
	}
	for _, r := range m.results {
		if r.LineNum == 0 || !strings.Contains(r.MatchedLine, "startServer") {
			t.Errorf("bad match %+v", r)
		}
	}
	if m.previewHit != m.results[0].LineNum || len(m.preview) == 0 {
		t.Errorf("the preview does not point at the match: hit %d, %d lines", m.previewHit, len(m.preview))
	}

	// A result for a query that has since changed is dropped.
	stale := contentFound{seq: m.seq - 1}
	m, _ = m.Update(stale)
	if len(m.results) != 3 {
		t.Error("a stale result replaced the current one")
	}

	lines := strings.Split(m.View(), "\n")
	for i, line := range lines {
		if w := lipgloss.Width(line); w != lipgloss.Width(lines[0]) {
			t.Errorf("line %d of the box is %d cells, the first is %d", i, w, lipgloss.Width(lines[0]))
		}
	}
	if len(lines) > 30 {
		t.Errorf("the box is %d rows in a 30-row area", len(lines))
	}
}
