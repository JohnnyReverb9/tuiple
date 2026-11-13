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

	"github.com/JohnnyReverb9/tuiple/clipboard"
	"github.com/JohnnyReverb9/tuiple/components/filelist"
	"github.com/JohnnyReverb9/tuiple/components/git"
	"github.com/JohnnyReverb9/tuiple/components/picker"
	"github.com/JohnnyReverb9/tuiple/components/preview"
	"github.com/JohnnyReverb9/tuiple/components/preview/mediarender"
	"github.com/JohnnyReverb9/tuiple/components/search"
	"github.com/JohnnyReverb9/tuiple/components/sidebar"
	"github.com/JohnnyReverb9/tuiple/config"
	"github.com/JohnnyReverb9/tuiple/filesystem"
	"github.com/JohnnyReverb9/tuiple/keys"
	"github.com/JohnnyReverb9/tuiple/theme"
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
	if !config.Get().Git.Enabled {
		m.gitRoot = ""
		m.gitBranch = ""
		m.gitFileStat = nil
		m.gitDirHas = nil
		m.filelist.SetGitState(nil, nil)
		return
	}
	root, err := git.FindRoot(m.currentPath)
	if err != nil {
		m.gitRoot = ""
		m.gitBranch = ""
		m.gitAhead, m.gitBehind = 0, 0
		m.gitFileStat = nil
		m.gitDirHas = nil
		m.filelist.SetGitState(nil, nil)
		return
	}
	prevRoot, prevBranch := m.gitRoot, m.gitBranch
	m.gitRoot = root
	if br, err := git.CurrentBranch(root); err == nil {
		m.gitBranch = br
	} else {
		m.gitBranch = ""
	}
	// Arriving in a repository is arriving at a project, and projects
	// are what the recent list is for.
	if root != prevRoot {
		TouchRecent(root, true)
	}
	// How far the branch is from its upstream costs two more git calls
	// and only changes on a commit, fetch or push — so it is re-read when
	// the branch changes and otherwise every few seconds, not every poll.
	if root != prevRoot || m.gitBranch != prevBranch || time.Since(m.lastTracking) > 5*time.Second {
		m.lastTracking = time.Now()
		if info, err := git.Tracking(root); err == nil {
			m.gitAhead, m.gitBehind = info.Ahead, info.Behind
		}
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
	filelist filelist.Model // the active tab's list — see tabs.go
	preview  preview.Model

	// tabs holds one file list per tab. The entry at tabIdx is stale
	// while that tab is in front: its live copy is filelist above.
	tabs       []filelist.Model
	tabIdx     int
	closedTabs []string // directories of closed tabs, for reopening

	picker picker.Model // command palette / recent places
	// infoBox is the rendered "Get info" popup; empty when closed.
	infoBox string

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
	settingsPopup    SettingsPopup
	awaitingSort     bool

	gitRoot        string
	gitBranch      string
	gitAhead       int // commits the branch is ahead of its upstream
	gitBehind      int
	lastTracking   time.Time
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
	lastGitPoll    time.Time

	// exitCwdFile is where the directory we quit in gets written, so a
	// shell wrapper can cd there. Empty unless --cwd-file was passed.
	exitCwdFile string

	width  int
	height int

	showHelp   bool
	helpTabIdx int // 0 = Navigate, 1 = Files, 2 = Git
	ready      bool

	// cyrillicActive remembers whether the last letter typed came from a
	// Cyrillic layout, which is what lets normalizeKey resolve the two
	// punctuation keys whose Cyrillic output is itself ASCII.
	cyrillicActive bool

	lastDirMod time.Time

	// statusAt is when statusMsg last changed; a message that has been
	// read does not need to stay on screen until the next one.
	statusAt time.Time

	// Free space on the current volume, for the status bar.
	diskFree   int64
	diskFreeOK bool

	// sessionDirty is set when the open tabs changed and cleared when
	// they have been written to disk.
	sessionDirty bool
	// sessionOff is set for a run started on an explicit path. Such a
	// run is a one-off look at one directory; letting it overwrite the
	// saved session would throw away the tabs of the real one.
	sessionOff bool

	// lastClick is the previous mouse press on a list row, for telling a
	// double click from two single ones.
	lastClick click
}

// New creates the application model with a single tab.
func New(startDir string) Model {
	return NewWithTabs([]string{startDir}, 0)
}

// NewWithTabs creates the application model with one tab per directory
// and the given one in front — how a saved session comes back.
func NewWithTabs(dirs []string, active int) Model {
	if len(dirs) == 0 {
		dirs = []string{filesystem.HomeDir()}
	}
	if active < 0 || active >= len(dirs) {
		active = 0
	}

	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.CharLimit = 255
	ti.Width = 40
	ti.PlaceholderStyle = theme.Dim
	ti.TextStyle = theme.Normal

	tabs := make([]filelist.Model, len(dirs))
	for i, dir := range dirs {
		tabs[i] = filelist.New(dir)
	}
	startDir := dirs[active]

	var lastMod time.Time
	if info, err := os.Stat(startDir); err == nil {
		lastMod = info.ModTime()
	}

	m := Model{
		sidebar:          sidebar.New(),
		filelist:         tabs[active],
		tabs:             tabs,
		tabIdx:           active,
		preview:          preview.New(),
		picker:           picker.New(),
		textInput:        ti,
		searchOverlay:    search.New(),
		gitOverlay:       git.New(),
		branchesPopup:    git.NewBranches(),
		fileHistoryPopup: git.NewFileHistory(),
		blamePopup:       git.NewBlame(),
		historyPopup:     NewHistoryPopup(),
		settingsPopup:    NewSettingsPopup(),
		active:           panelFileList,
		currentPath:      startDir,
		lastDirMod:       lastMod,
	}
	m.sidebar.SetCurrent(startDir)
	m.refreshDisk()
	return m
}

// WithExitCwdFile arranges for the directory tuiple exits in to be
// written to path. A process cannot change its parent shell's directory,
// so the shell wrapper printed by --shell-init reads this file instead.
func (m Model) WithExitCwdFile(path string) Model {
	m.exitCwdFile = path
	return m
}

// WithoutSession stops this run from saving its tabs as the session to
// restore next time.
func (m Model) WithoutSession() Model {
	m.sessionOff = true
	return m
}

// writeExitCwd records the current directory on the way out. Failures
// are deliberately silent: the file is a convenience for the wrapper,
// and an unwritable temp path is not worth an error dialog on quit.
func (m Model) writeExitCwd() {
	if m.exitCwdFile == "" {
		return
	}
	_ = os.WriteFile(m.exitCwdFile, []byte(m.currentPath+"\n"), 0600)
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

// statusLifetime is how long a status message stays up. Long enough to
// read, short enough that the bar is not still announcing a copy made
// ten minutes ago.
const statusLifetime = 6 * time.Second

// Update is the Bubble Tea entry point. The work happens in update; this
// wrapper does the two things that must hold after any message at all,
// whichever of the many return paths handled it: the status message is
// timestamped when it changes, and the sidebar knows where the list is.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prevStatus := m.statusMsg
	next, cmd := m.update(msg)
	model, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	if model.statusMsg != prevStatus {
		model.statusAt = time.Now()
	}
	model.sidebar.SetCurrent(model.currentPath)
	return model, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// A Cmd shortcut sent by the terminal as an escape sequence. It asks
	// for an action directly, so it only applies while the panels have
	// the keyboard — a popup or a prompt is reading its own keys.
	if chord, ok := chordFromMsg(msg); ok {
		if m.modalActive() || m.showHelp {
			return m, nil
		}
		if action, bound := keys.Match(keys.ScopeGlobal, chord); bound {
			next, cmd, _ := m.runGlobal(action, chord)
			return next, cmd
		}
		if action, bound := keys.Match(keys.ScopeList, chord); bound {
			return m.runList(action)
		}
		return m, nil
	}

	// Translate Cyrillic keystrokes into their QWERTY equivalents before
	// anything gets to look at them — unless some part of the UI is
	// reading text, in which case the keystrokes are content, not
	// commands. See app/keylayout.go.
	if keyMsg, ok := msg.(tea.KeyMsg); ok && !m.acceptsText() {
		msg = m.normalizeKey(keyMsg)
	}

	// Settings edits arrive as messages so the components that cache
	// config values are refreshed in exactly one place.
	if _, ok := msg.(settingsChangedMsg); ok {
		cmd := m.applySettings()
		return m, cmd
	}
	if _, ok := msg.(settingsReloadedMsg); ok {
		if err := config.Load(); err != nil {
			// The popup is still on screen after the editor exits, so
			// the complaint belongs there rather than in the status bar
			// hiding behind it.
			m.settingsPopup.setStatus("could not read config: "+err.Error(), false)
			m.statusMsg = "config: " + err.Error()
		} else {
			m.settingsPopup.setStatus("reloaded "+config.Path(), true)
		}
		cmd := m.applySettings()
		return m, cmd
	}

	if _, ok := msg.(deleteTickMsg); ok {
		if ActiveDeletesCount() > 0 {
			return m, deleteTick()
		}
		m.deletesTicking = false
		return m, nil
	}

	if _, ok := msg.(dirPollMsg); ok {
		var pollCmds []tea.Cmd
		// The once-a-second tick doubles as the housekeeping clock.
		if m.statusMsg != "" && !m.awaitingSort && time.Since(m.statusAt) > statusLifetime {
			m.statusMsg = ""
		}
		if m.sessionDirty {
			m.saveSession()
		}
		m.refreshDisk()
		if info, err := os.Stat(m.currentPath); err == nil {
			if info.ModTime().After(m.lastDirMod) {
				m.lastDirMod = info.ModTime()
				pollCmds = append(pollCmds, m.listCmd(filelist.RefreshListMsg{}))
			}
		}
		// The directory stat above is cheap and runs every tick; the git
		// probe forks three commands, so it only runs as often as the
		// configured refresh interval allows.
		interval := time.Duration(config.Get().Git.PollSeconds) * time.Second
		if time.Since(m.lastGitPoll) >= interval {
			m.lastGitPoll = time.Now()
			m.refreshGit()
		}
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
		pollCmds = append(pollCmds, dirPollCmd(), previewCmd)
		return m, tea.Batch(pollCmds...)
	}

	if _, ok := msg.(git.BranchesChangedMsg); ok {
		m.refreshGit()
		return m, nil
	}

	// ── Info box: any key closes it ────────────────────────────────
	if m.infoBox != "" {
		switch msg.(type) {
		case tea.KeyMsg:
			m.infoBox = ""
			return m, nil
		case tea.MouseMsg:
			return m, nil
		}
	}

	// ── Intercept the picker (palette / recent) ────────────────────
	if m.picker.IsActive() {
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		return m, cmd
	}
	if chosen, ok := msg.(picker.ChosenMsg); ok {
		return m.picked(chosen)
	}

	// ── Intercept Settings Popup ───────────────────────────────────
	if m.settingsPopup.IsActive() {
		var cmd tea.Cmd
		m.settingsPopup, cmd = m.settingsPopup.Update(msg)
		return m, cmd
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
			var sortCmd tea.Cmd
			switch keyMsg.String() {
			case "n":
				sortCmd = m.listCmd(filelist.SortListMsg{Mode: filesystem.SortByName})
				m.statusMsg = "Sorted by name"
			case "s":
				sortCmd = m.listCmd(filelist.SortListMsg{Mode: filesystem.SortBySize})
				m.statusMsg = "Sorted by size"
			case "d":
				sortCmd = m.listCmd(filelist.SortListMsg{Mode: filesystem.SortByDate})
				m.statusMsg = "Sorted by date"
			default:
				m.statusMsg = "Sort cancelled"
			}
			return m, sortCmd
		}
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	// ── Mouse ──────────────────────────────────────────────────────
	case tea.MouseMsg:
		if m.showHelp {
			return m, nil
		}
		return m.handleMouse(msg)

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
				m.helpTabIdx = (m.helpTabIdx + 1) % helpTabCount
			case "shift+tab", "left", "h":
				m.helpTabIdx = (m.helpTabIdx + helpTabCount - 1) % helpTabCount
			case "1", "2", "3":
				m.helpTabIdx = int(msg.String()[0] - '1')
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
		// Global shortcuts. The keymap lives in package keys, so a
		// binding is written down once and both the help screen and the
		// user's overrides read the same table.
		if action, bound := keys.Match(keys.ScopeGlobal, msg.String()); bound {
			if next, cmd, handled := m.runGlobal(action, msg.String()); handled {
				return next, cmd
			}
		}

	// ── Directory contents arrived (reads are asynchronous) ────────
	case filelist.EntriesLoadedMsg:
		// A read started by a tab that is now in the background belongs
		// to that tab, not to whichever list is on screen.
		if msg.ListID != m.filelist.ID() {
			for i := range m.tabs {
				if i != m.tabIdx && m.tabs[i].ID() == msg.ListID {
					m.tabs[i], _ = m.tabs[i].Update(msg)
				}
			}
			return m, nil
		}
		cmds = append(cmds, m.listCmd(msg))
		m.currentPath = m.filelist.CurrentPath()
		// Follow the selection with the preview, but only when it
		// actually moved: this message also arrives on every background
		// refresh, and re-rendering an image once a second would be
		// both wasteful and visibly flickery.
		if entry := m.filelist.SelectedEntry(); entry != nil {
			if m.preview.Path() != entry.Path {
				cmds = append(cmds, m.preview.LoadFile(*entry))
				m.stampPreview(entry)
			}
		} else if msg.Err == nil && len(msg.Entries) == 0 {
			// Nothing here to describe; do not keep describing the file
			// the cursor was on a directory ago.
			m.preview.Clear()
			m.stampPreview(nil)
		}
		return m, tea.Batch(cmds...)

	// ── Tabs and small file operations asked for by the list ───────
	case filelist.InfoRequestMsg:
		m.infoBox = fileInfoBox(msg.Path, m.gitFileStat[msg.Path], m.width-4)
		return m, nil
	case filelist.OpenInTabMsg:
		return m.openTab(msg.Path, "")
	case filelist.DuplicateRequestMsg:
		return m.duplicate(msg.Entries)
	case filelist.CopyPathMsg:
		return m.copyPaths(msg.Paths)
	case filelist.OpenExternalMsg:
		return m.openExternal(msg.Path, msg.Reveal)

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
		if !config.Get().Delete.Confirm {
			return m.performDelete(msg.Entries)
		}
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
		ext := strings.ToLower(filepath.Ext(msg.Path))
		cfg := config.Get()
		TouchRecent(msg.Path, false)

		// Audio already loaded in the preview toggles playback instead of
		// opening — unless the user has pointed audio at their own
		// player, in which case the built-in one is what they replaced.
		if m.preview.IsAudio() && m.preview.Path() == msg.Path &&
			cfg.Handler(config.HandlerAudio) == "" && cfg.RuleFor(ext) == nil {
			cmd := m.preview.ToggleAudio()
			return m, cmd
		}

		// Three layers, most specific first: a rule for this exact
		// extension, then the handler configured for the kind of file it
		// is, then the built-in behaviour below.
		if rule := cfg.RuleFor(ext); rule != nil {
			return m, openWithRule(*rule, msg.Path)
		}
		if cmd := cfg.Handler(handlerKindFor(ext)); cmd != "" {
			return m, openWithRule(config.OpenRule{
				Command:  cmd,
				Terminal: !config.HandlerIsGUI(cmd),
			}, msg.Path)
		}

		kind := mediarender.Classify(ext)

		// Open images and videos in terminal using 'chafa'
		if kind == mediarender.KindImage || kind == mediarender.KindVideo {
			if _, err := exec.LookPath("chafa"); err == nil {
				// Clear screen, show image in high quality, wait for any key, then clear again.
				// This prevents image artifacts from remaining in the terminal buffer.
				//
				// The path goes through shellQuote, not %q: Go's quoting
				// produces double quotes, and inside those sh still
				// expands $(...), backticks and $VAR — so a file named
				// "$(rm -rf ~).png" would run when opened.
				shCmd := fmt.Sprintf("clear; chafa %s; echo; echo '  Press any key to return...'; stty raw -echo; dd bs=1 count=1 2>/dev/null; stty -raw echo; clear", shellQuote(msg.Path))
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
				// Extract PDF text and read it with 'less' as fallback.
				// Single-quoted via shellQuote for the same reason as the
				// chafa call above.
				shCmd := fmt.Sprintf("pdftotext %s - | less -r", shellQuote(msg.Path))
				cmd := exec.Command("sh", "-c", shCmd)
				return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
					return filelist.RefreshListMsg{}
				})
			}
		}

		cmd := exec.Command(cfg.Editor(), msg.Path)
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return filelist.RefreshListMsg{}
		})
	case filelist.BulkRenameRequestMsg:
		return m, startBulkRename(msg.Dir, msg.Names)

	case bulkRenameDoneMsg:
		return m.finishBulkRename(msg)

	case filelist.DirSizeRequestMsg:
		for _, p := range msg.Paths {
			cmds = append(cmds, dirSizeCmd(p))
		}
		if len(msg.Paths) == 1 {
			m.statusMsg = "Measuring " + filepath.Base(msg.Paths[0]) + "…"
		} else {
			m.statusMsg = fmt.Sprintf("Measuring %d directories…", len(msg.Paths))
		}
		return m, tea.Batch(cmds...)

	case dirSizeResultMsg:
		m.filelist, _ = m.filelist.Update(filelist.SetDirSizeMsg{Path: msg.Path, Size: msg.Size})
		m.statusMsg = fmt.Sprintf("%s: %s", filepath.Base(msg.Path), filesystem.FormatSize(msg.Size))
		return m, nil

	case filelist.ArchiveRequestMsg:
		dst := uniqueDst(filepath.Join(msg.Dir, archiveName(msg.Dir, msg.Names)), false)
		m.statusMsg = "Archiving " + filepath.Base(dst) + "…"
		return m, createArchive(msg.Dir, dst, msg.Names)

	case filelist.ExtractRequestMsg:
		if !IsArchive(msg.Path) {
			m.statusMsg = "Not an archive: " + filepath.Base(msg.Path)
			return m, nil
		}
		destDir := uniqueDst(filepath.Join(filepath.Dir(msg.Path),
			archiveBaseName(filepath.Base(msg.Path))), true)
		if err := filesystem.CreateDir(destDir); err != nil {
			m.statusMsg = "extract: " + err.Error()
			return m, nil
		}
		m.statusMsg = "Extracting " + filepath.Base(msg.Path) + "…"
		return m, extractArchive(msg.Path, destDir)

	case archiveDoneMsg:
		m.statusMsg = msg.status()
		if msg.err == nil {
			// Both directions create something new, so undo just has to
			// remove it again.
			op := OpCreateFile
			if msg.isDir {
				op = OpCreateDir
			}
			PushHistory(HistoryEvent{Op: op, Items: []HistoryItem{{Dst: msg.path, IsDir: msg.isDir}}})
		} else if msg.isDir {
			// A failed extraction leaves an empty directory behind.
			_ = os.Remove(msg.path)
		}
		return m, m.listCmd(filelist.RefreshListMsg{})

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
		TouchRecent(msg.SelectedPath, false)
		next, cmd := m.goTo(filepath.Dir(msg.SelectedPath), filepath.Base(msg.SelectedPath))
		if msg.Reveal {
			return next, cmd
		}
		// Enter opens what was found: a text match at its line, a file
		// through the same route as Enter on its row.
		open := func() tea.Msg { return filelist.OpenFileRequestMsg{Path: msg.SelectedPath} }
		if msg.Mode == search.ModeContentSearch && msg.LineNum > 0 {
			editor := exec.Command(config.Get().Editor(), fmt.Sprintf("+%d", msg.LineNum), msg.SelectedPath)
			open = tea.ExecProcess(editor, func(error) tea.Msg { return filelist.RefreshListMsg{} })
		}
		return next, tea.Sequence(cmd, open)

	// ── Sidebar navigation ─────────────────────────────────────────
	case sidebar.NavigateMsg:
		TouchRecent(msg.Path, true)
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
		// Read the path from the list rather than the message: with tabs
		// the message may be about a list that is no longer in front.
		m.currentPath = m.filelist.CurrentPath()
		m.updateLastDirMod(m.currentPath)
		m.refreshGit()
		m.refreshDisk()
		m.sessionDirty = true
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

	return m.afterListInput(prevCursor, prevPath, cmds)
}

// ── View ───────────────────────────────────────────────────────────────

func (m Model) View() string {
	if !m.ready {
		return "\n  Loading tuiple..."
	}
	if m.showHelp {
		return m.renderHelp()
	}

	breadcrumb := m.renderBreadcrumb()
	sep := theme.Separator.Render(strings.Repeat("─", m.width))

	// Panel dimensions
	sidebarW, filelistW, previewW := m.panelWidths()
	contentH := m.contentHeight()

	// Render panels into fixed-size boxes, leaving out the ones switched
	// off in the settings along with their separator column.
	vsep := m.renderVSep(contentH)
	var panels []string
	if sidebarW > 0 {
		panels = append(panels,
			lipgloss.Place(sidebarW, contentH, lipgloss.Left, lipgloss.Top, m.sidebar.View()),
			vsep)
	}
	panels = append(panels,
		lipgloss.Place(filelistW, contentH, lipgloss.Left, lipgloss.Top, m.filelist.View()))
	if previewW > 0 {
		panels = append(panels, vsep,
			lipgloss.Place(previewW, contentH, lipgloss.Left, lipgloss.Top, m.preview.View()))
	}

	content := lipgloss.JoinHorizontal(lipgloss.Top, panels...)

	// Popups float over the panels. Several can be open at once (the
	// file history sits on top of the git panel); the topmost is the
	// one drawn, and the order below is bottom to top.
	var box string
	atTop := false
	if m.searchOverlay.IsActive() {
		m.searchOverlay.SetSize(m.searchWidth(), contentH)
		box = m.searchOverlay.View()
	}
	if m.gitOverlay.IsActive() {
		m.gitOverlay.SetSize(m.width*4/5, contentH)
		box = m.gitOverlay.View()
	}
	if m.branchesPopup.IsActive() {
		m.branchesPopup.SetSize(m.width*3/5, contentH*3/4)
		box = m.branchesPopup.View()
	}
	if m.fileHistoryPopup.IsActive() {
		m.fileHistoryPopup.SetSize(m.width*4/5, contentH*4/5)
		box = m.fileHistoryPopup.View()
	}
	if m.blamePopup.IsActive() {
		m.blamePopup.SetSize(m.width*4/5, contentH*4/5)
		box = m.blamePopup.View()
	}
	if m.historyPopup.IsActive() {
		m.historyPopup.SetSize(m.width*4/5, contentH*4/5)
		box = m.historyPopup.View()
	}
	if m.settingsPopup.IsActive() {
		m.settingsPopup.SetSize(m.width*4/5, contentH*4/5)
		box = m.settingsPopup.View()
	}
	if m.picker.IsActive() {
		m.picker.SetSize(m.width, contentH)
		box = m.picker.View()
		// A quick-open box belongs near the top, where the eye already
		// is, not dead centre.
		atTop = true
	}
	if m.infoBox != "" {
		box = m.infoBox
	}
	if box != "" {
		x, y := centred(box, m.width, contentH)
		if atTop {
			y = min(y, 2)
		}
		content = floatOver(content, box, m.width, x, y)
	}

	statusBar := m.renderStatusBar()

	rows := make([]string, 0, 6)
	if m.tabBarVisible() {
		rows = append(rows, m.renderTabBar())
	}
	rows = append(rows, breadcrumb, sep, content, sep, statusBar)

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// ── Layout helpers ─────────────────────────────────────────────────────

// cyclePanel moves focus to the next visible panel. Tab must never park
// the cursor on a panel that is not on screen.
func (m *Model) cyclePanel(dir int) {
	ui := config.Get().UI
	visible := func(p panel) bool {
		switch p {
		case panelSidebar:
			return ui.Sidebar
		case panelPreview:
			return ui.Preview
		}
		return true
	}
	for i := 0; i < 3; i++ {
		m.active = panel((int(m.active) + dir + 3) % 3)
		if visible(m.active) {
			return
		}
	}
	m.active = panelFileList
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
	// The quick-open popups scroll and lay out their preview by their
	// own height, so they must know it while handling keys — not only
	// in View, which works on a copy.
	m.searchOverlay.SetSize(m.searchWidth(), contentH)
	m.picker.SetSize(m.width, contentH)
	m.updateFocus()
}

// panelWidths splits the terminal between the three panels. A panel
// switched off in the settings gets zero width, and View leaves out both
// it and its separator.
func (m Model) panelWidths() (int, int, int) {
	ui := config.Get().UI

	// Only the separators of panels that are actually drawn cost a column.
	available := m.width
	if ui.Sidebar {
		available--
	}
	if ui.Preview {
		available--
	}

	var sidebarW, previewW int
	if ui.Sidebar {
		sidebarW = clamp(available*ui.SidebarWidth/100, 14, 40)
	}
	if ui.Preview {
		previewW = clamp(available*ui.PreviewWidth/100, 16, max(16, available/2))
	}
	filelistW := available - sidebarW - previewW
	if filelistW < 20 {
		filelistW = 20
	}
	return sidebarW, filelistW, previewW
}

// searchWidth is how wide the search popup is: most of the window, since
// it has to fit a path, a line of code and a preview.
func (m Model) searchWidth() int {
	return min(m.width-4, max(m.width*3/4, 70))
}

func (m Model) contentHeight() int {
	// [tab strip] + breadcrumb + rule + content + rule + status bar
	return max(1, m.height-m.chromeTop()-2)
}

// modalActive reports whether something other than the three panels has
// the keyboard: a popup, a prompt in the status bar, the list filter.
func (m Model) modalActive() bool {
	return m.dialogMode != DialogNone ||
		m.infoBox != "" ||
		m.awaitingSort ||
		m.filelist.IsFiltering() ||
		m.picker.IsActive() ||
		m.settingsPopup.IsActive() ||
		m.historyPopup.IsActive() ||
		m.blamePopup.IsActive() ||
		m.fileHistoryPopup.IsActive() ||
		m.branchesPopup.IsActive() ||
		m.gitOverlay.IsActive() ||
		m.searchOverlay.IsActive()
}

// ApplyGlobalConfig pushes the settings into the packages that keep
// their own copy of them — the keymap and the sorting/formatting used
// deep inside rendering. It runs once at startup, before the first
// directory is read, and again after every settings change.
func ApplyGlobalConfig(c config.Config) {
	keys.SetOverrides(c.Keys.Bindings)
	filesystem.SetSortOptions(c.Files.DirsFirst, c.Files.Reverse)
	filesystem.SetFormatOptions(c.Files.TimeFormat, c.Files.BinaryUnits)
}

// listCmd delivers a message to the file list and hands back the command
// it produced. Directory reads are asynchronous now, so dropping that
// command would mean the list quietly never reloads.
func (m *Model) listCmd(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.filelist, cmd = m.filelist.Update(msg)
	return cmd
}

// ── Settings plumbing ──────────────────────────────────────────────────

// applySettings pushes freshly-saved configuration into the components
// that keep their own copy of it. Everything else reads config.Get() at
// the point of use, so this stays short by design.
func (m *Model) applySettings() tea.Cmd {
	c := config.Get()
	ApplyGlobalConfig(c)

	var cmds []tea.Cmd
	cmds = append(cmds, m.listCmd(filelist.SetShowHiddenMsg{Show: c.Files.ShowHidden}))
	// Sorting options are not part of SortListMsg, so nudge the list into
	// re-reading with them applied even when the mode itself is unchanged.
	cmds = append(cmds, m.listCmd(filelist.RefreshListMsg{}))
	cmds = append(cmds, m.listCmd(filelist.SortListMsg{Mode: c.SortMode()}))
	m.refreshGit()

	// Reload the preview so a changed media / size limit is visible
	// immediately rather than after the next cursor move.
	if entry := m.filelist.SelectedEntry(); entry != nil {
		m.stampPreview(entry)
		cmds = append(cmds, m.preview.LoadFile(*entry))
	}
	return tea.Batch(cmds...)
}

// ── File operation handlers ──────────────────────────────────────────────

// performDelete removes entries using the configured delete mode and
// records the result so u can undo it. Shared by the confirmed path
// (y in the dialog) and the unconfirmed one (confirmation switched off).
func (m Model) performDelete(entries []filesystem.FileEntry) (tea.Model, tea.Cmd) {
	var items []HistoryItem
	var delErr error
	for _, entry := range entries {
		dst, err := DeleteEntry(entry.Path, entry.IsDir)
		if err != nil {
			delErr = err
			break
		}
		items = append(items, HistoryItem{Src: entry.Path, Dst: dst, IsDir: entry.IsDir})
	}
	// Whatever was deleted before the failure still happened, so it goes
	// on the undo stack: aborting without recording it would leave those
	// files unrecoverable.
	if len(items) > 0 {
		PushHistory(HistoryEvent{Op: OpDelete, Items: items})
	}
	if delErr != nil {
		m.statusMsg = "Error: " + delErr.Error()
	} else {
		m.statusMsg = deleteStatus(len(items))
	}

	var cmds []tea.Cmd
	cmds = append(cmds, m.listCmd(filelist.RefreshListMsg{}))
	m.filelist, _ = m.filelist.Update(filelist.ClearSelectionMsg{})

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

// deleteStatus phrases the status line to match what actually happened —
// "deleted" means something different in each mode, and the difference
// is exactly the thing the user needs to know.
func deleteStatus(n int) string {
	c := config.Get()
	switch c.Delete.Mode {
	case config.DeleteTrash:
		return fmt.Sprintf("Moved %d item(s) to Trash", n)
	case config.DeletePermanent:
		return fmt.Sprintf("Deleted %d item(s) permanently", n)
	default:
		return fmt.Sprintf("Deleted %d item(s) — u to undo within %ds",
			n, c.Delete.TimerSeconds)
	}
}

// handlerKindFor maps an extension onto one of the file kinds the
// settings screen offers a handler for. The kinds mirror what tuiple
// itself knows how to do: everything it can preview or launch specially
// gets its own row, and the rest falls under "default".
func handlerKindFor(ext string) string {
	switch mediarender.Classify(ext) {
	case mediarender.KindImage:
		return config.HandlerImage
	case mediarender.KindVideo:
		return config.HandlerVideo
	case mediarender.KindAudio:
		return config.HandlerAudio
	case mediarender.KindPDF:
		return config.HandlerPDF
	}
	switch ext {
	case ".epub", ".djvu", ".mobi", ".fb2":
		return config.HandlerEbook
	}
	if IsArchive("x" + ext) {
		return config.HandlerArchive
	}
	return config.HandlerDefault
}

// openWithRule runs a user-configured open-with command. "{}" in the
// command is replaced by the quoted path; a command without a
// placeholder gets the path appended.
//
// Terminal rules take the screen over and tuiple restores itself when
// they exit; everything else is launched detached, so a GUI application
// never blocks the file list. Detached commands inherit no terminal, so
// their output cannot corrupt the TUI.
func openWithRule(rule config.OpenRule, path string) tea.Cmd {
	cmdLine := rule.Command
	quoted := shellQuote(path)
	if strings.Contains(cmdLine, "{}") {
		cmdLine = strings.ReplaceAll(cmdLine, "{}", quoted)
	} else {
		cmdLine += " " + quoted
	}

	if rule.Terminal {
		return tea.ExecProcess(exec.Command("sh", "-c", cmdLine), func(err error) tea.Msg {
			return filelist.RefreshListMsg{}
		})
	}
	return func() tea.Msg {
		_ = exec.Command("sh", "-c", cmdLine).Start()
		return nil
	}
}

// shellQuote makes a path safe to paste into an `sh -c` command line.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

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
			var discardCmd tea.Cmd
			switch msg.String() {
			case "y", "Y":
				if m.gitRoot != "" {
					rel := strings.TrimPrefix(m.gitDiscardPath, m.gitRoot+"/")
					if err := git.DiscardFile(m.gitRoot, rel); err != nil {
						m.statusMsg = "discard: " + err.Error()
					} else {
						m.statusMsg = "discarded " + filepath.Base(m.gitDiscardPath)
						discardCmd = m.listCmd(filelist.RefreshListMsg{})
						m.refreshGit()
					}
				}
				m.dialogMode = DialogNone
				m.gitDiscardPath = ""
			case "n", "N", "esc":
				m.dialogMode = DialogNone
				m.gitDiscardPath = ""
			}
			return m, discardCmd
		}

		if m.dialogMode == DialogDelete {
			if msg.String() == "y" || msg.String() == "Y" {
				entries := m.opEntries
				m.dialogMode = DialogNone
				return m.performDelete(entries)
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
			TouchRecent(target, info.IsDir())
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
			var refreshCmd tea.Cmd
			val := m.textInput.Value()
			if val != "" {
				var err error
				switch m.dialogMode {
				case DialogRename:
					// Next to the original, which in the tree view need
					// not be the directory the list is rooted at.
					dst := filepath.Join(filepath.Dir(m.opEntries[0].Path), val)
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
					refreshCmd = m.listCmd(filelist.RefreshListMsg{})
				}
			}
			m.dialogMode = DialogNone
			m.textInput.Blur()
			return m, refreshCmd
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
// If dst already exists it inserts _1, _2, ... before the extension (for
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

	conflict := config.Get().Files.PasteConflict
	var skipped int

	for _, item := range items {
		dst := filepath.Join(m.currentPath, item.Entry.Name)
		if _, taken := os.Stat(dst); taken == nil {
			switch conflict {
			case config.PasteSkip:
				skipped++
				continue
			case config.PasteOverwrite:
				// Move refuses to replace a directory, and CopyDir would
				// merge into it; clearing the way first makes overwrite
				// mean the same thing for both kinds.
				if err := filesystem.Remove(dst, item.Entry.IsDir); err != nil {
					m.statusMsg = "Paste error: " + err.Error()
					return m, nil
				}
			default:
				dst = uniqueDst(dst, item.Entry.IsDir)
			}
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
	switch {
	case skipped > 0 && len(itemsDone) == 0:
		m.statusMsg = fmt.Sprintf("Nothing pasted — %d name(s) already taken", skipped)
	case skipped > 0:
		m.statusMsg = fmt.Sprintf("Pasted %d item(s), skipped %d", len(itemsDone), skipped)
	default:
		m.statusMsg = "Pasted successfully"
	}
	return m, m.listCmd(filelist.RefreshListMsg{})
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

// ── Help screen ────────────────────────────────────────────────────────

// helpTabCount is the number of tabs on the help screen.
const helpTabCount = 3

func (m Model) renderHelp() string {
	type helpItem struct{ key, desc string }
	type helpCat struct {
		title string
		items []helpItem
	}

	// ── Tab 0: System — generated from the keymap ────────────────────
	//
	// Nothing here is written down twice: change a binding in package
	// keys (or override it in config.json) and this screen follows.
	fromKeymap := func(categories ...string) []helpCat {
		var out []helpCat
		for _, name := range categories {
			cat := helpCat{title: name}
			for _, b := range keys.InCategory(name) {
				cat.items = append(cat.items, helpItem{b.Display(), b.Desc})
			}
			if len(cat.items) > 0 {
				out = append(out, cat)
			}
		}
		return out
	}

	navLeft := fromKeymap("Navigation", "Tabs")
	navRight := fromKeymap("Search & Bookmarks", "System & Options", "Audio Player")
	filesLeft := fromKeymap("Selection & Clipboard", "Undo & History")
	filesRight := fromKeymap("File Operations")

	// ── Tab 1: Git ───────────────────────────────────────────────────
	//
	// The popups own their keys — inside them a letter means whatever
	// that popup is showing — so those stay written out here. Only the
	// bindings that reach git from the file list come from the keymap.
	gitLeft := fromKeymap("Git")
	gitLeft = append(gitLeft,
		helpCat{"Git panel  (Ctrl+G)", []helpItem{
			{"Tab / Shift+Tab", "Switch tab (Commit / Log / Stashes)"},
			{"1 / 2 / 3", "Jump to tab directly"},
			{"Esc", "Close panel"},
		}},
		helpCat{"Branches popup  (b)", []helpItem{
			{"Tab / 1 2 3", "Branches / Remotes / Tags"},
			{"Enter / l", "Checkout (smart if changes block)"},
			{"Enter / Space", "Fold / unfold a feature/ folder"},
			{"c", "Compare with current branch"},
			{"n / N", "New branch from HEAD / selected"},
			{"r / d / D", "Rename / Delete safe / Force-delete"},
			{"m / R", "Merge into current / Rebase onto"},
			{"t", "Track remote branch / untrack local"},
			{"P / p / U", "Push / Pull / Pull with rebase"},
			{"F", "Fetch all remotes"},
			{"C / A", "Continue / Abort merge or rebase"},
			{"/  ·  Ctrl+R", "Filter  ·  Reload"},
			{"i", "git init (outside a repository)"},
		}},
	)

	gitRight := []helpCat{
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
		{"Log tab  (branch graph)", []helpItem{
			{"a", "All branches ↔ current branch only"},
			{"d", "Hide / show the details pane"},
			{"Enter", "Actions: checkout, merge, rebase…"},
			{"g / G", "Newest / oldest commit"},
			{"/", "Filter by subject / author / SHA"},
			{"r", "Reset HEAD here (soft/mixed/hard)"},
			{"c / v", "Cherry-pick / Revert"},
			{"b", "Create branch from commit"},
			{"Ctrl+R", "Reload log"},
		}},
		{"Branches popup: Remotes / Tags", []helpItem{
			{"a / e / r / d", "Add / Edit URL / Rename / Remove remote"},
			{"f / o / Enter", "Fetch / Open in browser / Its branches"},
			{"n / d / P", "New tag at HEAD / Delete / Push tag"},
		}},
		{"Stashes tab", []helpItem{
			{"a / p / D", "Apply / Pop / Drop"},
			{"r", "Reload"},
		}},
		{"Diff / detail scrolling", []helpItem{
			{"Ctrl+D / PgDn", "Half-page down"},
			{"Ctrl+U / PgUp", "Half-page up"},
			{"Home / End", "Top / Bottom"},
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
	switch m.helpTabIdx {
	case 0:
		leftCats, rightCats = navLeft, navRight
	case 1:
		leftCats, rightCats = filesLeft, filesRight
	default:
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

	// The left column is as wide as its longest line, so a description
	// never wraps under the key column.
	leftColW := 58
	for _, l := range leftLines {
		leftColW = max(leftColW, lipgloss.Width(l)+3)
	}
	leftCol := lipgloss.NewStyle().Width(leftColW).Render(strings.Join(leftLines, "\n"))
	rightCol := strings.Join(rightLines, "\n")
	cols := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, rightCol)

	// Tab bar (same style as the git overlay)
	tabTitles := []string{"Navigate", "Files", "Git"}
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
		theme.Dim.Render("    Tab / Shift+Tab switch · 1 2 3 jump · ? or Esc close")

	var out []string
	out = append(out, "")
	out = append(out, theme.PreviewTitle.Render("  Tuiple — Keyboard Shortcuts"))
	out = append(out, tabBar)
	out = append(out, "")
	out = append(out, cols)
	if m.helpTabIdx < 2 {
		// The ⌘ keys only exist where the terminal passes them through.
		out = append(out, theme.Muted.Render(
			"  ⌘ shortcuts reach tuiple through the \"nvim IDE\" iTerm2 profile (nvim-ide/install.sh);"+
				" the plain keys work everywhere."))
		out = append(out, theme.Muted.Render(
			"  Every action is also in the palette — Ctrl+P — and can be rebound in the settings file."))
	}

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
