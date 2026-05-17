// Package app wires together the sidebar, file list, and preview
// into a single three-panel Bubble Tea application.
package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/bookmarks"
	"tuiple/clipboard"
	"tuiple/components/filelist"
	"tuiple/components/git"
	"tuiple/components/preview"
	"tuiple/components/preview/mediarender"
	"tuiple/components/search"
	"tuiple/components/sidebar"
	"tuiple/filesystem"
	"tuiple/theme"
)

type deleteTickMsg struct{}

func deleteTick() tea.Cmd {
	return tea.Tick(time.Second, func(_ time.Time) tea.Msg {
		return deleteTickMsg{}
	})
}

type dirPollMsg struct{}

func dirPollCmd() tea.Cmd {
	return tea.Tick(time.Second, func(_ time.Time) tea.Msg {
		return dirPollMsg{}
	})
}

func (m *Model) updateLastDirMod(path string) {
	if info, err := os.Stat(path); err == nil {
		m.lastDirMod = info.ModTime()
	}
}

// stampPreview records the mtime of entry so dirPollMsg can detect when the
// file changes on disk and trigger a preview refresh automatically.
func (m *Model) stampPreview(entry *filesystem.FileEntry) {
	if entry == nil || entry.IsDir {
		m.previewLoadedPath = ""
		return
	}
	m.previewLoadedPath = entry.Path
	if info, err := os.Stat(entry.Path); err == nil {
		m.previewFileMod = info.ModTime()
	}
}

// refreshGit re-probes the git repo root, current branch, and porcelain
// status, then pushes the file-level and directory-level markers into
// the filelist component. Safe to call from any directory: when the path
// is not inside a repo all git state is cleared.
func (m *Model) refreshGit() {
	root, err := git.FindRoot(m.currentPath)
	if err != nil {
		m.gitRoot = ""
		m.gitBranch = ""
		m.gitFileStat = nil
		m.gitDirHas = nil
		m.filelist.SetGitState(nil, nil)
		return
	}
	m.gitRoot = root
	if br, err := git.CurrentBranch(root); err == nil {
		m.gitBranch = br
	} else {
		m.gitBranch = ""
	}
	changes, _ := git.Status(root)
	fileStat := make(map[string]string, len(changes))
	dirHas := make(map[string]struct{}, len(changes))
	for _, c := range changes {
		abs := filepath.Join(root, c.Path)
		fileStat[abs] = string(c.IndexStatus) + string(c.WorktreeStatus)
		parent := filepath.Dir(abs)
		for {
			if parent == root || parent == "/" || parent == "." || parent == "" {
				break
			}
			if !strings.HasPrefix(parent, root) {
				break
			}
			dirHas[parent] = struct{}{}
			next := filepath.Dir(parent)
			if next == parent {
				break
			}
			parent = next
		}
	}
	m.gitFileStat = fileStat
	m.gitDirHas = dirHas
	m.filelist.SetGitState(fileStat, dirHas)
}

// ── Dialog / State ─────────────────────────────────────────────────────

type DialogMode int

const (
	DialogNone DialogMode = iota
	DialogDelete
	DialogRename
	DialogNewFile
	DialogNewDir
	DialogGitDiscard // y/N confirmation before `git checkout -- <file>`
	DialogGoToPath   // free-form path entry (Finder-style ⌘⇧G)
)

// ── Panel enum ─────────────────────────────────────────────────────────

type panel int

const (
	panelSidebar panel = iota
	panelFileList
	panelPreview
)

// ── Model ──────────────────────────────────────────────────────────────

// Model is the root application model.
type Model struct {
	sidebar  sidebar.Model
	filelist filelist.Model
	preview  preview.Model

	textInput  textinput.Model
	dialogMode DialogMode
	opEntries  []filesystem.FileEntry
	statusMsg  string

	searchOverlay    search.Model
	gitOverlay       git.Model
	branchesPopup    git.BranchesPopup
	fileHistoryPopup git.FileHistoryPopup
	blamePopup       git.BlamePopup
	historyPopup     HistoryPopup
	awaitingSort     bool

	gitRoot        string
	gitBranch      string
	gitFileStat    map[string]string
	gitDirHas      map[string]struct{}
	gitDiscardPath string // abs path pending discard confirmation

	// Preview auto-refresh: track the path and mtime of the last loaded file
	// so dirPollMsg can reload it when the file changes on disk.
	previewLoadedPath string
	previewFileMod    time.Time

	active      panel
	currentPath string

	deletesTicking bool

	width  int
	height int

	showHelp   bool
	helpTabIdx int // 0 = System, 1 = Git
	ready      bool

	lastDirMod time.Time
}

// New creates the application model.
func New(startDir string) Model {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.CharLimit = 255
	ti.Width = 40
	ti.PlaceholderStyle = theme.Dim
	ti.TextStyle = theme.Normal

	var lastMod time.Time
	if info, err := os.Stat(startDir); err == nil {
		lastMod = info.ModTime()
	}

	return Model{
		sidebar:          sidebar.New(),
		filelist:         filelist.New(startDir),
		preview:          preview.New(),
		textInput:        ti,
		searchOverlay:    search.New(),
		gitOverlay:       git.New(),
		branchesPopup:    git.NewBranches(),
		fileHistoryPopup: git.NewFileHistory(),
		blamePopup:       git.NewBlame(),
		historyPopup:     NewHistoryPopup(),
		active:           panelFileList,
		currentPath:      startDir,
		lastDirMod:       lastMod,
	}
}

// ── Init ───────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if entry := m.filelist.SelectedEntry(); entry != nil {
		cmds = append(cmds, m.preview.LoadFile(*entry))
	}
	cmds = append(cmds, dirPollCmd())
	return tea.Batch(cmds...)
}

// ── Update ─────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(deleteTickMsg); ok {
		if ActiveDeletesCount() > 0 {
			return m, deleteTick()
		}
		m.deletesTicking = false
		return m, nil
	}

	if _, ok := msg.(dirPollMsg); ok {
		if info, err := os.Stat(m.currentPath); err == nil {
			if info.ModTime().After(m.lastDirMod) {
				m.lastDirMod = info.ModTime()
				m.filelist, _ = m.filelist.Update(filelist.RefreshListMsg{})
			}
		}
		m.refreshGit()
		// Auto-refresh preview when the viewed file is modified on disk,
		// or when the selected entry changed (e.g. the previewed file was deleted).
		var previewCmd tea.Cmd
		if entry := m.filelist.SelectedEntry(); entry != nil && !entry.IsDir {
			if entry.Path != m.previewLoadedPath {
				// Cursor landed on a different file (e.g. after external deletion).
				previewCmd = m.preview.LoadFile(*entry)
				m.stampPreview(entry)
			} else if info, err := os.Stat(entry.Path); err == nil && info.ModTime().After(m.previewFileMod) {
				// Same file, but it was modified on disk.
				m.previewFileMod = info.ModTime()
				previewCmd = m.preview.LoadFile(*entry)
			}
		}
		return m, tea.Batch(dirPollCmd(), previewCmd)
	}

	if _, ok := msg.(git.BranchesChangedMsg); ok {
		m.refreshGit()
		return m, nil
	}

	// ── Intercept History Popup ────────────────────────────────────
	if m.historyPopup.IsActive() {
		var cmd tea.Cmd
		m.historyPopup, cmd = m.historyPopup.Update(msg)
		return m, cmd
	}

	// ── Intercept Blame Popup ─────────────────────────────────────
	if m.blamePopup.IsActive() {
		var cmd tea.Cmd
		m.blamePopup, cmd = m.blamePopup.Update(msg)
		return m, cmd
	}

	// ── Intercept File History Popup ───────────────────────────────
	if m.fileHistoryPopup.IsActive() {
		var cmd tea.Cmd
		m.fileHistoryPopup, cmd = m.fileHistoryPopup.Update(msg)
		return m, cmd
	}

	// ── Intercept Branches Popup ───────────────────────────────────
	if m.branchesPopup.IsActive() {
		var cmd tea.Cmd
		m.branchesPopup, cmd = m.branchesPopup.Update(msg)
		return m, cmd
	}

	// ── Intercept Git Overlay ──────────────────────────────────────
	if m.gitOverlay.IsActive() {
		var cmd tea.Cmd
		m.gitOverlay, cmd = m.gitOverlay.Update(msg)
		return m, cmd
	}

	// ── Intercept Search Overlay ───────────────────────────────────
	if m.searchOverlay.IsActive() {
		var cmd tea.Cmd
		m.searchOverlay, cmd = m.searchOverlay.Update(msg)
		return m, cmd
	}

	// ── Intercept Dialog keys ──────────────────────────────────────
	if m.dialogMode != DialogNone {
		return m.updateDialog(msg)
	}

	// ── Sorting ───────────────────────────────────────
	if m.awaitingSort {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			m.awaitingSort = false
			switch keyMsg.String() {
			case "n":
				m.filelist, _ = m.filelist.Update(filelist.SortListMsg{Mode: filesystem.SortByName})
				m.statusMsg = "Sorted by name"
			case "s":
				m.filelist, _ = m.filelist.Update(filelist.SortListMsg{Mode: filesystem.SortBySize})
				m.statusMsg = "Sorted by size"
			case "d":
				m.filelist, _ = m.filelist.Update(filelist.SortListMsg{Mode: filesystem.SortByDate})
				m.statusMsg = "Sorted by date"
			default:
				m.statusMsg = "Sort cancelled"
			}
			return m, nil
		}
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	// ── Mouse Router ───────────────────────────────────────────────
	case tea.MouseMsg:
		if msg.Type != tea.MouseWheelUp && msg.Type != tea.MouseWheelDown {
			// Ignore hover/motion events to prevent application freeze/flood
			return m, nil
		}
		sidebarW, filelistW, _ := m.panelWidths()
		if msg.X > sidebarW+filelistW+1 {
			// Hover over Preview
			m.preview, _ = m.preview.Update(msg)
		} else if msg.X > sidebarW {
			// Hover over Filelist
			prevCursor := m.filelist.CursorIdx()
			m.filelist, _ = m.filelist.Update(msg)
			if m.filelist.CursorIdx() != prevCursor {
				m.currentPath = m.filelist.CurrentPath()
				if entry := m.filelist.SelectedEntry(); entry != nil {
					cmds = append(cmds, m.preview.LoadFile(*entry))
				}
			}
		} else {
			// Hover over Sidebar
			m.sidebar, _ = m.sidebar.Update(msg)
		}
		return m, tea.Batch(cmds...)

	// ── Resize ─────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateSizes()
		if !m.ready {
			m.refreshGit()
		}
		m.ready = true

		if entry := m.filelist.SelectedEntry(); entry != nil {
			cmds = append(cmds, m.preview.LoadFile(*entry))
			m.stampPreview(entry)
		}
		return m, tea.Batch(cmds...)

	// ── Global keys ────────────────────────────────────────────────
	case tea.KeyMsg:
		// Help screen swallows all keys while visible.
		if m.showHelp {
			switch msg.String() {
			case "?", "esc", "q":
				m.showHelp = false
			case "tab", "right", "l":
				m.helpTabIdx = (m.helpTabIdx + 1) % 2
			case "shift+tab", "left", "h":
				m.helpTabIdx = (m.helpTabIdx + 2 - 1) % 2
			case "1":
				m.helpTabIdx = 0
			case "2":
				m.helpTabIdx = 1
			}
			return m, nil
		}

		if m.filelist.IsFiltering() {
			var cmd tea.Cmd
			m.filelist, cmd = m.filelist.Update(msg)
			if entry := m.filelist.SelectedEntry(); entry != nil {
				cmds = append(cmds, m.preview.LoadFile(*entry))
			}
			return m, tea.Batch(append(cmds, cmd)...)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			CleanupPendingDeletes()
			return m, tea.Quit
		case "-", "−": // Catch both ASCII minus and Unicode minus just in case
			if m.preview.IsAudio() {
				return m, m.preview.SeekAudio(-5)
			}
		case "=", "+":
			if m.preview.IsAudio() {
				// if shifted, it's + which usually means plus, but we'll use + for +30 and = for +5
				if msg.String() == "+" {
					return m, m.preview.SeekAudio(30)
				}
				return m, m.preview.SeekAudio(5)
			}
		case "_":
			if m.preview.IsAudio() {
				return m, m.preview.SeekAudio(-30)
			}
		case "tab":
			m.cyclePanel(1)
			m.updateFocus()
			return m, nil
		case "shift+tab":
			m.cyclePanel(-1)
			m.updateFocus()
			return m, nil
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case ":":
			m.dialogMode = DialogGoToPath
			m.textInput.SetValue("")
			m.textInput.Placeholder = "Enter path..."
			m.textInput.Focus()
			return m, nil
		case "Y":
			m.historyPopup.Start()
			return m, nil
		case "f":
			return m, m.searchOverlay.Start(search.ModeNameSearch, m.currentPath)
		case "F":
			return m, m.searchOverlay.Start(search.ModeContentSearch, m.currentPath)
		case "ctrl+g":
			return m, m.gitOverlay.Start(m.currentPath)
		case "b":
			return m, m.branchesPopup.Start(m.currentPath)
		case "'":
			added, err := bookmarks.Toggle(m.currentPath)
			if err != nil {
				m.statusMsg = "Error saving bookmark: " + err.Error()
			} else {
				if added {
					m.statusMsg = "Added to favorites"
				} else {
					m.statusMsg = "Removed from favorites"
				}
				m.sidebar = sidebar.New() // Reload sidebar to show updated favorites
				m.updateSizes()
			}
			return m, nil
		case "o":
			m.awaitingSort = true
			m.statusMsg = "Sort by: (n)ame, (s)ize, (d)ate"
			return m, nil
		case "S":
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "sh"
			}
			cmd := exec.Command(shell)
			cmd.Dir = m.currentPath
			return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
				return filelist.RefreshListMsg{}
			})
		case "u":
			undoMsg, err := Undo()
			if err != nil {
				m.statusMsg = "Error: " + err.Error()
			} else {
				m.statusMsg = undoMsg
				m.filelist, _ = m.filelist.Update(filelist.RefreshListMsg{})
			}
			var cmd tea.Cmd
			if ActiveDeletesCount() > 0 && !m.deletesTicking {
				m.deletesTicking = true
				cmd = deleteTick()
			}
			return m, cmd
		case "U":
			redoMsg, err := Redo()
			if err != nil {
				m.statusMsg = "Error: " + err.Error()
			} else {
				m.statusMsg = redoMsg
				m.filelist, _ = m.filelist.Update(filelist.RefreshListMsg{})
			}
			var cmd tea.Cmd
			if ActiveDeletesCount() > 0 && !m.deletesTicking {
				m.deletesTicking = true
				cmd = deleteTick()
			}
			return m, cmd
		}

	// ── File Operations ────────────────────────────────────────────
	case filelist.CopyMsg:
		var items []clipboard.Item
		for _, e := range msg.Entries {
			items = append(items, clipboard.Item{Entry: e})
		}
		clipboard.Set(items, clipboard.OpCopy)
		m.statusMsg = fmt.Sprintf("Copied %d items", len(items))
		m.filelist, _ = m.filelist.Update(filelist.ClearSelectionMsg{})
		return m, nil
	case filelist.CutMsg:
		var items []clipboard.Item
		for _, e := range msg.Entries {
			items = append(items, clipboard.Item{Entry: e})
		}
		clipboard.Set(items, clipboard.OpCut)
		m.statusMsg = fmt.Sprintf("Cut %d items", len(items))
		m.filelist, _ = m.filelist.Update(filelist.ClearSelectionMsg{})
		return m, nil
	case filelist.PasteRequestMsg:
		items, op := clipboard.Get()
		if len(items) == 0 {
			m.statusMsg = "Clipboard is empty"
			return m, nil
		}
		return m.handlePaste(items, op)
	case filelist.DeleteRequestMsg:
		m.dialogMode = DialogDelete
		m.opEntries = msg.Entries
		return m, nil
	case filelist.RenameRequestMsg:
		m.dialogMode = DialogRename
		m.opEntries = []filesystem.FileEntry{msg.Entry}
		m.textInput.SetValue(msg.Entry.Name)
		m.textInput.Focus()
		return m, nil
	case filelist.OpenFileRequestMsg:
		// If it's an audio file and it's currently loaded in preview,
		// toggle playback instead of opening in an editor.
		if m.preview.IsAudio() && m.preview.Path() == msg.Path {
			cmd := m.preview.ToggleAudio()
			return m, cmd
		}

		ext := strings.ToLower(filepath.Ext(msg.Path))
		kind := mediarender.Classify(ext)

		// Open images and videos in terminal using 'chafa'
		if kind == mediarender.KindImage || kind == mediarender.KindVideo {
			if _, err := exec.LookPath("chafa"); err == nil {
				// Clear screen, show image in high quality, wait for any key, then clear again.
				// This prevents image artifacts from remaining in the terminal buffer.
				shCmd := fmt.Sprintf("clear; chafa %q; echo; echo '  Press any key to return...'; stty raw -echo; dd bs=1 count=1 2>/dev/null; stty -raw echo; clear", msg.Path)
				cmd := exec.Command("sh", "-c", shCmd)
				return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
					return filelist.RefreshListMsg{}
				})
			}
		}

		// Attempt to open e-books and documents using Bookokrat
		if ext == ".pdf" || ext == ".epub" || ext == ".djvu" {
			if _, err := exec.LookPath("bookokrat"); err == nil {
				cmd := exec.Command("bookokrat", msg.Path)
				return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
					return filelist.RefreshListMsg{}
				})
			}
		}

		if ext == ".pdf" {
			if _, err := exec.LookPath("pdftotext"); err == nil {
				// Extract PDF text and read it with 'less' as fallback
				shCmd := fmt.Sprintf("pdftotext %q - | less -r", msg.Path)
				cmd := exec.Command("sh", "-c", shCmd)
				return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
					return filelist.RefreshListMsg{}
				})
			}
		}

		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi" // Default fallback editor
		}
		cmd := exec.Command(editor, msg.Path)
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return filelist.RefreshListMsg{}
		})
	case filelist.CreateFileRequestMsg:
		m.dialogMode = DialogNewFile
		m.textInput.SetValue("")
		m.textInput.Focus()
		return m, nil
	case filelist.CreateDirRequestMsg:
		m.dialogMode = DialogNewDir
		m.textInput.SetValue("")
		m.textInput.Focus()
		return m, nil

	// ── Git overlay → open sub-popups ────────────────────────────
	case git.OpenFileHistoryMsg:
		m.fileHistoryPopup.Start(msg.Repo, msg.Path)
		return m, nil
	case git.OpenBlameMsg:
		m.blamePopup.Start(msg.Repo, msg.Path)
		return m, nil

	// ── Git file operations from filelist ─────────────────────────
	case filelist.GitStageMsg:
		if m.gitRoot != "" {
			rel := strings.TrimPrefix(msg.Path, m.gitRoot+"/")
			if err := git.StageFile(m.gitRoot, rel); err != nil {
				m.statusMsg = "git add: " + err.Error()
			} else {
				m.statusMsg = "staged " + filepath.Base(msg.Path)
				m.refreshGit()
			}
		}
		return m, nil
	case filelist.GitUnstageMsg:
		if m.gitRoot != "" {
			rel := strings.TrimPrefix(msg.Path, m.gitRoot+"/")
			if err := git.UnstageFile(m.gitRoot, rel); err != nil {
				m.statusMsg = "git restore: " + err.Error()
			} else {
				m.statusMsg = "unstaged " + filepath.Base(msg.Path)
				m.refreshGit()
			}
		}
		return m, nil
	case filelist.GitDiscardMsg:
		if m.gitRoot != "" {
			m.dialogMode = DialogGitDiscard
			m.gitDiscardPath = msg.Path
		}
		return m, nil
	case filelist.GitIgnoreMsg:
		if m.gitRoot != "" {
			rel := strings.TrimPrefix(msg.Path, m.gitRoot+"/")
			added, err := git.AddToGitIgnore(m.gitRoot, rel)
			if err != nil {
				m.statusMsg = "gitignore: " + err.Error()
			} else {
				if added {
					m.statusMsg = "added to .gitignore: " + rel
				} else {
					m.statusMsg = "removed from .gitignore: " + rel
				}
				m.refreshGit()
			}
		}
		return m, nil
	case filelist.GitFileHistoryMsg:
		if m.gitRoot != "" {
			rel := strings.TrimPrefix(msg.Path, m.gitRoot+"/")
			m.fileHistoryPopup.Start(m.gitRoot, rel)
		}
		return m, nil
	case filelist.GitBlameMsg:
		if m.gitRoot != "" {
			rel := strings.TrimPrefix(msg.Path, m.gitRoot+"/")
			m.blamePopup.Start(m.gitRoot, rel)
		}
		return m, nil

	// ── Search completed ──────────────────────────────────────────
	case search.SearchCompletedMsg:
		dir := filepath.Dir(msg.SelectedPath)
		m.currentPath = dir
		var listCmd tea.Cmd
		m.filelist, listCmd = m.filelist.NavigateTo(dir)
		m.filelist = m.filelist.SelectByName(filepath.Base(msg.SelectedPath))
		cmds = append(cmds, listCmd)
		if entry := m.filelist.SelectedEntry(); entry != nil {
			cmds = append(cmds, m.preview.LoadFile(*entry))
		}
		if msg.Mode == search.ModeContentSearch && msg.LineNum > 0 {
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			lineArg := fmt.Sprintf("+%d", msg.LineNum)
			editorCmd := exec.Command(editor, lineArg, msg.SelectedPath)
			cmds = append(cmds, tea.ExecProcess(editorCmd, func(err error) tea.Msg {
				return filelist.RefreshListMsg{}
			}))
		}
		return m, tea.Batch(cmds...)

	// ── Sidebar navigation ─────────────────────────────────────────
	case sidebar.NavigateMsg:
		m.currentPath = msg.Path
		m.updateLastDirMod(msg.Path)
		var cmd tea.Cmd
		m.filelist, cmd = m.filelist.NavigateTo(msg.Path)
		cmds = append(cmds, cmd)
		m.refreshGit()
		if entry := m.filelist.SelectedEntry(); entry != nil {
			cmds = append(cmds, m.preview.LoadFile(*entry))
			m.stampPreview(entry)
		}
		return m, tea.Batch(cmds...)

	// ── Dir changed in file list ───────────────────────────────────
	case filelist.DirChangedMsg:
		m.currentPath = msg.Path
		m.refreshGit()
		if entry := m.filelist.SelectedEntry(); entry != nil {
			cmds = append(cmds, m.preview.LoadFile(*entry))
			m.stampPreview(entry)
		}
		return m, tea.Batch(cmds...)

	// ── Preview content loaded ─────────────────────────────────────
	case preview.ContentLoadedMsg:
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd

	// ── Directory size walk finished (delivered later than the
	//    initial ContentLoadedMsg for the same path) ───────────────
	case preview.DirSizeComputedMsg:
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd

	// ── Audio tick / seek routed regardless of active panel ────────
	case preview.AudioTickMsg:
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd

	case preview.AudioSeekFinishedMsg:
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd
	}

	// ── Route to active panel ──────────────────────────────────────
	prevCursor := m.filelist.CursorIdx()
	prevPath := m.filelist.CurrentPath()

	var cmd tea.Cmd
	switch m.active {
	case panelSidebar:
		m.sidebar, cmd = m.sidebar.Update(msg)
		cmds = append(cmds, cmd)
	case panelFileList:
		m.filelist, cmd = m.filelist.Update(msg)
		cmds = append(cmds, cmd)
	case panelPreview:
		m.preview, cmd = m.preview.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Cursor or path changed → refresh preview
	if m.filelist.CursorIdx() != prevCursor || m.filelist.CurrentPath() != prevPath {
		m.currentPath = m.filelist.CurrentPath()
		if entry := m.filelist.SelectedEntry(); entry != nil {
			cmds = append(cmds, m.preview.LoadFile(*entry))
			m.stampPreview(entry)
		}
	}

	if ActiveDeletesCount() > 0 && !m.deletesTicking {
		m.deletesTicking = true
		cmds = append(cmds, deleteTick())
	}

	return m, tea.Batch(cmds...)
}

// ── View ───────────────────────────────────────────────────────────────

func (m Model) View() string {
	if !m.ready {
		return "\n  Loading tuiple…"
	}
	if m.showHelp {
		return m.renderHelp()
	}

	breadcrumb := m.renderBreadcrumb()
	sep := theme.Separator.Render(strings.Repeat("─", m.width))

	// Panel dimensions
	sidebarW, filelistW, previewW := m.panelWidths()
	contentH := m.contentHeight()

	// Render panels into fixed-size boxes
	sidebarBox := lipgloss.Place(sidebarW, contentH, lipgloss.Left, lipgloss.Top, m.sidebar.View())
	filelistBox := lipgloss.Place(filelistW, contentH, lipgloss.Left, lipgloss.Top, m.filelist.View())
	previewBox := lipgloss.Place(previewW, contentH, lipgloss.Left, lipgloss.Top, m.preview.View())

	// Vertical separator column
	vsep := m.renderVSep(contentH)

	content := lipgloss.JoinHorizontal(lipgloss.Top,
		sidebarBox, vsep, filelistBox, vsep, previewBox,
	)

	// Search Overlay
	if m.searchOverlay.IsActive() {
		m.searchOverlay.SetSize(m.width/2, contentH)
		overlay := m.searchOverlay.View()
		content = lipgloss.Place(m.width, contentH, lipgloss.Center, lipgloss.Center, overlay)
	}

	// Git Overlay
	if m.gitOverlay.IsActive() {
		m.gitOverlay.SetSize(m.width*4/5, contentH)
		overlay := m.gitOverlay.View()
		content = lipgloss.Place(m.width, contentH, lipgloss.Center, lipgloss.Center, overlay)
	}

	// Branches Popup
	if m.branchesPopup.IsActive() {
		m.branchesPopup.SetSize(m.width*3/5, contentH*3/4)
		overlay := m.branchesPopup.View()
		content = lipgloss.Place(m.width, contentH, lipgloss.Center, lipgloss.Center, overlay)
	}

	// File History Popup (purple border, on top of git overlay)
	if m.fileHistoryPopup.IsActive() {
		m.fileHistoryPopup.SetSize(m.width*4/5, contentH*4/5)
		overlay := m.fileHistoryPopup.View()
		content = lipgloss.Place(m.width, contentH, lipgloss.Center, lipgloss.Center, overlay)
	}

	// Blame Popup (orange border, on top of everything)
	if m.blamePopup.IsActive() {
		m.blamePopup.SetSize(m.width*4/5, contentH*4/5)
		overlay := m.blamePopup.View()
		content = lipgloss.Place(m.width, contentH, lipgloss.Center, lipgloss.Center, overlay)
	}

	// History Popup (read-only viewer over undo/redo stacks)
	if m.historyPopup.IsActive() {
		m.historyPopup.SetSize(m.width*4/5, contentH*4/5)
		overlay := m.historyPopup.View()
		content = lipgloss.Place(m.width, contentH, lipgloss.Center, lipgloss.Center, overlay)
	}

	statusBar := m.renderStatusBar()

	ui := lipgloss.JoinVertical(lipgloss.Left,
		breadcrumb,
		sep,
		content,
		sep,
		statusBar,
	)

	return ui
}

// ── Layout helpers ─────────────────────────────────────────────────────

func (m *Model) cyclePanel(dir int) {
	m.active = panel((int(m.active) + dir + 3) % 3)
}

func (m *Model) updateFocus() {
	m.sidebar.SetFocused(m.active == panelSidebar)
	m.filelist.SetFocused(m.active == panelFileList)
	m.preview.SetFocused(m.active == panelPreview)
}

func (m *Model) updateSizes() {
	sidebarW, filelistW, previewW := m.panelWidths()
	contentH := m.contentHeight()

	m.sidebar.SetSize(sidebarW, contentH)
	m.filelist.SetSize(filelistW, contentH)
	m.preview.SetSize(previewW, contentH)
	m.updateFocus()
}

func (m Model) panelWidths() (int, int, int) {
	available := m.width - 2 // 2 separator columns (│)

	sidebarW := clamp(available*20/100, 14, 28)
	previewW := clamp(available*30/100, 16, available/2)
	filelistW := available - sidebarW - previewW
	if filelistW < 20 {
		filelistW = 20
	}
	return sidebarW, filelistW, previewW
}

func (m Model) contentHeight() int {
	// breadcrumb(1) + sep(1) + content + sep(1) + statusbar(1) = content + 4
	return max(1, m.height-4)
}

// ── File operation handlers ──────────────────────────────────────────────

func (m Model) updateDialog(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.dialogMode = DialogNone
			m.textInput.Blur()
			return m, nil
		}

		if m.dialogMode == DialogGitDiscard {
			switch msg.String() {
			case "y", "Y":
				if m.gitRoot != "" {
					rel := strings.TrimPrefix(m.gitDiscardPath, m.gitRoot+"/")
					if err := git.DiscardFile(m.gitRoot, rel); err != nil {
						m.statusMsg = "discard: " + err.Error()
					} else {
						m.statusMsg = "discarded " + filepath.Base(m.gitDiscardPath)
						m.filelist, _ = m.filelist.Update(filelist.RefreshListMsg{})
						m.refreshGit()
					}
				}
				m.dialogMode = DialogNone
				m.gitDiscardPath = ""
			case "n", "N", "esc":
				m.dialogMode = DialogNone
				m.gitDiscardPath = ""
			}
			return m, nil
		}

		if m.dialogMode == DialogDelete {
			if msg.String() == "y" || msg.String() == "Y" {
				var delErr error
				var items []HistoryItem
				for _, entry := range m.opEntries {
					trashPath, err := SoftDeletePath(entry.Path)
					if err != nil {
						delErr = err
						break
					}
					items = append(items, HistoryItem{Src: entry.Path, Dst: trashPath, IsDir: entry.IsDir})
				}
				m.dialogMode = DialogNone
				if delErr != nil {
					m.statusMsg = "Error: " + delErr.Error()
				} else {
					if len(items) > 0 {
						PushHistory(HistoryEvent{Op: OpDelete, Items: items})
					}
					m.statusMsg = fmt.Sprintf("Deleted %d item(s)", len(m.opEntries))
					m.filelist, _ = m.filelist.Update(filelist.RefreshListMsg{})
					m.filelist, _ = m.filelist.Update(filelist.ClearSelectionMsg{})
					var cmds []tea.Cmd
					if entry := m.filelist.SelectedEntry(); entry != nil {
						cmds = append(cmds, m.preview.LoadFile(*entry))
						m.stampPreview(entry)
					}
					if ActiveDeletesCount() > 0 && !m.deletesTicking {
						m.deletesTicking = true
						cmds = append(cmds, deleteTick())
					}
					return m, tea.Batch(cmds...)
				}
				return m, nil
			} else if msg.String() == "n" || msg.String() == "N" {
				m.dialogMode = DialogNone
				return m, nil
			}
			return m, nil
		}

		if msg.String() == "tab" && m.dialogMode == DialogGoToPath {
			typed := m.textInput.Value()
			completed := completePath(typed, m.currentPath)
			if completed != typed {
				m.textInput.SetValue(completed)
				m.textInput.CursorEnd()
			}
			return m, nil
		}

		if msg.String() == "enter" && m.dialogMode == DialogGoToPath {
			val := strings.TrimSpace(m.textInput.Value())
			m.dialogMode = DialogNone
			m.textInput.Blur()
			if val == "" {
				return m, nil
			}
			target := expandPath(val, m.currentPath)
			info, err := os.Stat(target)
			if err != nil {
				m.statusMsg = "Cannot open: " + err.Error()
				return m, nil
			}
			var navDir, selectName string
			if info.IsDir() {
				navDir = target
			} else {
				navDir = filepath.Dir(target)
				selectName = filepath.Base(target)
			}
			m.currentPath = navDir
			m.updateLastDirMod(navDir)
			var navCmd tea.Cmd
			m.filelist, navCmd = m.filelist.NavigateTo(navDir)
			if selectName != "" {
				m.filelist = m.filelist.SelectByName(selectName)
			}
			m.refreshGit()
			cmds := []tea.Cmd{navCmd}
			if entry := m.filelist.SelectedEntry(); entry != nil {
				cmds = append(cmds, m.preview.LoadFile(*entry))
				m.stampPreview(entry)
			}
			return m, tea.Batch(cmds...)
		}

		if msg.String() == "enter" {
			val := m.textInput.Value()
			if val != "" {
				var err error
				switch m.dialogMode {
				case DialogRename:
					dst := filepath.Join(m.currentPath, val)
					err = filesystem.Move(m.opEntries[0].Path, dst)
					if err == nil {
						PushHistory(HistoryEvent{
							Op:    OpRename,
							Items: []HistoryItem{{Src: m.opEntries[0].Path, Dst: dst, IsDir: m.opEntries[0].IsDir}},
						})
					}
				case DialogNewFile:
					dst := filepath.Join(m.currentPath, val)
					err = filesystem.CreateFile(dst)
					if err == nil {
						PushHistory(HistoryEvent{
							Op:    OpCreateFile,
							Items: []HistoryItem{{Dst: dst, IsDir: false}},
						})
					}
				case DialogNewDir:
					dst := filepath.Join(m.currentPath, val)
					err = filesystem.CreateDir(dst)
					if err == nil {
						PushHistory(HistoryEvent{
							Op:    OpCreateDir,
							Items: []HistoryItem{{Dst: dst, IsDir: true}},
						})
					}
				}
				if err != nil {
					m.statusMsg = "Error: " + err.Error()
				} else {
					m.statusMsg = "Done."
					m.filelist, _ = m.filelist.Update(filelist.RefreshListMsg{})
				}
			}
			m.dialogMode = DialogNone
			m.textInput.Blur()
			return m, nil
		}

		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

// expandPath turns a user-typed path into something os.Stat can use:
//   - "~" or "~/foo" → home directory (~ alone, ~/ prefix)
//   - "$VAR/foo"     → expanded via os.ExpandEnv
//   - relative paths → joined against the current directory
//
// Trailing whitespace is already stripped by the caller.
func expandPath(p, cwd string) string {
	p = os.ExpandEnv(p)
	if p == "~" {
		return filesystem.HomeDir()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(filesystem.HomeDir(), p[2:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p)
}

// completePath performs shell-style Tab completion on a path being typed
// in the Go-to-path dialog. It splits typed into the directory part the
// user has already entered and the prefix that should be matched against
// the contents of that directory.
//
//   - If exactly one entry matches, that entry's name (plus a trailing
//     "/" for directories) replaces the prefix.
//   - If several entries share a longer common prefix than what the user
//     typed, the prefix is extended to that common stem.
//   - Otherwise (no matches, or already at the common stem) the input is
//     left unchanged.
//
// The "user form" of the path is preserved: typing "~/Doc" + Tab yields
// "~/Documents/" rather than the fully-expanded "/Users/<you>/Documents/".
func completePath(typed, cwd string) string {
	if typed == "" {
		return typed
	}

	// Split into the part the user already wrote (kept verbatim) and the
	// last segment that we are completing.
	var userDir, userPrefix string
	if idx := strings.LastIndex(typed, "/"); idx >= 0 {
		userDir = typed[:idx+1]
		userPrefix = typed[idx+1:]
	} else {
		userDir = ""
		userPrefix = typed
	}

	// Figure out which real directory to scan.
	resolveDir := cwd
	if userDir != "" {
		resolveDir = expandPath(userDir, cwd)
	}

	entries, err := os.ReadDir(resolveDir)
	if err != nil {
		return typed
	}

	// Only surface hidden entries when the user has explicitly typed a
	// leading dot — matches the behaviour of bash and zsh's globbing.
	showHidden := strings.HasPrefix(userPrefix, ".")

	// Match case-insensitively (matches oh-my-zsh's default `case-sensitive
	// "false"` and Finder behaviour). The replacement always uses the real
	// on-disk casing so "doc" + Tab fills in "Documents", not "documents".
	prefixLower := strings.ToLower(userPrefix)
	type match struct {
		name  string
		isDir bool
	}
	var matches []match
	for _, e := range entries {
		name := e.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(name), prefixLower) {
			matches = append(matches, match{name: name, isDir: e.IsDir()})
		}
	}

	if len(matches) == 0 {
		return typed
	}

	if len(matches) == 1 {
		suffix := ""
		if matches[0].isDir {
			suffix = "/"
		}
		return userDir + matches[0].name + suffix
	}

	// Multiple matches → extend prefix to the longest common stem,
	// comparing case-insensitively but keeping the case of the first
	// match for the visible result.
	common := matches[0].name
	for _, mt := range matches[1:] {
		i := 0
		for i < len(common) && i < len(mt.name) &&
			strings.EqualFold(common[i:i+1], mt.name[i:i+1]) {
			i++
		}
		common = common[:i]
	}
	if len(common) > len(userPrefix) {
		return userDir + common
	}
	return typed
}

// uniqueDst returns dst unchanged when the path does not exist.
// If dst already exists it inserts _1, _2, … before the extension (for
// files) or at the end (for directories) until an unused path is found.
func uniqueDst(dst string, isDir bool) string {
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		return dst
	}
	base := dst
	ext := ""
	if !isDir {
		ext = filepath.Ext(dst)
		base = strings.TrimSuffix(dst, ext)
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (m Model) handlePaste(items []clipboard.Item, op clipboard.OpType) (tea.Model, tea.Cmd) {
	var itemsDone []HistoryItem
	var opType OpType
	if op == clipboard.OpCut {
		opType = OpMove
	} else {
		opType = OpCopy
	}

	for _, item := range items {
		dst := filepath.Join(m.currentPath, item.Entry.Name)
		if op == clipboard.OpCopy {
			dst = uniqueDst(dst, item.Entry.IsDir)
		}
		var err error
		if op == clipboard.OpCut {
			err = filesystem.Move(item.Entry.Path, dst)
		} else {
			if item.Entry.IsDir {
				err = filesystem.CopyDir(item.Entry.Path, dst)
			} else {
				err = filesystem.CopyFile(item.Entry.Path, dst)
			}
		}
		if err != nil {
			m.statusMsg = "Paste error: " + err.Error()
			return m, nil
		}
		itemsDone = append(itemsDone, HistoryItem{Src: item.Entry.Path, Dst: dst, IsDir: item.Entry.IsDir})
	}
	if op == clipboard.OpCut {
		clipboard.Clear()
	}
	if len(itemsDone) > 0 {
		PushHistory(HistoryEvent{Op: opType, Items: itemsDone})
	}
	m.statusMsg = "Pasted successfully"
	m.filelist, _ = m.filelist.Update(filelist.RefreshListMsg{})
	return m, nil
}

// ── Breadcrumb ─────────────────────────────────────────────────────────

func (m Model) renderBreadcrumb() string {
	short := filesystem.ShortenPath(m.currentPath)
	parts := strings.Split(short, "/")

	var rendered []string
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == len(parts)-1 {
			rendered = append(rendered, theme.BreadcrumbActive.Render(part))
		} else {
			rendered = append(rendered, theme.BreadcrumbNormal.Render(part))
		}
	}

	if len(rendered) == 0 {
		rendered = append(rendered, theme.BreadcrumbActive.Render("/"))
	}

	path := strings.Join(rendered, theme.BreadcrumbSep.Render(" / "))
	return " " + path
}

// ── Vertical separator ────────────────────────────────────────────────

func (m Model) renderVSep(h int) string {
	ch := theme.Separator.Render("│")
	lines := make([]string, h)
	for i := range lines {
		lines[i] = ch
	}
	return strings.Join(lines, "\n")
}

// ── Status bar ─────────────────────────────────────────────────────────

func (m Model) renderStatusBar() string {
	if m.dialogMode == DialogGitDiscard {
		return theme.StatusBar.Width(m.width).Render(
			fmt.Sprintf(" Discard changes to %s? (y/N) ", filepath.Base(m.gitDiscardPath)))
	}
	if m.dialogMode == DialogDelete {
		title := m.opEntries[0].Name
		if len(m.opEntries) > 1 {
			title = fmt.Sprintf("%d items", len(m.opEntries))
		}
		return theme.StatusBar.Width(m.width).Render(fmt.Sprintf(" Delete %s? (y/N) ", title))
	} else if m.dialogMode != DialogNone {
		prefix := " Rename: "
		switch m.dialogMode {
		case DialogNewFile:
			prefix = " New File: "
		case DialogNewDir:
			prefix = " New Dir: "
		case DialogGoToPath:
			prefix = " Go to: "
		}
		return theme.StatusBar.Width(m.width).Render(prefix + m.textInput.View())
	}

	leftStr := " " + filesystem.ShortenPath(m.currentPath)
	if m.statusMsg != "" {
		leftStr += "  [" + m.statusMsg + "]"
	}

	delCount := ActiveDeletesCount()
	if delCount > 0 {
		rem := NextDeleteRemaining()
		leftStr += fmt.Sprintf("  [%d pending (%ds)]", delCount, int(rem.Seconds()))
	}

	left := theme.StatusPath.Render(leftStr)
	if m.gitBranch != "" {
		branchStyle := lipgloss.NewStyle().Foreground(theme.AccentGreen).Bold(true)
		left += "  " + branchStyle.Render(" ["+m.gitBranch+"]")
	}

	entryCount := len(m.filelist.Entries())
	info := fmt.Sprintf("%d items", entryCount)

	if entry := m.filelist.SelectedEntry(); entry != nil {
		info += " · " + entry.Name
		if !entry.IsDir {
			info += " · " + filesystem.FormatSize(entry.Size)
		}
	}

	right := theme.StatusInfo.Render(info + " ")

	gap := max(0, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

// ── Help screen ────────────────────────────────────────────────────────

func (m Model) renderHelp() string {
	type helpItem struct{ key, desc string }
	type helpCat struct {
		title string
		items []helpItem
	}

	// ── Tab 0: System ────────────────────────────────────────────────
	sysLeft := []helpCat{
		{"Navigation", []helpItem{
			{"↑ ↓  /  k j", "Move up / down"},
			{"Enter / → / l", "Enter dir / open file"},
			{"Backspace / ← / h", "Go back to parent"},
			{"g / G", "Go to top / bottom"},
			{"Ctrl+U / Ctrl+D", "Page up / down"},
			{"~", "Go to home directory"},
			{":", "Go to path"},
			{"Tab / Shift+Tab", "Switch active panel"},
		}},
		{"File Operations", []helpItem{
			{"Space", "Toggle selection (multi-select)"},
			{"Esc", "Clear all selections"},
			{"c / x / p", "Copy / Cut / Paste"},
			{"d", "Delete (to trash)"},
			{"r", "Rename"},
			{"n / N", "New File / New Directory"},
			{"u / U", "Undo / Redo"},
			{"Y", "Show action history (read-only)"},
		}},
	}
	sysRight := []helpCat{
		{"Search & Bookmarks", []helpItem{
			{"f", "Search file by name (fuzzy)"},
			{"F", "Search in file contents"},
			{"/", "Live list filter"},
			{"'", "(Un)Bookmark to Favorites"},
		}},
		{"Audio Player", []helpItem{
			{"l / Enter", "Play / Pause audio file"},
			{"- / =", "Seek ±5 seconds"},
			{"_ / +", "Seek ±30 seconds"},
		}},
		{"System & Options", []helpItem{
			{".", "Toggle hidden files"},
			{"o n / o s / o d", "Sort by: Name / Size / Date"},
			{"S (Shift+S)", "Open subshell here"},
			{"?", "Toggle this help screen"},
			{"q / Ctrl+C", "Quit"},
		}},
	}

	// ── Tab 1: Git ───────────────────────────────────────────────────
	gitLeft := []helpCat{
		{"Git panel  (Ctrl+G)", []helpItem{
			{"Tab / Shift+Tab", "Switch tab (Commit / Log / Stashes)"},
			{"1 / 2 / 3", "Jump to tab directly"},
			{"Esc", "Close panel"},
		}},
		{"Branches popup  (b)", []helpItem{
			{"↑ ↓  /  k j", "Navigate branches"},
			{"Enter / l", "Checkout"},
			{"n", "New branch from HEAD"},
			{"r / d / D", "Rename / Delete safe / Force-delete"},
			{"m / R", "Merge into current / Rebase onto"},
			{"P / p / F", "Push / Pull / Fetch"},
			{"/  ·  Ctrl+R", "Filter  ·  Reload"},
		}},
		{"Commit tab", []helpItem{
			{"↑ ↓  /  k j", "Navigate changed files"},
			{"Space", "Toggle stage for file"},
			{"a / A", "Stage file / Stage all"},
			{"R", "Unstage file"},
			{"D", "Discard working-tree changes (y/n)"},
			{"I", "Add file to .gitignore"},
			{"H / L", "File history / Blame popup"},
			{"i", "Focus commit message (Esc to leave)"},
			{"s  ·  Ctrl+S", "Stash  ·  Commit"},
		}},
	}
	gitRight := []helpCat{
		{"Log tab", []helpItem{
			{"↑ ↓  /  k j", "Navigate commits"},
			{"/", "Filter by subject / author / SHA"},
			{"Esc", "Clear active filter"},
			{"r", "Reset HEAD here (soft/mixed/hard)"},
			{"c / v", "Cherry-pick / Revert"},
			{"b", "Create branch from commit"},
			{"Ctrl+R", "Reload log"},
		}},
		{"Stashes tab", []helpItem{
			{"↑ ↓  /  k j", "Navigate stashes"},
			{"a / p / D", "Apply / Pop / Drop"},
			{"r", "Reload"},
		}},
		{"Diff / detail scrolling", []helpItem{
			{"Ctrl+D / PgDn", "Half-page down"},
			{"Ctrl+U / PgUp", "Half-page up"},
			{"Home / End", "Top / Bottom"},
		}},
		{"Git file ops  (in filelist)", []helpItem{
			{"a / R", "Stage / Unstage"},
			{"D", "Discard changes (y/n)"},
			{"i", "Add to .gitignore"},
			{"H / L", "File history / Blame"},
		}},
	}

	renderCol := func(cats []helpCat, keyW int) []string {
		var out []string
		for _, c := range cats {
			out = append(out, theme.ListHeader.Render("  "+c.title))
			for _, it := range c.items {
				key := theme.HelpKey.Width(keyW).Render("    " + it.key)
				desc := theme.HelpDesc.Render(it.desc)
				out = append(out, key+" "+desc)
			}
			out = append(out, "")
		}
		return out
	}

	var leftCats, rightCats []helpCat
	if m.helpTabIdx == 0 {
		leftCats, rightCats = sysLeft, sysRight
	} else {
		leftCats, rightCats = gitLeft, gitRight
	}

	leftLines := renderCol(leftCats, 22)
	rightLines := renderCol(rightCats, 22)
	for len(leftLines) < len(rightLines) {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < len(leftLines) {
		rightLines = append(rightLines, "")
	}

	const leftColW = 58
	leftCol := lipgloss.NewStyle().Width(leftColW).Render(strings.Join(leftLines, "\n"))
	rightCol := strings.Join(rightLines, "\n")
	cols := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, rightCol)

	// Tab bar (same style as the git overlay)
	tabTitles := []string{"System", "Git"}
	activeStyle := lipgloss.NewStyle().Foreground(theme.AccentGreen).Bold(true).Underline(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(theme.FgDimColor)
	dimDot := theme.Dim.Render("·")
	var tabParts []string
	for i, t := range tabTitles {
		label := " " + t + " "
		if i == m.helpTabIdx {
			tabParts = append(tabParts, activeStyle.Render(label))
		} else {
			tabParts = append(tabParts, inactiveStyle.Render(label))
		}
	}
	tabBar := " " + strings.Join(tabParts, dimDot) +
		theme.Dim.Render("    Tab / Shift+Tab switch · 1 / 2 jump · ? or Esc close")

	var out []string
	out = append(out, "")
	out = append(out, theme.PreviewTitle.Render("  ⌨  Tuiple — Keyboard Shortcuts"))
	out = append(out, tabBar)
	out = append(out, "")
	out = append(out, cols)

	return strings.Join(out, "\n")
}

// ── Utilities ──────────────────────────────────────────────────────────

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
