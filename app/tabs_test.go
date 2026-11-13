package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JohnnyReverb9/tuiple/components/filelist"
	"github.com/JohnnyReverb9/tuiple/components/picker"
	"github.com/JohnnyReverb9/tuiple/config"
	"github.com/JohnnyReverb9/tuiple/filesystem"
	"github.com/JohnnyReverb9/tuiple/keys"
)

// project builds a small tree and returns its root:
//
//	root/alpha.txt  root/beta.txt  root/sub/inner.txt  root/other/
func project(t *testing.T) string {
	t.Helper()
	// Resolve symlinks: on macOS the temp directory lives behind one,
	// and paths are compared as strings throughout.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha.txt", "beta.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"sub", "other"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "inner.txt"), []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

// newTestModel returns a sized model on dir, with settings and state
// files pointed at a throwaway directory.
func newTestModel(t *testing.T, dir string) Model {
	t.Helper()
	tempConfig(t, func(c *config.Config) { c.Git.Enabled = false })
	m := New(dir)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return next.(Model)
}

// settle runs a command and feeds what it produces back into the model,
// as the Bubble Tea runtime would, until nothing is left. Timers are
// given a moment and then dropped — no test here waits on one.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 500 {
			t.Fatal("commands never settled")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(200 * time.Millisecond):
			continue
		}
		switch msg := msg.(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			next, more := m.Update(msg)
			m = next.(Model)
			queue = append(queue, more)
		}
	}
	return m
}

func press(t *testing.T, m Model, key string) Model {
	t.Helper()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	next, cmd := m.Update(msg)
	return settle(t, next.(Model), cmd)
}

// ── Tabs ───────────────────────────────────────────────────────────────

func TestTabsOpenSwitchAndClose(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)

	if m.tabBarVisible() {
		t.Error("a single tab should not draw a tab strip")
	}

	m = press(t, m, "t")
	if len(m.tabs) != 2 || m.tabIdx != 1 {
		t.Fatalf("after t: %d tabs, active %d; want 2 tabs, active 1", len(m.tabs), m.tabIdx)
	}
	if !m.tabBarVisible() {
		t.Error("two tabs should draw a tab strip")
	}
	if m.currentPath != root {
		t.Errorf("the new tab opened at %q, want the directory it came from", m.currentPath)
	}

	// Take the second tab somewhere else; the first must not follow.
	sub := filepath.Join(root, "sub")
	next, cmd := m.goTo(sub, "")
	m = settle(t, next, cmd)

	m = press(t, m, "{")
	if m.tabIdx != 0 || m.currentPath != root {
		t.Fatalf("previous tab: active %d at %q, want 0 at %q", m.tabIdx, m.currentPath, root)
	}
	m = press(t, m, "2")
	if m.tabIdx != 1 || m.currentPath != sub {
		t.Fatalf("jump to tab 2: active %d at %q, want 1 at %q", m.tabIdx, m.currentPath, sub)
	}
	if got := m.filelist.Entries(); len(got) != 1 || got[0].Name != "inner.txt" {
		t.Errorf("tab 2 lists %v, want [inner.txt]", got)
	}

	m = press(t, m, "w")
	if len(m.tabs) != 1 || m.currentPath != root {
		t.Fatalf("after closing: %d tabs at %q, want 1 at %q", len(m.tabs), m.currentPath, root)
	}

	// The last tab stays open, and the closed one can come back.
	m = press(t, m, "w")
	if len(m.tabs) != 1 {
		t.Fatal("the last tab was closed")
	}
	next, cmd = m.reopenTab()
	m = settle(t, next, cmd)
	if len(m.tabs) != 2 || m.currentPath != sub {
		t.Errorf("reopen: %d tabs at %q, want 2 at %q", len(m.tabs), m.currentPath, sub)
	}
}

// Each tab is a list of its own: cursor and marks survive a trip to
// another tab and back.
func TestTabsKeepTheirOwnState(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)

	m = press(t, m, "j")
	m = press(t, m, " ") // mark "sub" and step down
	wantCursor, wantMarked := m.filelist.CursorIdx(), m.filelist.MarkedCount()
	if wantMarked != 1 {
		t.Fatalf("marked %d entries, want 1", wantMarked)
	}

	m = press(t, m, "t")
	m = press(t, m, "g")
	if m.filelist.MarkedCount() != 0 {
		t.Error("the new tab inherited the marks of the one it came from")
	}

	m = press(t, m, "1")
	if got := m.filelist.CursorIdx(); got != wantCursor {
		t.Errorf("tab 1 cursor is at %d after the round trip, want %d", got, wantCursor)
	}
	if got := m.filelist.MarkedCount(); got != wantMarked {
		t.Errorf("tab 1 has %d marks after the round trip, want %d", got, wantMarked)
	}
}

// Closing a tab that is not in front must leave the front one alone.
func TestClosingABackgroundTab(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)
	for _, dir := range []string{"sub", "other"} {
		next, cmd := m.openTab(filepath.Join(root, dir), "")
		m = settle(t, next, cmd)
	}
	// Tabs: root, sub, other — "other" in front.
	next, cmd := m.closeTab(0)
	m = settle(t, next, cmd)

	if len(m.tabs) != 2 {
		t.Fatalf("%d tabs left, want 2", len(m.tabs))
	}
	if want := filepath.Join(root, "other"); m.currentPath != want || m.filelist.CurrentPath() != want {
		t.Errorf("front tab is now %q, want it still at %q", m.currentPath, want)
	}
	if got, want := m.tabPaths(), []string{filepath.Join(root, "sub"), filepath.Join(root, "other")}; !equal(got, want) {
		t.Errorf("tabs are %v, want %v", got, want)
	}
}

// A directory read started by a tab that has since gone to the
// background belongs to that tab.
func TestBackgroundLoadReachesItsOwnTab(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)

	// Start a navigation in tab 1 but do not let the read land yet.
	var load tea.Cmd
	m.filelist, load = m.filelist.NavigateTo(filepath.Join(root, "sub"))
	m.currentPath = m.filelist.CurrentPath()

	next, cmd := m.openTab(filepath.Join(root, "other"), "")
	m = settle(t, next, cmd)
	if n := len(m.filelist.Entries()); n != 0 {
		t.Fatalf("the front tab lists %d entries, want an empty directory", n)
	}

	m = settle(t, m, load)
	if n := len(m.filelist.Entries()); n != 0 {
		t.Errorf("a background read landed in the front tab: %d entries", n)
	}
	m = press(t, m, "1")
	if got := m.filelist.Entries(); len(got) != 1 || got[0].Name != "inner.txt" {
		t.Errorf("tab 1 lists %v, want [inner.txt]", got)
	}
}

func TestTabTitles(t *testing.T) {
	home := filesystem.HomeDir()
	got := tabTitles([]string{
		home,
		"/",
		"/work/tuiple/app",
		"/work/other/app",
		"/work/tuiple",
		"/work/tuiple",
		"/work/" + strings.Repeat("long", 12),
	})
	want := []string{"~", "/", "tuiple/app", "other/app", "tuiple", "tuiple"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("title %d = %q, want %q", i, got[i], w)
		}
	}
	if n := len([]rune(got[6])); n > tabTitleMax {
		t.Errorf("a long title is %d runes, want at most %d", n, tabTitleMax)
	}
}

// However many tabs there are, the strip is exactly as wide as the
// window, the active tab is on it, and a click lands on the tab drawn.
func TestTabStripFitsAndKeepsTheActiveTabVisible(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)
	for i := 0; i < 14; i++ {
		next, cmd := m.openTab(filepath.Join(root, "sub"), "")
		m = settle(t, next, cmd)
	}

	for _, width := range []int{40, 61, 120} {
		for _, active := range []int{0, 7, 14} {
			m.stashTab()
			m.width, m.tabIdx = width, active
			m.filelist = m.tabs[active]

			if w := lipgloss.Width(m.renderTabBar()); w != width {
				t.Errorf("width %d, tab %d: strip is %d cells", width, active, w)
			}
			cells, _, _ := m.tabLayout()
			found := false
			for _, c := range cells {
				if c.x1 > width {
					t.Errorf("width %d: tab %d runs off the strip (to column %d)", width, c.idx+1, c.x1)
				}
				if idx, ok := m.tabAt(c.x0); !ok || idx != c.idx {
					t.Errorf("width %d: click on tab %d hit %d (ok=%v)", width, c.idx+1, idx+1, ok)
				}
				found = found || c.idx == active
			}
			if !found {
				t.Errorf("width %d: active tab %d is not on the strip", width, active+1)
			}
		}
	}
}

// ── Session ────────────────────────────────────────────────────────────

func TestSessionRoundTrip(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)
	next, cmd := m.openTab(filepath.Join(root, "sub"), "")
	m = settle(t, next, cmd)
	next, cmd = m.openTab(filepath.Join(root, "other"), "")
	m = settle(t, next, cmd)
	m = press(t, m, "{") // sub in front

	m.saveSession()
	dirs, active, ok := LoadSession()
	if !ok {
		t.Fatal("nothing to restore after saving")
	}
	want := []string{root, filepath.Join(root, "sub"), filepath.Join(root, "other")}
	if !equal(dirs, want) || active != 1 {
		t.Fatalf("restored %v active %d, want %v active 1", dirs, active, want)
	}

	// A directory deleted since drops out, and the active tab is still
	// the same directory, not the same number.
	if err := os.Remove(filepath.Join(root, "alpha.txt")); err != nil {
		t.Fatal(err)
	}
	m.tabs[0] = filelist.New(filepath.Join(root, "gone"))
	m.saveSession()
	dirs, active, ok = LoadSession()
	if !ok || !equal(dirs, want[1:]) || active != 0 {
		t.Errorf("restored %v active %d (ok=%v), want %v active 0", dirs, active, ok, want[1:])
	}

	restored := NewWithTabs(dirs, active)
	if len(restored.tabs) != 2 || restored.currentPath != want[1] {
		t.Errorf("rebuilt %d tabs at %q, want 2 at %q", len(restored.tabs), restored.currentPath, want[1])
	}
}

// ── Recent ─────────────────────────────────────────────────────────────

func TestRecentIsNewestFirstWithoutDuplicates(t *testing.T) {
	root := project(t)
	tempConfig(t, func(*config.Config) {})

	a, b := filepath.Join(root, "alpha.txt"), filepath.Join(root, "beta.txt")
	TouchRecent(a, false)
	TouchRecent(b, false)
	TouchRecent(root, true)
	TouchRecent(a, false) // again: moves to the top, does not repeat

	var got []string
	for _, e := range RecentEntries() {
		got = append(got, e.Path)
	}
	if want := []string{a, root, b}; !equal(got, want) {
		t.Fatalf("recent = %v, want %v", got, want)
	}

	// What no longer exists is left out, without being forgotten on disk.
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	if n := len(RecentEntries()); n != 2 {
		t.Errorf("%d entries after deleting one of the files, want 2", n)
	}
}

// ── Actions ────────────────────────────────────────────────────────────

// Everything bound to a key is reachable from the palette, apart from
// the handful of single-step movements that make no sense as a row.
func TestPaletteOffersEveryAction(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)
	m = press(t, m, "j")

	cmd := m.openPalette()
	_ = cmd
	if !m.picker.IsActive() {
		t.Fatal("the palette did not open")
	}

	// Picking a row runs the action, whichever scope it lives in.
	next, c := m.picked(picker.ChosenMsg{Kind: pickActions, ID: string(keys.SelectAll)})
	m = settle(t, next.(Model), c)
	if got := m.filelist.MarkedCount(); got != len(m.filelist.Entries()) {
		t.Errorf("select all from the palette marked %d of %d", got, len(m.filelist.Entries()))
	}
	next, c = m.picked(picker.ChosenMsg{Kind: pickActions, ID: string(keys.TabNew)})
	m = settle(t, next.(Model), c)
	if len(m.tabs) != 2 {
		t.Errorf("new tab from the palette left %d tabs, want 2", len(m.tabs))
	}

	for _, b := range keys.Defaults {
		if _, ok := keys.Find(b.Action); !ok {
			t.Errorf("%q cannot be looked up, so the palette could not run it", b.Action)
		}
	}
}

func TestDuplicateCopiesNextToTheOriginal(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)

	entry := filesystem.FileEntry{Name: "alpha.txt", Path: filepath.Join(root, "alpha.txt")}
	next, cmd := m.duplicate([]filesystem.FileEntry{entry})
	m = settle(t, next.(Model), cmd)

	copyPath := filepath.Join(root, "alpha_1.txt")
	if data, err := os.ReadFile(copyPath); err != nil || string(data) != "x" {
		t.Fatalf("the copy is missing or wrong: %q, %v", data, err)
	}
	if e := m.filelist.SelectedEntry(); e == nil || e.Name != "alpha_1.txt" {
		t.Errorf("cursor is on %v, want the new copy", e)
	}
	// It is undoable like any other copy.
	if _, err := Undo(); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Error("undo left the duplicate behind")
	}
}

// ── Cmd shortcuts ──────────────────────────────────────────────────────

func TestParseChord(t *testing.T) {
	for seq, want := range map[string]string{
		"\x1b[101;9u":  "cmd+e",
		"\x1b[97;10u":  "cmd+shift+a",
		"\x1b[111;11u": "cmd+alt+o",
		"\x1b[91;9u":   "cmd+[",
		"\x1b[127;9u":  "cmd+backspace",
		"\x1b[13;3u":   "alt+enter",
		"\x1b[1;11D":   "cmd+alt+left",
		"\x1b[1;10A":   "cmd+shift+up",
	} {
		got, ok := parseChord([]byte(seq))
		if !ok || got != want {
			t.Errorf("%q parsed as %q (ok=%v), want %q", seq, got, ok, want)
		}
	}
	for _, seq := range []string{"", "\x1b[A", "\x1b[115u", "\x1b[x;9u", "\x1b[115;1u", "\x1b[24;9~"} {
		if got, ok := parseChord([]byte(seq)); ok {
			t.Errorf("%q should not parse, got %q", seq, got)
		}
	}
}

// Every Cmd key the keymap advertises is one the iTerm profile sends —
// otherwise the help screen promises a shortcut that cannot arrive.
func TestCmdBindingsAreSentByTheITermProfile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "nvim-ide", "iterm", "keys.tsv"))
	if err != nil {
		t.Skipf("no iTerm key table: %v", err)
	}
	sent := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, "\t")
		if strings.HasPrefix(line, "#") || len(fields) < 2 {
			continue
		}
		if chord, ok := parseChord([]byte("\x1b" + fields[1])); ok {
			sent[chord] = true
		}
	}
	for _, b := range keys.Defaults {
		for _, k := range b.Keys {
			if strings.HasPrefix(k, "cmd+") && !sent[k] {
				t.Errorf("%q is bound to %s, which nvim-ide/iterm/keys.tsv does not send", b.Action, k)
			}
		}
	}
}

// ── Frame ──────────────────────────────────────────────────────────────

func TestBreadcrumbSegmentsLeadToTheirDirectories(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)
	m.currentPath = "/usr/local/bin"

	var targets []string
	for _, c := range m.crumbs() {
		targets = append(targets, c.target)
		if c.target != "" && m.crumbAt(c.x0) != c.target {
			t.Errorf("a click on %q leads to %q, want %q", c.label, m.crumbAt(c.x0), c.target)
		}
	}
	want := []string{crumbBack, crumbForward, "/", "/usr", "/usr/local", "/usr/local/bin"}
	if !equal(targets, want) {
		t.Errorf("breadcrumb targets = %v, want %v", targets, want)
	}

	// Too long for the window: it still fits, and still ends where we are.
	m.width = 30
	m.currentPath = "/one/two/three/four/five/six/seven"
	if w := lipgloss.Width(m.renderBreadcrumb()); w != 30 {
		t.Errorf("a long breadcrumb is %d cells in a 30-cell window", w)
	}
	crumbs := m.crumbs()
	if last := crumbs[len(crumbs)-1]; last.label != "seven" {
		t.Errorf("the breadcrumb ends in %q, want the current directory", last.label)
	}
}

func TestChildOf(t *testing.T) {
	for _, tc := range []struct{ ancestor, descendant, want string }{
		{"/a", "/a/b/c", "b"},
		{"/", "/usr/local", "usr"},
		{"/a/b", "/a/b", ""},
		{"/a/b", "/a/c", ""},
	} {
		if got := childOf(tc.ancestor, tc.descendant); got != tc.want {
			t.Errorf("childOf(%q, %q) = %q, want %q", tc.ancestor, tc.descendant, got, tc.want)
		}
	}
}

// Every frame, with or without a popup, is exactly the size of the
// window: a line too long wraps and breaks the whole screen.
func TestFramesFillTheWindowExactly(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)

	check := func(name string, m Model) {
		t.Helper()
		lines := strings.Split(m.View(), "\n")
		if len(lines) != m.height {
			t.Errorf("%s: %d rows, want %d", name, len(lines), m.height)
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w != m.width {
				t.Errorf("%s: row %d is %d cells wide, want %d", name, i, w, m.width)
				break
			}
		}
	}

	check("one tab", m)
	m = press(t, m, "t")
	check("two tabs", m)
	m.openPalette()
	check("palette", m)
	m.picker.Close()
	m.statusMsg = strings.Repeat("a long status message ", 20)
	check("long status", m)
}

func TestStatusMessageExpires(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)

	m.statusMsg = "Copied 2 items"
	m.statusAt = time.Now()
	next, _ := m.Update(dirPollMsg{})
	m = next.(Model)
	if m.statusMsg == "" {
		t.Fatal("a fresh message was cleared at once")
	}
	m.statusAt = time.Now().Add(-2 * statusLifetime)
	next, _ = m.Update(dirPollMsg{})
	if got := next.(Model).statusMsg; got != "" {
		t.Errorf("an old message is still up: %q", got)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A run started on an explicit path must not replace the saved session.
func TestOneOffRunLeavesTheSessionAlone(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)
	next, cmd := m.openTab(filepath.Join(root, "sub"), "")
	m = settle(t, next, cmd)
	m.saveSession()

	oneOff := New(filepath.Join(root, "other")).WithoutSession()
	oneOff.saveSession()

	dirs, _, ok := LoadSession()
	if want := []string{root, filepath.Join(root, "sub")}; !ok || !equal(dirs, want) {
		t.Errorf("session is now %v (ok=%v), want it still %v", dirs, ok, want)
	}
}

// An action run from the palette while another panel has the keyboard
// must leave the list able to read keys — the filter above all, which
// would otherwise swallow every keystroke and act on none.
func TestListActionFromThePaletteFocusesTheList(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)
	m.active = panelSidebar
	m.updateFocus()

	next, cmd := m.picked(picker.ChosenMsg{Kind: pickActions, ID: string(keys.Filter)})
	m = settle(t, next.(Model), cmd)
	if m.active != panelFileList {
		t.Fatal("the list did not get the keyboard")
	}
	for _, r := range "alp" {
		m = press(t, m, string(r))
	}
	if got := m.filelist.Entries(); len(got) != 1 || got[0].Name != "alpha.txt" {
		t.Errorf("typing into the filter left %v, want [alpha.txt]", got)
	}
}

// A window far too small still gets rows no wider than itself.
func TestFrameRowsNeverExceedANarrowWindow(t *testing.T) {
	root := project(t)
	m := newTestModel(t, root)
	m = press(t, m, "t")
	m.width = 18
	m.statusMsg = "a status message"
	for name, row := range map[string]string{
		"tab strip":  m.renderTabBar(),
		"breadcrumb": m.renderBreadcrumb(),
		"status bar": m.renderStatusBar(),
	} {
		if w := lipgloss.Width(row); w > m.width {
			t.Errorf("%s is %d cells in an %d-cell window", name, w, m.width)
		}
	}
}
