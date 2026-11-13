// Package search is the project-wide search popup: go to a file by name,
// or find text in files.
//
// Neither mode ever blocks the interface. The list of files is read once,
// in the background, when the popup opens, and typing only filters it in
// memory. Text search waits for a pause in the typing and then runs
// ripgrep in the background; a result that arrives after the query has
// moved on is thrown away. Under the list, a preview shows the lines
// around the match, so most of the time the answer is on screen before
// anything is opened.
package search

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/JohnnyReverb9/tuiple/filesystem"
	"github.com/JohnnyReverb9/tuiple/theme"
)

// SearchMode defines whether we are searching by name or content.
type SearchMode int

const (
	ModeNameSearch SearchMode = iota
	ModeContentSearch
)

// SearchCompletedMsg is emitted when a result is picked. Reveal is true
// when it was picked with Tab: show it in the list, do not open it.
type SearchCompletedMsg struct {
	SelectedPath string
	LineNum      int
	Mode         SearchMode
	Reveal       bool
}

const (
	maxResults   = 300 // rows kept; more than anyone scrolls through
	maxFiles     = 200000
	walkDepth    = 8
	debounce     = 160 * time.Millisecond
	previewBytes = 512 << 10
)

// Messages the popup sends itself. Each carries the generation it was
// started in, so stale answers can be told from current ones.
type (
	filesLoadedMsg struct {
		gen   int
		files []string
	}
	debounceMsg  struct{ seq int }
	contentFound struct {
		seq     int
		results []filesystem.SearchMatch
	}
)

// Model is the search overlay model.
type Model struct {
	mode     SearchMode
	input    textinput.Model
	results  []filesystem.SearchMatch
	cursor   int
	offset   int
	rootPath string
	active   bool
	width    int
	height   int

	gen       int      // bumped on every Start
	files     []string // paths relative to rootPath (name mode)
	loading   bool     // the file list is still being read
	seq       int      // bumped on every edit of the query (content mode)
	searching bool

	preview     []string // lines shown under the list
	previewFrom int      // number of the first preview line
	previewHit  int      // line to highlight, 0 for none
}

// New creates a new Search overlay model.
func New() Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 100
	ti.PlaceholderStyle = theme.Dim
	ti.TextStyle = theme.Normal
	return Model{input: ti}
}

func (m *Model) SetSize(w, h int) { m.width = w; m.height = h }
func (m Model) IsActive() bool    { return m.active }

// AcceptsText reports whether the query field is currently taking
// keystrokes, so the app can skip keyboard-layout translation while the
// user types a search term.
func (m Model) AcceptsText() bool { return m.active && m.input.Focused() }

// Start opens the popup. root is where to search from — the app passes
// the repository root when there is one, so a search covers the project
// and not just the directory the list happens to be in.
func (m *Model) Start(mode SearchMode, root string) tea.Cmd {
	m.active = true
	m.mode = mode
	m.rootPath = root
	m.gen++
	m.seq++
	m.input.SetValue("")
	m.results = nil
	m.files = nil
	m.cursor, m.offset = 0, 0
	m.preview = nil
	m.searching = false
	m.loading = mode == ModeNameSearch

	cmds := []tea.Cmd{m.input.Focus()}
	if mode == ModeNameSearch {
		m.input.Placeholder = "file name…"
		gen := m.gen
		cmds = append(cmds, func() tea.Msg { return filesLoadedMsg{gen, listFiles(root)} })
	} else {
		m.input.Placeholder = "text to find…"
	}
	return tea.Batch(cmds...)
}

// Stop closes the search overlay.
func (m *Model) Stop() {
	m.active = false
	m.input.Blur()
}

// ── Backends ───────────────────────────────────────────────────────────

// skipDir names directories never worth descending into when ripgrep is
// not there to consult .gitignore.
var skipDir = map[string]bool{"node_modules": true, "vendor": true, "target": true, "dist": true}

// listFiles returns every file under root, relative to it. ripgrep does
// it best — it honours .gitignore — and a plain walk stands in when it
// is not installed.
func listFiles(root string) []string {
	if rg, err := exec.LookPath("rg"); err == nil {
		cmd := exec.Command(rg, "--files", "--hidden", "--glob", "!.git")
		cmd.Dir = root
		if out, err := cmd.Output(); err == nil {
			files := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
			if len(files) > maxFiles {
				files = files[:maxFiles]
			}
			if len(files) == 1 && files[0] == "" {
				return nil
			}
			return files
		}
	}
	var files []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || skipDir[name] ||
				strings.Count(rel, string(os.PathSeparator)) >= walkDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxFiles {
			return filepath.SkipAll
		}
		files = append(files, rel)
		return nil
	})
	return files
}

// findText searches file contents for a literal string, smart-case.
func findText(root, query string) []filesystem.SearchMatch {
	rg, err := exec.LookPath("rg")
	if err != nil {
		res := filesystem.ContentSearch(root, query, walkDepth)
		if len(res) > maxResults {
			res = res[:maxResults]
		}
		return res
	}
	cmd := exec.Command(rg, "--line-number", "--no-heading", "--color=never",
		"--smart-case", "--fixed-strings", "--hidden", "--glob", "!.git",
		"--max-columns", "400", "--max-count", "50", "--", query, ".")
	cmd.Dir = root
	stdout, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return nil
	}
	var results []filesystem.SearchMatch
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() && len(results) < maxResults {
		// ./path/to/file:12:the line
		parts := strings.SplitN(sc.Text(), ":", 3)
		if len(parts) < 3 {
			continue
		}
		line, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		rel := strings.TrimPrefix(parts[0], "./")
		results = append(results, filesystem.SearchMatch{
			Path: filepath.Join(root, rel), Name: rel,
			MatchedLine: strings.TrimSpace(parts[2]), LineNum: line,
		})
	}
	// Enough rows: stop ripgrep rather than read the rest of a huge tree.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return results
}

func (m *Model) filterNames() {
	query := strings.TrimSpace(m.input.Value())
	m.results = m.results[:0]
	m.cursor, m.offset = 0, 0
	if query == "" {
		return
	}
	for _, match := range fuzzy.Find(query, m.files) {
		if len(m.results) >= maxResults {
			break
		}
		m.results = append(m.results, filesystem.SearchMatch{
			Path: filepath.Join(m.rootPath, match.Str), Name: match.Str,
		})
	}
}

// loadPreview reads the lines to show under the list for the row the
// cursor is on: the top of the file, or the neighbourhood of the match.
func (m *Model) loadPreview() {
	m.preview, m.previewHit, m.previewFrom = nil, 0, 1
	if m.cursor < 0 || m.cursor >= len(m.results) {
		return
	}
	res := m.results[m.cursor]
	info, err := os.Stat(res.Path)
	if err != nil || info.IsDir() || info.Size() > previewBytes {
		return
	}
	data, err := os.ReadFile(res.Path)
	if err != nil || strings.IndexByte(string(data), 0) >= 0 {
		return
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	want := m.previewHeight()
	from := 0
	if res.LineNum > 0 {
		m.previewHit = res.LineNum
		from = max(0, res.LineNum-1-want/2)
	}
	from = max(0, min(from, len(lines)-want))
	m.previewFrom = from + 1
	m.preview = lines[from:min(len(lines), from+want)]
}

// ── Update ─────────────────────────────────────────────────────────────

// Update handles search input and list navigation.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}

	switch msg := msg.(type) {
	case filesLoadedMsg:
		if msg.gen == m.gen {
			m.files, m.loading = msg.files, false
			m.filterNames()
			m.loadPreview()
		}
		return m, nil
	case debounceMsg:
		query := strings.TrimSpace(m.input.Value())
		if msg.seq != m.seq || len(query) < 2 {
			return m, nil
		}
		m.searching = true
		seq, root := m.seq, m.rootPath
		return m, func() tea.Msg { return contentFound{seq, findText(root, query)} }
	case contentFound:
		if msg.seq == m.seq {
			m.searching = false
			m.results = msg.results
			m.cursor, m.offset = 0, 0
			m.loadPreview()
		}
		return m, nil

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
		case "up", "ctrl+k", "ctrl+p":
			m.move(-1)
			return m, nil
		case "down", "ctrl+j", "ctrl+n":
			m.move(1)
			return m, nil
		case "pgup":
			m.move(-m.listHeight())
			return m, nil
		case "pgdown":
			m.move(m.listHeight())
			return m, nil
		case "esc":
			m.Stop()
			return m, nil
		case "enter", "tab":
			if m.cursor < 0 || m.cursor >= len(m.results) {
				if msg.String() == "enter" {
					m.Stop()
				}
				return m, nil
			}
			res := m.results[m.cursor]
			done := SearchCompletedMsg{
				SelectedPath: res.Path, LineNum: res.LineNum,
				Mode: m.mode, Reveal: msg.String() == "tab",
			}
			m.Stop()
			return m, func() tea.Msg { return done }
		}
	}

	prev := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() == prev {
		return m, cmd
	}

	if m.mode == ModeNameSearch {
		m.filterNames()
		m.loadPreview()
		return m, cmd
	}
	// Text search: wait for the typing to pause before starting ripgrep.
	m.seq++
	seq := m.seq
	if len(strings.TrimSpace(m.input.Value())) < 2 {
		m.results, m.searching = nil, false
		m.loadPreview()
		return m, cmd
	}
	return m, tea.Batch(cmd, tea.Tick(debounce, func(time.Time) tea.Msg { return debounceMsg{seq} }))
}

func (m *Model) move(delta int) {
	if len(m.results) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.results)-1)
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.loadPreview()
}

// ── View ───────────────────────────────────────────────────────────────

func (m Model) innerWidth() int { return max(m.width-2, 30) }

// The box is: border, query, rule, list, rule, preview, rule, footer,
// border. The list and the preview share what is left.
func (m Model) body() int          { return max(m.height-7, 4) }
func (m Model) listHeight() int    { return max(m.body()*45/100, 2) }
func (m Model) previewHeight() int { return max(m.body()-m.listHeight(), 2) }

// View renders the search overlay.
func (m Model) View() string {
	if !m.active {
		return ""
	}
	w := m.innerWidth()
	rule := theme.Separator.Render(strings.Repeat("─", w))

	name := "Go to file"
	if m.mode == ModeContentSearch {
		name = "Find in files"
	}
	title := lipgloss.NewStyle().Foreground(theme.AccentBlue).Bold(true).Render(" " + name + " ")
	scope := theme.Muted.Render(" in " + filepath.Base(m.rootPath) + " ")
	state := fmt.Sprintf("%d ", len(m.results))
	switch {
	case m.loading:
		state = "reading files… "
	case m.searching:
		state = "searching… "
	case len(m.results) >= maxResults:
		state = fmt.Sprintf("%d+ ", maxResults)
	}
	count := theme.Muted.Render(state)
	fieldW := max(w-lipgloss.Width(title)-lipgloss.Width(scope)-lipgloss.Width(count)-1, 8)
	m.input.Width = fieldW - 1
	field := lipgloss.NewStyle().Width(fieldW).MaxWidth(fieldW).Render(m.input.View())
	lines := []string{pad(title+" "+field+scope+count, w), rule}

	// List.
	listH := m.listHeight()
	if len(m.results) == 0 {
		hint := "  Type to search"
		if strings.TrimSpace(m.input.Value()) != "" && !m.loading && !m.searching {
			hint = "  Nothing found"
		}
		lines = append(lines, pad(theme.Dim.Render(hint), w))
	}
	for i := m.offset; i < min(m.offset+listH, len(m.results)); i++ {
		lines = append(lines, m.renderRow(m.results[i], i == m.cursor, w))
	}
	for len(lines) < 2+listH {
		lines = append(lines, strings.Repeat(" ", w))
	}

	// Preview.
	lines = append(lines, rule)
	numW := len(strconv.Itoa(m.previewFrom + len(m.preview)))
	for i, text := range m.preview {
		n := m.previewFrom + i
		text = strings.ReplaceAll(text, "\t", "    ")
		num := fmt.Sprintf(" %*d  ", numW, n)
		if n == m.previewHit {
			hit := lipgloss.NewStyle().Background(theme.BgSelected)
			line := hit.Foreground(theme.AccentBlue).Bold(true).Render(num) +
				hit.Foreground(theme.FgColor).Render(text)
			line = clip(line, w)
			if lw := lipgloss.Width(line); lw < w {
				line += hit.Render(strings.Repeat(" ", w-lw))
			}
			lines = append(lines, line)
			continue
		}
		lines = append(lines, pad(theme.Muted.Render(num)+theme.PreviewContent.Render(text), w))
	}
	for len(lines) < 3+listH+m.previewHeight() {
		lines = append(lines, strings.Repeat(" ", w))
	}

	lines = append(lines, rule, pad(theme.Muted.Render(
		" Enter open  ·  Tab show in the list  ·  ↑ ↓ move  ·  Esc close"), w))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.BorderActive).
		Render(strings.Join(lines, "\n"))
}

// renderRow draws one result: the file name first, because that is what
// is being looked for, then the directory (or the matching line).
func (m Model) renderRow(res filesystem.SearchMatch, cursor bool, w int) string {
	base := lipgloss.NewStyle()
	if cursor {
		base = base.Background(theme.BgSelected)
	}
	strong := base.Foreground(theme.FgColor)
	if cursor {
		strong = strong.Bold(true)
	}
	dim := base.Foreground(theme.FgMutedColor)

	file, dir := filepath.Base(res.Name), filepath.Dir(res.Name)
	if dir == "." {
		dir = ""
	}
	var line string
	if m.mode == ModeNameSearch {
		line = base.Render(" ") + strong.Render(file) + dim.Render("  "+dir)
	} else {
		loc := fmt.Sprintf("%s:%d", file, res.LineNum)
		line = base.Render(" ") + base.Foreground(theme.AccentBlue).Render(loc) +
			strong.Render("  "+res.MatchedLine) + dim.Render("  "+dir)
	}
	line = clip(line, w)
	if lw := lipgloss.Width(line); lw < w {
		line += base.Render(strings.Repeat(" ", w-lw))
	}
	return line
}

func clip(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

func pad(s string, w int) string {
	s = clip(s, w)
	if lw := lipgloss.Width(s); lw < w {
		s += strings.Repeat(" ", w-lw)
	}
	return s
}
