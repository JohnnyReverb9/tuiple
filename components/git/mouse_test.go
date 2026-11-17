package git

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func clickAt(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

// The commit tab: clicking a file's [ ] box stages it, clicking it again
// unstages it, and the tab titles switch tabs.
func TestPanelMouseStagesAndSwitchesTabs(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "changed\n")
	write(t, dir, "b.txt", "new\n")

	m := New()
	m.SetSize(120, 30)
	m.Start(dir)
	if len(m.commit.changes) != 2 {
		t.Fatalf("changes = %+v", m.commit.changes)
	}

	// Row 3 is the list header, row 4 the first file; the box is at
	// columns 2–4 (border, lead space, "[ ]").
	m, _ = m.Update(clickAt(3, 4))
	if got := sh(t, dir, "diff", "--cached", "--name-only"); got != "a.txt" {
		t.Fatalf("staged after click = %q", got)
	}
	m, _ = m.Update(clickAt(3, 4))
	if got := sh(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("staged after second click = %q", got)
	}

	// A click on the second row's path only selects it.
	m, _ = m.Update(clickAt(15, 5))
	if m.commit.cursor != 1 {
		t.Fatalf("cursor = %d", m.commit.cursor)
	}
	if got := sh(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("a plain click staged something: %q", got)
	}

	// Tabs: " Commit · Log · Stashes" from column 1.
	view := m.View()
	tabRow := strings.Split(view, "\n")[2]
	if !strings.Contains(tabRow, "Log") {
		t.Fatalf("tab row = %q", tabRow)
	}
	m, _ = m.Update(clickAt(12, 2)) // on "Log"
	if m.tab != TabLog {
		t.Fatalf("tab = %d", m.tab)
	}
}

// The branches popup: a click on a tab switches view, a double click on a
// branch checks it out, a click on a folder folds it.
func TestBranchesPopupMouse(t *testing.T) {
	dir := newRepo(t)
	for _, b := range []string{"task/a", "task/b", "zeta"} {
		sh(t, dir, "branch", b)
	}
	p := NewBranches()
	p.SetSize(90, 24)
	p.Start(dir)

	rowOf := func(name string) int {
		start, _ := windowAround(p.cursor(), p.visibleCount(), 24)
		for i, r := range p.rows {
			if r.label == name {
				return 3 + i - start
			}
		}
		t.Fatalf("no row %q", name)
		return 0
	}
	y := rowOf("zeta")
	p, _ = p.Update(clickAt(10, y))
	p, _ = p.Update(clickAt(10, y))
	if current(t, dir) != "zeta" {
		t.Fatalf("double click did not check out zeta (on %q, status %q)", current(t, dir), p.status)
	}

	p, _ = p.Update(clickAt(10, rowOf("task/")))
	for _, r := range p.rows {
		if r.label == "a" {
			t.Fatal("folder did not fold on click")
		}
	}

	p, _ = p.Update(clickAt(16, 2)) // "Remotes 0" after " Branches 4 ·"
	if p.view != viewRemotes {
		t.Fatalf("view = %d", p.view)
	}
}
