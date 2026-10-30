package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// divergedRepo has main and feature each changing a.txt, and a dirty
// working tree on main whose a.txt edit blocks a plain checkout.
func divergedRepo(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	sh(t, dir, "switch", "-q", "-c", "feature")
	write(t, dir, "a.txt", "feature\n")
	sh(t, dir, "commit", "-q", "-am", "feature edit")
	sh(t, dir, "switch", "-q", "main")
	write(t, dir, "b.txt", "main\n")
	sh(t, dir, "add", ".")
	sh(t, dir, "commit", "-q", "-m", "main edit")
	return dir
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// A dirty file that differs between branches blocks a plain checkout;
// smart checkout carries the edit over when it merges cleanly.
func TestSmartSwitchCleanRestore(t *testing.T) {
	dir := newRepo(t)
	lines := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"
	write(t, dir, "a.txt", lines)
	sh(t, dir, "commit", "-q", "-am", "ten lines")
	sh(t, dir, "switch", "-q", "-c", "feature")
	write(t, dir, "a.txt", strings.Replace(lines, "1\n", "one\n", 1))
	sh(t, dir, "commit", "-q", "-am", "feature edits line 1")
	sh(t, dir, "switch", "-q", "main")

	write(t, dir, "a.txt", strings.Replace(lines, "10\n", "ten\n", 1))
	write(t, dir, "new.txt", "untracked\n")
	if err := Switch(dir, "feature"); !IsLocalChangesError(err) {
		t.Fatalf("plain switch: %v", err)
	}

	_, conflicted, err := SmartSwitch(dir, Branch{Name: "feature"})
	if err != nil || conflicted {
		t.Fatalf("SmartSwitch: conflicted=%v err=%v", conflicted, err)
	}
	if current(t, dir) != "feature" {
		t.Fatalf("on %q", current(t, dir))
	}
	data, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if !strings.HasPrefix(string(data), "one\n") || !strings.HasSuffix(string(data), "ten\n") {
		t.Fatalf("a.txt = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal("untracked file lost")
	}
	if got := sh(t, dir, "stash", "list"); got != "" {
		t.Fatalf("stash left behind: %q", got)
	}
}

// When the carried edit conflicts, the switch stands, the conflict is in
// the working tree, and the stash is kept as a backup.
func TestSmartSwitchConflict(t *testing.T) {
	dir := divergedRepo(t)
	write(t, dir, "a.txt", "dirty\n")
	_, conflicted, err := SmartSwitch(dir, Branch{Name: "feature"})
	if err != nil || !conflicted {
		t.Fatalf("SmartSwitch: conflicted=%v err=%v", conflicted, err)
	}
	if current(t, dir) != "feature" {
		t.Fatalf("on %q", current(t, dir))
	}
	if got := sh(t, dir, "stash", "list"); !strings.Contains(got, "smart checkout") {
		t.Fatalf("stash backup missing: %q", got)
	}
}

func TestCompare(t *testing.T) {
	dir := divergedRepo(t)
	cmp, err := Compare(dir, "main", "feature", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp.OnlyOther) != 1 || cmp.OnlyOther[0].Subject != "feature edit" {
		t.Fatalf("OnlyOther = %+v", cmp.OnlyOther)
	}
	if len(cmp.OnlyBase) != 1 || cmp.OnlyBase[0].Subject != "main edit" {
		t.Fatalf("OnlyBase = %+v", cmp.OnlyBase)
	}
	if !strings.Contains(cmp.Stat, "1 file changed") {
		t.Fatalf("Stat = %q", cmp.Stat)
	}
}

func TestRecentBranches(t *testing.T) {
	dir := newRepo(t)
	for _, b := range []string{"one", "two", "three"} {
		sh(t, dir, "switch", "-q", "-c", b)
	}
	sh(t, dir, "switch", "-q", "one")
	sh(t, dir, "branch", "-q", "-D", "two")
	// Current is "one"; newest first, deleted "two" dropped.
	got := strings.Join(RecentBranches(dir, 5), ",")
	if got != "three,main" {
		t.Fatalf("RecentBranches = %q", got)
	}
}

func TestPopupGroupsBranches(t *testing.T) {
	dir := newRepo(t)
	for _, b := range []string{"feature/a", "feature/b", "fix/only", "zeta"} {
		sh(t, dir, "branch", b)
	}
	sh(t, dir, "switch", "-q", "zeta")
	sh(t, dir, "switch", "-q", "main")

	p := NewBranches()
	p.Start(dir)
	var got []string
	for _, r := range p.rows {
		got = append(got, strings.Repeat("  ", r.depth)+r.label)
	}
	want := []string{"Recent", "zeta", "Local", "feature/", "  a", "  b", "fix/only", "main", "zeta"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %q", got)
	}
	if p.rows[p.cursor()].kind == rowSection {
		t.Fatal("cursor on a section header")
	}

	// Fold feature/: Enter on the folder row hides its branches.
	for i, r := range p.rows {
		if r.kind == rowFolder {
			p.cursors[viewBranches] = i
		}
	}
	p, _ = p.Update(key("enter"))
	for _, r := range p.rows {
		if r.label == "a" {
			t.Fatal("folder did not fold")
		}
	}

	// A filter flattens the tree.
	p.filter = "feature"
	p.applyFilter()
	if len(p.rows) != 2 || p.rows[0].label != "feature/a" {
		t.Fatalf("filtered rows = %+v", p.rows)
	}
}

func TestPopupSmartCheckoutPrompt(t *testing.T) {
	dir := divergedRepo(t)
	write(t, dir, "a.txt", "dirty\n")

	p := NewBranches()
	p.Start(dir)
	for i, r := range p.rows {
		if r.kind == rowBranch && p.branches[r.branch].Name == "feature" {
			p.cursors[viewBranches] = i
		}
	}
	p, _ = p.Update(key("enter"))
	if p.pending != pendingSmartCheckout {
		t.Fatalf("pending = %v, status %q", p.pending, p.status)
	}
	p, _ = p.Update(key("y"))
	if current(t, dir) != "feature" {
		t.Fatalf("on %q, status %q", current(t, dir), p.status)
	}
}

func TestPopupCompareView(t *testing.T) {
	dir := divergedRepo(t)
	p := NewBranches()
	p.SetSize(90, 20)
	p.Start(dir)
	for i, r := range p.rows {
		if r.kind == rowBranch && p.branches[r.branch].Name == "feature" {
			p.cursors[viewBranches] = i
		}
	}
	p, _ = p.Update(key("c"))
	if p.compare == nil {
		t.Fatalf("compare not open: %q", p.status)
	}
	view := p.View()
	if !strings.Contains(view, "feature edit") || !strings.Contains(view, "main edit") {
		t.Fatalf("compare view:\n%s", view)
	}
	p, _ = p.Update(key("m"))
	if p.compare != nil || p.pending != pendingMerge || p.pendingName != "feature" {
		t.Fatalf("m from compare: compare=%v pending=%v %q", p.compare != nil, p.pending, p.pendingName)
	}
}

func TestLogMenuCheckout(t *testing.T) {
	dir := divergedRepo(t)
	m := New()
	m.SetSize(120, 30)
	m.Start(dir)
	m.tab = TabLog
	for i := 0; i < m.log.visibleCount(); i++ {
		if c, _ := m.log.commitAt(i); c.Subject == "feature edit" {
			m.log.cursor = i
		}
	}
	m, _ = m.Update(key("enter"))
	if m.log.menu == nil {
		t.Fatal("menu not open")
	}
	labels := []string{}
	for _, it := range m.log.menu.items {
		labels = append(labels, it.label)
	}
	if labels[0] != "Checkout feature" || !strings.Contains(strings.Join(labels, "|"), "Merge feature into main") {
		t.Fatalf("menu = %q", labels)
	}
	m, _ = m.Update(key("enter")) // run "Checkout feature"
	if current(t, dir) != "feature" || m.branch != "feature" {
		t.Fatalf("on %q (panel says %q): %q", current(t, dir), m.branch, m.log.status)
	}
}
