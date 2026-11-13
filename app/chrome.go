package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/JohnnyReverb9/tuiple/filesystem"
	"github.com/JohnnyReverb9/tuiple/theme"
)

// The window frame: the breadcrumb above the panels and the status bar
// below them. (The tab strip is in tabs.go, next to the tabs.)
//
// The two rows used to say the same thing — the path, twice. Now the top
// row is where you are and the bottom row is everything else: the
// branch, what just happened, what is marked, how much room is left.

// ── Breadcrumb ─────────────────────────────────────────────────────────

// Sentinels crumbAt returns for the two history arrows; a real target is
// always an absolute path, so these cannot collide with one.
const (
	crumbBack    = "<back>"
	crumbForward = "<forward>"
)

// crumb is one clickable piece of the breadcrumb row.
type crumb struct {
	label  string
	target string // directory to open, or one of the sentinels above
	x0, x1 int    // [x0, x1)
	last   bool
	gap    bool // the "…" standing in for the segments that did not fit
}

const crumbSep = " › "

// crumbs lays the row out: the two history arrows, then the path as one
// segment per directory. When the path is too long for the window the
// middle goes first — the root and the last few directories are what
// identify a place.
func (m Model) crumbs() []crumb {
	out := []crumb{
		{label: "‹", target: crumbBack, x0: 1, x1: 2},
		{label: "›", target: crumbForward, x0: 3, x1: 4},
	}

	// Every ancestor, from the root (or ~) down to the current directory.
	var segs []crumb
	home := filesystem.HomeDir()
	rest := m.currentPath
	switch {
	case rest == home || strings.HasPrefix(rest, home+"/"):
		segs = append(segs, crumb{label: "~", target: home})
		rest = strings.TrimPrefix(rest, home)
	default:
		segs = append(segs, crumb{label: "/", target: "/"})
	}
	dir := segs[0].target
	for _, part := range strings.Split(rest, "/") {
		if part == "" {
			continue
		}
		dir = filepath.Join(dir, part)
		segs = append(segs, crumb{label: part, target: dir})
	}
	segs[len(segs)-1].last = true

	const startX = 6
	width := func(list []crumb) int {
		w := 0
		for i, s := range list {
			if i > 0 {
				w += lipgloss.Width(crumbSep)
			}
			w += lipgloss.Width(s.label)
		}
		return w
	}
	room := m.width - startX - 1
	for len(segs) > 2 && width(segs) > room {
		// Fold the second segment into a "…", or drop the one after an
		// existing "…".
		if segs[1].gap {
			segs = append(segs[:2], segs[3:]...)
		} else {
			segs[1] = crumb{label: "…", gap: true}
		}
	}

	x := startX
	for i, s := range segs {
		if i > 0 {
			x += lipgloss.Width(crumbSep)
		}
		s.x0, s.x1 = x, x+lipgloss.Width(s.label)
		x = s.x1
		out = append(out, s)
	}
	return out
}

// crumbAt returns what a click at column x of the breadcrumb row means:
// a directory, one of the history sentinels, or "" for empty space.
func (m Model) crumbAt(x int) string {
	for _, c := range m.crumbs() {
		if x >= c.x0 && x < c.x1 {
			return c.target
		}
	}
	return ""
}

func (m Model) renderBreadcrumb() string {
	var b strings.Builder
	col := 0
	for i, c := range m.crumbs() {
		if c.x0 > col {
			pad := strings.Repeat(" ", c.x0-col)
			// Between two path segments the padding is the separator.
			if i > 2 && c.x0-col == lipgloss.Width(crumbSep) {
				pad = theme.BreadcrumbSep.Render(crumbSep)
			}
			b.WriteString(pad)
		}
		style := theme.BreadcrumbNormal
		switch {
		case c.target == crumbBack:
			style = arrowStyle(m.filelist.CanGoBack())
		case c.target == crumbForward:
			style = arrowStyle(m.filelist.CanGoForward())
		case c.last:
			style = theme.BreadcrumbActive
		}
		b.WriteString(style.Render(c.label))
		col = c.x1
	}
	line := b.String()
	if w := lipgloss.Width(line); w < m.width {
		line += strings.Repeat(" ", m.width-w)
	}
	return clip(line, m.width)
}

// arrowStyle draws a history arrow lit when there is somewhere to go and
// barely there when there is not.
func arrowStyle(available bool) lipgloss.Style {
	if available {
		return lipgloss.NewStyle().Foreground(theme.FgColor).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(theme.SeparatorColor)
}

// childOf names the entry of ancestor that leads towards descendant —
// the row to land on after jumping up to ancestor.
func childOf(ancestor, descendant string) string {
	rel, err := filepath.Rel(ancestor, descendant)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	return first
}

// ── Status bar ─────────────────────────────────────────────────────────

func (m Model) renderStatusBar() string {
	if m.dialogMode != DialogNone {
		return m.renderPrompt()
	}

	sep := theme.BreadcrumbSep.Render("  ·  ")

	// Left: the repository, then whatever just happened.
	var left []string
	if m.gitBranch != "" {
		branch := lipgloss.NewStyle().Foreground(theme.AccentGreen).Bold(true).Render(m.gitBranch)
		if m.gitAhead > 0 {
			branch += theme.Dim.Render(fmt.Sprintf(" ↑%d", m.gitAhead))
		}
		if m.gitBehind > 0 {
			branch += theme.Dim.Render(fmt.Sprintf(" ↓%d", m.gitBehind))
		}
		left = append(left, branch)
		if n := len(m.gitFileStat); n > 0 {
			left = append(left, lipgloss.NewStyle().Foreground(theme.AccentBlue).
				Render(fmt.Sprintf("%d changed", n)))
		}
	}
	if m.statusMsg != "" {
		style := theme.Normal
		if statusIsError(m.statusMsg) {
			style = theme.ErrorMsg
		}
		left = append(left, style.Render(m.statusMsg))
	}
	if n := ActiveDeletesCount(); n > 0 {
		left = append(left, lipgloss.NewStyle().Foreground(theme.AccentYellow).Render(
			fmt.Sprintf("%d in the bin, %ds to undo", n, int(NextDeleteRemaining().Seconds()))))
	}

	// Right: the list itself — what is marked, how it is narrowed, where
	// the cursor is, and the free space Finder keeps in this corner.
	var right []string
	if n := m.filelist.MarkedCount(); n > 0 {
		marked := fmt.Sprintf("%d marked", n)
		if size := m.filelist.MarkedSize(); size > 0 {
			marked += ", " + filesystem.FormatSize(size)
		}
		right = append(right, lipgloss.NewStyle().Foreground(theme.AccentYellow).Bold(true).Render(marked))
	}
	if f := m.filelist.Filter(); f != "" && !m.filelist.IsFiltering() {
		right = append(right, lipgloss.NewStyle().Foreground(theme.AccentBlue).Render("filter: "+f))
	}
	if m.filelist.ShowHidden() {
		right = append(right, theme.StatusInfo.Render("hidden shown"))
	}
	if total := len(m.filelist.Entries()); total > 0 {
		right = append(right, theme.StatusInfo.Render(
			fmt.Sprintf("%d of %d", m.filelist.CursorIdx()+1, total)))
	} else {
		right = append(right, theme.StatusInfo.Render("empty"))
	}
	if m.diskFreeOK {
		right = append(right, theme.StatusInfo.Render(filesystem.FormatSize(m.diskFree)+" free"))
	}

	leftStr := " " + strings.Join(left, sep)
	rightStr := strings.Join(right, sep) + " "

	// The right side is the stable one; when the window is narrow the
	// message on the left gives way.
	if room := m.width - lipgloss.Width(rightStr) - 2; lipgloss.Width(leftStr) > room {
		leftStr = lipgloss.NewStyle().MaxWidth(max(room, 0)).Render(leftStr)
	}
	gap := max(0, m.width-lipgloss.Width(leftStr)-lipgloss.Width(rightStr))
	// In a window too narrow even for the right side alone, cut rather
	// than wrap: a wrapped row pushes the whole frame down by one.
	return clip(leftStr+strings.Repeat(" ", gap)+rightStr, m.width)
}

// clip cuts a rendered line down to w cells.
func clip(line string, w int) string {
	if lipgloss.Width(line) <= w {
		return line
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(line)
}

// renderPrompt draws the status bar while it is asking for something: a
// yes or no, or a line of text. It takes the bar's background so the
// question cannot be mistaken for an ordinary status message.
func (m Model) renderPrompt() string {
	bar := theme.StatusBar.Foreground(theme.FgColor)
	label := func(s string, color lipgloss.TerminalColor) string {
		return bar.Foreground(color).Bold(true).Render(" " + s + " ")
	}
	finish := func(parts ...string) string {
		line := strings.Join(parts, "")
		if w := lipgloss.Width(line); w < m.width {
			line += bar.Render(strings.Repeat(" ", m.width-w))
		}
		return clip(line, m.width)
	}
	yesNo := bar.Foreground(theme.FgDimColor).Render("  y yes  ·  n no ")

	switch m.dialogMode {
	case DialogGitDiscard:
		return finish(label("Discard changes", theme.AccentRed),
			bar.Render(filepath.Base(m.gitDiscardPath)+"?"), yesNo)
	case DialogDelete:
		what := m.opEntries[0].Name
		if len(m.opEntries) > 1 {
			what = fmt.Sprintf("%d items", len(m.opEntries))
		}
		return finish(label("Delete", theme.AccentRed), bar.Render(what+"?"), yesNo)
	}

	title := "Rename"
	switch m.dialogMode {
	case DialogNewFile:
		title = "New file"
	case DialogNewDir:
		title = "New folder"
	case DialogGoToPath:
		title = "Go to"
	}
	return finish(label(title, theme.AccentBlue), bar.Render(m.textInput.View()),
		bar.Foreground(theme.FgDimColor).Render("  Enter confirm  ·  Esc cancel "))
}

// refreshDisk re-reads the free space on the volume being shown.
func (m *Model) refreshDisk() {
	m.diskFree, m.diskFreeOK = filesystem.FreeSpace(m.currentPath)
}
