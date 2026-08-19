package filelist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/filesystem"
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
