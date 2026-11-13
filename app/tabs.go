package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JohnnyReverb9/tuiple/components/filelist"
	"github.com/JohnnyReverb9/tuiple/filesystem"
	"github.com/JohnnyReverb9/tuiple/theme"
)

// Tabs.
//
// A tab is a file list of its own: its directory, cursor, marks, filter,
// sort order and back / forward history. The sidebar and the preview are
// shared and follow whichever tab is in front — the same split Finder
// makes.
//
// The list of the active tab lives in m.filelist, where every handler
// written before tabs existed already looks for it. m.tabs holds the
// others, and the active slot of m.tabs goes stale the moment the user
// touches anything. So the rule for this file is: stashTab before
// reading m.tabs as a whole, showActiveTab after changing which one is
// in front. Nothing outside this file indexes m.tabs directly.

// closedTabsLimit is how many closed tabs can be brought back.
const closedTabsLimit = 20

// stashTab writes the live list back into its slot.
func (m *Model) stashTab() {
	if m.tabIdx >= 0 && m.tabIdx < len(m.tabs) {
		m.tabs[m.tabIdx] = m.filelist
	}
}

// showActiveTab brings m.tabs[m.tabIdx] to the front and catches up
// everything that follows the active tab: panel sizes, git state, the
// preview. The list itself is re-read, because whatever happened to that
// directory while the tab was in the background went unnoticed.
func (m *Model) showActiveTab() tea.Cmd {
	m.filelist = m.tabs[m.tabIdx]
	m.currentPath = m.filelist.CurrentPath()
	m.updateSizes()
	m.updateLastDirMod(m.currentPath)
	m.refreshGit()
	m.refreshDisk()
	m.sessionDirty = true

	cmds := []tea.Cmd{m.listCmd(filelist.RefreshListMsg{})}
	if entry := m.filelist.SelectedEntry(); entry != nil {
		m.stampPreview(entry)
		cmds = append(cmds, m.preview.LoadFile(*entry))
	} else {
		m.preview.Clear()
		m.stampPreview(nil)
	}
	return tea.Batch(cmds...)
}

// switchTab brings another tab to the front.
func (m Model) switchTab(idx int) (Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.tabs) || idx == m.tabIdx {
		return m, nil
	}
	m.stashTab()
	m.tabIdx = idx
	cmd := m.showActiveTab()
	return m, cmd
}

// cycleTab steps to the neighbouring tab, wrapping at the ends.
func (m Model) cycleTab(dir int) (Model, tea.Cmd) {
	n := len(m.tabs)
	if n < 2 {
		m.statusMsg = "Only one tab — t opens another"
		return m, nil
	}
	return m.switchTab((m.tabIdx + dir + n) % n)
}

// openTab opens a directory in a new tab right after the current one and
// switches to it. selectName, when given, is the entry to start on — a
// tab duplicated from the current one keeps its place.
func (m Model) openTab(path, selectName string) (Model, tea.Cmd) {
	m.stashTab()

	list := m.filelist.Spawn(path)
	if selectName != "" {
		list = list.SelectByName(selectName)
	}

	at := m.tabIdx + 1
	tabs := make([]filelist.Model, 0, len(m.tabs)+1)
	tabs = append(tabs, m.tabs[:at]...)
	tabs = append(tabs, list)
	tabs = append(tabs, m.tabs[at:]...)
	m.tabs = tabs
	m.tabIdx = at
	// The first tab was the whole window; with a second one the strip
	// has to appear, and showActiveTab re-measures the panels for it.
	m.active = panelFileList
	cmd := m.showActiveTab()
	return m, cmd
}

// closeTab closes a tab and remembers where it was, so ctrl+t can bring
// it back. The last tab stays: closing it would leave nothing to show,
// and quitting is what q is for.
func (m Model) closeTab(idx int) (Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.tabs) {
		return m, nil
	}
	if len(m.tabs) == 1 {
		m.statusMsg = "This is the last tab — q quits"
		return m, nil
	}
	m.stashTab()

	m.closedTabs = append(m.closedTabs, m.tabs[idx].CurrentPath())
	if len(m.closedTabs) > closedTabsLimit {
		m.closedTabs = m.closedTabs[len(m.closedTabs)-closedTabsLimit:]
	}

	tabs := make([]filelist.Model, 0, len(m.tabs)-1)
	tabs = append(tabs, m.tabs[:idx]...)
	tabs = append(tabs, m.tabs[idx+1:]...)
	m.tabs = tabs

	// Closing a background tab must not change which one is in front;
	// closing the front one moves to its right-hand neighbour, as a
	// browser does, or to the new last tab.
	wasActive := idx == m.tabIdx
	if idx < m.tabIdx {
		m.tabIdx--
	}
	if m.tabIdx >= len(m.tabs) {
		m.tabIdx = len(m.tabs) - 1
	}
	if !wasActive {
		m.filelist = m.tabs[m.tabIdx]
		m.updateSizes()
		m.sessionDirty = true
		return m, nil
	}
	cmd := m.showActiveTab()
	return m, cmd
}

// reopenTab brings back the most recently closed tab.
func (m Model) reopenTab() (Model, tea.Cmd) {
	for len(m.closedTabs) > 0 {
		path := m.closedTabs[len(m.closedTabs)-1]
		m.closedTabs = m.closedTabs[:len(m.closedTabs)-1]
		if isDir(path) {
			return m.openTab(path, "")
		}
		// The directory is gone; try the one closed before it.
	}
	m.statusMsg = "No closed tab to reopen"
	return m, nil
}

// tabPaths lists the directory of every tab, in order.
func (m Model) tabPaths() []string {
	paths := make([]string, len(m.tabs))
	for i := range m.tabs {
		paths[i] = m.tabs[i].CurrentPath()
	}
	if m.tabIdx >= 0 && m.tabIdx < len(paths) {
		paths[m.tabIdx] = m.filelist.CurrentPath()
	}
	return paths
}

// ── Tab strip ──────────────────────────────────────────────────────────

// tabBarVisible reports whether the strip is drawn. One tab needs no
// strip: the breadcrumb already says where it is, and the row is better
// spent on files.
func (m Model) tabBarVisible() bool { return len(m.tabs) > 1 }

// tabTitleMax keeps one long directory name from crowding out the rest.
const tabTitleMax = 22

// tabTitles names the tabs after their directories. Two tabs that would
// read the same get their parent in front, so "app" and "app" become
// "tuiple/app" and "other/app".
func tabTitles(paths []string) []string {
	base := func(p string) string {
		switch p {
		case filesystem.HomeDir():
			return "~"
		case "/":
			return "/"
		}
		return filepath.Base(p)
	}

	titles := make([]string, len(paths))
	seen := make(map[string]int, len(paths))
	for i, p := range paths {
		titles[i] = base(p)
		seen[titles[i]]++
	}
	for i, p := range paths {
		// Tabs on the very same directory are told apart by their
		// number; a parent would not help them.
		if seen[titles[i]] > 1 && !samePathElsewhere(paths, i) && p != "/" {
			titles[i] = base(filepath.Dir(p)) + "/" + titles[i]
		}
	}
	for i, t := range titles {
		if r := []rune(t); len(r) > tabTitleMax {
			titles[i] = "…" + string(r[len(r)-tabTitleMax+1:])
		}
	}
	return titles
}

func samePathElsewhere(paths []string, i int) bool {
	for j, p := range paths {
		if j != i && p == paths[i] {
			return true
		}
	}
	return false
}

// tabCell is one tab as laid out on the strip: which tab, and the
// columns it occupies. Rendering and mouse hit-testing both read this,
// so a click can never land on a different tab than the one drawn there.
type tabCell struct {
	idx    int
	x0, x1 int // [x0, x1)
	label  string
}

// tabLayout decides which tabs fit and where. When they do not all fit,
// the window is grown outwards from the active tab, so the one in front
// is always on screen with as many neighbours as there is room for.
func (m Model) tabLayout() (cells []tabCell, moreLeft, moreRight bool) {
	titles := tabTitles(m.tabPaths())
	labels := make([]string, len(titles))
	widths := make([]int, len(titles))
	for i, t := range titles {
		labels[i] = fmt.Sprintf(" %d %s ", i+1, t)
		widths[i] = lipgloss.Width(labels[i])
	}

	const gap = 1
	// One column of margin on the left, two on either side kept free for
	// the "more tabs this way" marks.
	room := m.width - 5
	lo, hi := m.tabIdx, m.tabIdx
	used := widths[m.tabIdx]
	for {
		grew := false
		if hi+1 < len(labels) && used+gap+widths[hi+1] <= room {
			hi++
			used += gap + widths[hi]
			grew = true
		}
		if lo > 0 && used+gap+widths[lo-1] <= room {
			lo--
			used += gap + widths[lo]
			grew = true
		}
		if !grew {
			break
		}
	}

	x := 1
	if lo > 0 {
		x = 3
	}
	for i := lo; i <= hi; i++ {
		cells = append(cells, tabCell{idx: i, x0: x, x1: x + widths[i], label: labels[i]})
		x += widths[i] + gap
	}
	return cells, lo > 0, hi < len(labels)-1
}

// tabAt returns the tab drawn at column x of the strip.
func (m Model) tabAt(x int) (int, bool) {
	cells, _, _ := m.tabLayout()
	for _, c := range cells {
		if x >= c.x0 && x < c.x1 {
			return c.idx, true
		}
	}
	return 0, false
}

// renderTabBar draws the strip: the active tab on the cursor colour, the
// rest dim. Numbers come first because they are how you reach a tab —
// press the number.
func (m Model) renderTabBar() string {
	cells, moreLeft, moreRight := m.tabLayout()

	var b strings.Builder
	col := 0
	advance := func(to int) {
		if to > col {
			b.WriteString(strings.Repeat(" ", to-col))
			col = to
		}
	}
	if moreLeft {
		advance(1)
		b.WriteString(theme.Muted.Render("‹"))
		col++
	}
	for _, c := range cells {
		advance(c.x0)
		// " 3 name " — the number and the name are styled apart.
		num, name, _ := strings.Cut(strings.TrimPrefix(c.label, " "), " ")
		if c.idx == m.tabIdx {
			bg := lipgloss.NewStyle().Background(theme.BgSelected)
			b.WriteString(bg.Render(" "))
			b.WriteString(bg.Foreground(theme.AccentBlue).Bold(true).Render(num))
			b.WriteString(bg.Render(" "))
			b.WriteString(bg.Foreground(theme.FgColor).Bold(true).Render(name))
		} else {
			b.WriteString(" ")
			b.WriteString(theme.Muted.Render(num))
			b.WriteString(" ")
			b.WriteString(theme.Dim.Render(name))
		}
		col = c.x1
	}
	if moreRight {
		advance(col + 1)
		b.WriteString(theme.Muted.Render("›"))
		col++
	}
	advance(m.width)
	return clip(b.String(), m.width)
}

func isDir(path string) bool {
	return filesystem.IsDir(path)
}
