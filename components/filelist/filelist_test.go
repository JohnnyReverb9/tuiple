package filelist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JohnnyReverb9/tuiple/filesystem"
	"github.com/JohnnyReverb9/tuiple/keys"
)

// fixture builds a directory with two files and a subdirectory holding
// one more, and returns the root.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"alpha.txt", "beta.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "inner.txt"), []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

// deliver runs a command and feeds whatever it produced back into the
// model, the way the Bubble Tea runtime would.
func deliver(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = deliver(t, m, c)
		}
		return m
	}
	m, _ = m.Update(msg)
	return m
}

func TestNewLoadsSynchronously(t *testing.T) {
	m := New(fixture(t))
	if len(m.Entries()) != 3 {
		t.Fatalf("got %d entries at startup, want 3", len(m.Entries()))
	}
	// Directories first, then files by name.
	if m.Entries()[0].Name != "sub" {
		t.Errorf("first entry is %q, want the directory", m.Entries()[0].Name)
	}
}

func TestNavigationLoadsAsynchronously(t *testing.T) {
	root := fixture(t)
	m := New(root)

	m, cmd := m.NavigateTo(filepath.Join(root, "sub"))
	// The command has not run yet: the panel is still showing the old
	// directory rather than a blank screen, and it knows it is loading.
	if !m.loading {
		t.Error("model should be marked as loading right after navigating")
	}

	m = deliver(t, m, cmd)
	if m.loading {
		t.Error("still loading after the entries were delivered")
	}
	if len(m.Entries()) != 1 || m.Entries()[0].Name != "inner.txt" {
		t.Fatalf("entries = %v, want [inner.txt]", m.Entries())
	}
}

// Results for a directory the user has already left must be discarded,
// or a slow read would overwrite the listing of wherever they are now.
func TestStaleLoadIsIgnored(t *testing.T) {
	root := fixture(t)
	m := New(root)

	m, slowCmd := m.NavigateTo(filepath.Join(root, "sub"))
	stale := slowCmd().(tea.BatchMsg)

	// Go back before the first read lands.
	m, backCmd := m.goUp()
	m = deliver(t, m, backCmd)

	before := len(m.Entries())
	for _, c := range stale {
		m, _ = m.Update(c())
	}
	if len(m.Entries()) != before {
		t.Errorf("a stale read replaced the listing: %d entries, want %d",
			len(m.Entries()), before)
	}
}

// Going up puts the cursor back on the directory you came out of.
func TestGoUpSelectsTheChild(t *testing.T) {
	root := fixture(t)
	m := New(root)
	m.SetSize(80, 20)

	m, cmd := m.NavigateTo(filepath.Join(root, "sub"))
	m = deliver(t, m, cmd)

	m, cmd = m.goUp()
	m = deliver(t, m, cmd)

	entry := m.SelectedEntry()
	if entry == nil || entry.Name != "sub" {
		t.Fatalf("cursor is on %v, want sub", entry)
	}
}

// Typing in the filter narrows the list from memory — no directory read
// per keystroke.
func TestFilterIsInMemory(t *testing.T) {
	m := New(fixture(t))
	m.SetSize(80, 20)
	m.filtering = true

	tokenBefore := m.loadToken
	for _, r := range "alp" {
		var cmd tea.Cmd
		m, cmd = m.updateFilter(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if cmd != nil {
			t.Fatal("filtering should not trigger a directory read")
		}
	}
	if m.loadToken != tokenBefore {
		t.Error("filtering started a load")
	}
	if len(m.Entries()) != 1 || m.Entries()[0].Name != "alpha.txt" {
		t.Fatalf("entries = %v, want [alpha.txt]", m.Entries())
	}
}

// A refresh keeps the cursor on the same file even when the listing
// around it changed.
func TestRefreshKeepsTheCursorOnItsFile(t *testing.T) {
	root := fixture(t)
	m := New(root)
	m.SetSize(80, 20)
	m.cursor = 2 // beta.txt (sub, alpha.txt, beta.txt)

	if err := os.WriteFile(filepath.Join(root, "aaa.txt"), []byte("z"), 0644); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.Update(RefreshListMsg{})
	m = deliver(t, m, cmd)

	entry := m.SelectedEntry()
	if entry == nil || entry.Name != "beta.txt" {
		t.Fatalf("cursor is on %v, want beta.txt", entry)
	}
}

// Every rendered row has to be exactly as wide as the panel. A cell that
// overflows does not get truncated by lipgloss — it wraps, and the
// overflow lands in the next column, which is how "918.2 KiB" once
// pushed the Modified column off the row.
func TestRowsNeverOverflowTheirColumns(t *testing.T) {
	root := t.TempDir()
	big := filepath.Join(root, "big.bin")
	if err := os.WriteFile(big, make([]byte, 940*1024), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, timeFormat := range []string{"short", "iso", "relative"} {
		filesystem.SetFormatOptions(timeFormat, true)

		m := New(root)
		m.SetSize(80, 10)
		// Directory sizes only appear once measured, which is what put
		// a nine-cell string into an eight-cell column in the first place.
		m.dirSizes[root] = 940 * 1024

		for i := range m.entries {
			for _, focused := range []bool{true, false} {
				m.focused = focused
				m.cursor = i
				row := m.renderEntry(i, true)
				if strings.Contains(row, "\n") {
					t.Fatalf("%s: row %d wrapped onto a second line:\n%s",
						timeFormat, i, row)
				}
				if w := lipgloss.Width(row); w != 80 {
					t.Errorf("%s: row %d is %d cells wide, want 80", timeFormat, i, w)
				}
			}
		}

		if w := lipgloss.Width(m.renderHeader()); w > 80 {
			t.Errorf("%s: header is %d cells wide, want at most 80", timeFormat, w)
		}
	}
	filesystem.SetFormatOptions("short", true)
}

// Back and Forward walk the history without growing it, and each stop
// remembers which entry the cursor was on.
func TestBackAndForward(t *testing.T) {
	root := fixture(t)
	sub := filepath.Join(root, "sub")
	m := New(root)
	m.SetSize(80, 20)

	if m.CanGoBack() || m.CanGoForward() {
		t.Fatal("a fresh list should have no history")
	}

	m, cmd := m.NavigateTo(sub)
	m = deliver(t, m, cmd)
	if !m.CanGoBack() || m.CanGoForward() {
		t.Fatalf("after navigating: back=%v forward=%v, want true/false", m.CanGoBack(), m.CanGoForward())
	}

	m, cmd = m.Back()
	m = deliver(t, m, cmd)
	if m.CurrentPath() != root {
		t.Fatalf("Back went to %q, want %q", m.CurrentPath(), root)
	}
	if !m.CanGoForward() {
		t.Error("Back should leave somewhere to go forward to")
	}

	m, cmd = m.Forward()
	m = deliver(t, m, cmd)
	if m.CurrentPath() != sub {
		t.Fatalf("Forward went to %q, want %q", m.CurrentPath(), sub)
	}
	if m.CanGoForward() {
		t.Error("Forward should have used the forward history up")
	}

	// Going somewhere new from the middle of the history drops what was
	// ahead, as a browser does.
	m, cmd = m.Back()
	m = deliver(t, m, cmd)
	m, cmd = m.NavigateTo(sub)
	m = deliver(t, m, cmd)
	if m.CanGoForward() {
		t.Error("navigating should clear the forward history")
	}
}

// Parent means the enclosing directory, whatever the history says: a
// jump from somewhere unrelated must not turn "up" into "back".
func TestGoUpIsTheParentNotTheHistory(t *testing.T) {
	root := fixture(t)
	elsewhere := t.TempDir()
	sub := filepath.Join(root, "sub")

	m := New(elsewhere)
	m.SetSize(80, 20)
	m, cmd := m.NavigateTo(sub) // as a bookmark or a search result would
	m = deliver(t, m, cmd)

	m, cmd = m.goUp()
	m = deliver(t, m, cmd)
	if m.CurrentPath() != root {
		t.Fatalf("up from %q went to %q, want its parent %q", sub, m.CurrentPath(), root)
	}
	if e := m.SelectedEntry(); e == nil || e.Name != "sub" {
		t.Errorf("cursor is on %v, want the directory just left", e)
	}

	// And it is a navigation like any other: Back leads into the child.
	m, cmd = m.Back()
	m = deliver(t, m, cmd)
	if m.CurrentPath() != sub {
		t.Errorf("Back after going up went to %q, want %q", m.CurrentPath(), sub)
	}
}

// Marks are made in a directory and stay there. Carried along, they
// would make the bulk operations act on rows that are not on screen.
func TestMarksDoNotFollowNavigation(t *testing.T) {
	root := fixture(t)
	m := New(root)
	m.SetSize(80, 20)

	m, _ = m.Do(keys.SelectAll)
	if got := m.MarkedCount(); got != 3 {
		t.Fatalf("select all marked %d entries, want 3", got)
	}
	m, _ = m.Do(keys.InvertMarks)
	if got := m.MarkedCount(); got != 0 {
		t.Fatalf("inverting a full selection left %d marked, want 0", got)
	}

	m, _ = m.Do(keys.SelectAll)
	m, cmd := m.NavigateTo(filepath.Join(root, "sub"))
	m = deliver(t, m, cmd)
	if got := m.MarkedCount(); got != 0 {
		t.Errorf("%d marks survived a change of directory", got)
	}
}

// With tabs several lists are alive at once. A read finishing for one of
// them must not be taken for the answer to another's, even when both
// are showing the same directory.
func TestLoadFromAnotherListIsIgnored(t *testing.T) {
	root := fixture(t)
	a := New(root)
	b := a.Spawn(root)
	if a.ID() == b.ID() {
		t.Fatal("two lists share an id")
	}

	_, cmd := a.Update(RefreshListMsg{})
	loaded := cmd().(EntriesLoadedMsg)

	// Make the result recognisable, then offer it to the wrong list.
	loaded.Entries = nil
	loaded.Token = b.loadToken
	b, _ = b.Update(loaded)
	if len(b.Entries()) != 3 {
		t.Errorf("list b took a read addressed to list a: %d entries, want 3", len(b.Entries()))
	}
}

// A click is turned into a row with EntryAt and into a sort order with
// SortAt; both have to agree with what is actually drawn.
func TestMouseHitTesting(t *testing.T) {
	m := New(fixture(t))
	m.SetSize(80, 20)

	if _, ok := m.EntryAt(0); ok {
		t.Error("row 0 is the header, not an entry")
	}
	if idx, ok := m.EntryAt(1); !ok || idx != 0 {
		t.Errorf("row 1 = entry %d (ok=%v), want entry 0", idx, ok)
	}
	if _, ok := m.EntryAt(4); ok {
		t.Error("a row below the last entry should hit nothing")
	}

	header := m.renderHeader()
	for label, want := range map[string]filesystem.SortMode{
		"Name":     filesystem.SortByName,
		"Size":     filesystem.SortBySize,
		"Modified": filesystem.SortByDate,
	} {
		// Columns are counted in cells; the header is plain ASCII up to
		// each label once the styling is stripped.
		x := lipgloss.Width(header[:strings.Index(header, label)]) + 1
		if got := m.SortAt(x); got != want {
			t.Errorf("a click on %q sorts by %v, want %v", label, got, want)
		}
	}
}

// Expanding a folder lists its contents under it, one level in; the
// cursor stays on the folder, and collapsing from inside lands back on it.
func TestExpandAndCollapseInPlace(t *testing.T) {
	root := fixture(t) // sub/ (inner.txt), alpha.txt, beta.txt
	m := New(root)
	m.SetSize(80, 20)

	m, cmd := m.Do(keys.Expand)
	m = deliver(t, m, cmd)
	var got []string
	for _, e := range m.Entries() {
		got = append(got, strings.Repeat(">", e.Depth)+e.Name)
	}
	if want := "sub,>inner.txt,alpha.txt,beta.txt"; strings.Join(got, ",") != want {
		t.Fatalf("expanded listing = %v, want %s", got, want)
	}
	if e := m.SelectedEntry(); e == nil || e.Name != "sub" {
		t.Fatalf("cursor moved to %v on expanding", e)
	}
	if !strings.Contains(m.renderEntry(1, true), "  inner.txt") {
		t.Error("the child row is not indented")
	}

	// Expand again steps into the folder; collapse from the child closes it.
	m, _ = m.Do(keys.Expand)
	if e := m.SelectedEntry(); e == nil || e.Name != "inner.txt" {
		t.Fatalf("cursor is on %v, want the first child", e)
	}
	m, cmd = m.Do(keys.Collapse)
	m = deliver(t, m, cmd)
	if len(m.Entries()) != 3 {
		t.Fatalf("%d rows after collapsing, want 3", len(m.Entries()))
	}
	if e := m.SelectedEntry(); e == nil || e.Name != "sub" {
		t.Errorf("cursor is on %v after collapsing, want the folder", e)
	}

	// A refresh keeps the tree open and every row the panel's width.
	m, cmd = m.Do(keys.Expand)
	m = deliver(t, m, cmd)
	m, cmd = m.Update(RefreshListMsg{})
	m = deliver(t, m, cmd)
	if len(m.Entries()) != 4 {
		t.Errorf("a refresh closed the tree: %d rows", len(m.Entries()))
	}
	for i := range m.Entries() {
		if w := lipgloss.Width(m.renderEntry(i, true)); w != 80 {
			t.Errorf("row %d is %d cells wide, want 80", i, w)
		}
	}
	// Bulk rename covers one directory: the nested file stays out of it.
	if names := m.bulkRenameNames(); len(names) != 3 {
		t.Errorf("bulk rename would cover %v", names)
	}
}
