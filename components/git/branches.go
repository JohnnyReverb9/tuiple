package git

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JohnnyReverb9/tuiple/theme"
)

// BranchesChangedMsg is emitted by the popup after an operation that
// might have changed the current branch (e.g. switch, merge), so the
// outer app can refresh its working-directory state if needed.
type BranchesChangedMsg struct{}

// branchesView is the list the popup shows: branches, remotes or tags.
type branchesView int

const (
	viewBranches branchesView = iota
	viewRemotes
	viewTags
)

var branchesViewTitles = []string{"Branches", "Remotes", "Tags"}

type branchesInputMode int

const (
	branchesInputNone branchesInputMode = iota
	branchesInputCreate
	branchesInputCreateFrom
	branchesInputRename
	branchesInputFilter
	branchesInputRemoteName
	branchesInputRemoteURL
	branchesInputRemoteSetURL
	branchesInputRemoteRename
	branchesInputTagName
	branchesInputTagMessage
)

// pendingConfirm describes an operation that needs a y/n confirmation.
type pendingConfirm int

const (
	pendingNone pendingConfirm = iota
	pendingMerge
	pendingRebase
	pendingForceDelete
	pendingDeleteRemoteBranch
	pendingRemoveRemote
	pendingDeleteTag
	pendingAbort
	pendingInit
)

// opKind identifies an async (network) git operation.
type opKind int

const (
	opPush opKind = iota
	opPull
	opPullRebase
	opFetch
	opFetchRemote
	opAddRemote
	opPushTag
	opDeleteRemoteBranch
)

var opBusyLabels = map[opKind]string{
	opPush:               "Pushing",
	opPull:               "Pulling",
	opPullRebase:         "Pulling (rebase)",
	opFetch:              "Fetching",
	opFetchRemote:        "Fetching",
	opAddRemote:          "Adding remote",
	opPushTag:            "Pushing tag",
	opDeleteRemoteBranch: "Deleting remote branch",
}

// opDoneMsg delivers the result of an async git operation back to the
// popup's Update loop. The "Pushing..." style status set before issuing
// the command stays on screen until the corresponding done-message lands.
type opDoneMsg struct {
	op     opKind
	target string // remote / tag / branch the op worked on, for the status line
	out    string
	err    error
}

func opCmd(op opKind, target string, fn func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		out, err := fn()
		return opDoneMsg{op: op, target: target, out: out, err: err}
	}
}

// BranchesPopup is a standalone overlay with three views: branches
// (checkout / create / delete / rename / merge / rebase / track / push /
// pull), remotes (add / edit URL / rename / remove / fetch / open) and
// tags (create / delete / push). Outside a repository it offers git init.
type BranchesPopup struct {
	active bool

	path string // directory the popup was opened from (git init target)
	repo string
	view branchesView
	head Head

	branches []Branch
	remotes  []Remote
	tags     []Tag

	filter   string
	filtered []int // indexes into the current view's list when filter is active; nil otherwise

	cursors [3]int // one cursor per view

	loadErr error

	input     textinput.Model
	inputMode branchesInputMode
	draft     string // first answer of a two-step prompt (remote name, tag name)

	pending     pendingConfirm
	pendingName string // branch / remote / tag the pending op targets
	pendingItem Branch // full branch for pendingDeleteRemoteBranch

	status    string
	statusErr bool

	// busy is set while an async network operation is in flight. All
	// mutating keys are no-ops in this state so we never end up with two
	// `git` processes fighting for .git/index.lock. Navigation, filter,
	// view switching and closing the popup remain available.
	busy string // "" when idle, otherwise a label such as "Pushing"

	width  int
	height int
}

// NewBranches creates a fresh BranchesPopup.
func NewBranches() BranchesPopup {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.CharLimit = 400
	ti.Width = 40
	return BranchesPopup{input: ti}
}

// SetSize sets the rendering viewport for the popup.
func (m *BranchesPopup) SetSize(w, h int) { m.width = w; m.height = h }

// IsActive reports whether the popup is currently visible.
func (m BranchesPopup) IsActive() bool { return m.active }

// AcceptsText reports whether the popup's prompt (filter, names, URLs)
// currently has the keyboard.
func (m BranchesPopup) AcceptsText() bool { return m.active && m.input.Focused() }

// Start opens the popup at the given working directory. The repository
// root is detected from this path.
func (m *BranchesPopup) Start(path string) tea.Cmd {
	m.active = true
	m.path = path
	m.view = viewBranches
	m.filter = ""
	m.filtered = nil
	m.cursors = [3]int{}
	m.status = ""
	m.statusErr = false
	m.pending = pendingNone
	m.pendingName = ""
	m.busy = ""
	m.inputMode = branchesInputNone
	m.draft = ""
	m.input.Blur()
	m.open()
	return nil
}

// open probes the repository at m.path and loads every view.
func (m *BranchesPopup) open() {
	root, err := FindRoot(m.path)
	if err != nil {
		m.loadErr = err
		m.repo = ""
		m.branches, m.remotes, m.tags = nil, nil, nil
		return
	}
	m.loadErr = nil
	m.repo = root
	m.reload()
}

// Stop hides the popup.
func (m *BranchesPopup) Stop() {
	m.active = false
	m.input.Blur()
}

func (m *BranchesPopup) reload() {
	if m.repo == "" {
		return
	}
	m.head = HeadState(m.repo)
	branches, err := ListBranches(m.repo)
	if err != nil {
		m.loadErr = err
		return
	}
	m.loadErr = nil
	m.branches = branches
	m.remotes, _ = Remotes(m.repo)
	m.tags, _ = ListTags(m.repo)
	m.applyFilter()
	m.clampCursor()
}

func (m *BranchesPopup) clampCursor() {
	c := &m.cursors[m.view]
	if *c >= m.visibleCount() {
		*c = max(0, m.visibleCount()-1)
	}
}

// itemName returns the name of item i of the current view (unfiltered).
func (m BranchesPopup) itemName(i int) string {
	switch m.view {
	case viewRemotes:
		return m.remotes[i].Name + " " + m.remotes[i].FetchURL
	case viewTags:
		return m.tags[i].Name + " " + m.tags[i].Subject
	}
	return m.branches[i].Name
}

func (m BranchesPopup) itemCount() int {
	switch m.view {
	case viewRemotes:
		return len(m.remotes)
	case viewTags:
		return len(m.tags)
	}
	return len(m.branches)
}

func (m *BranchesPopup) applyFilter() {
	if m.filter == "" {
		m.filtered = nil
		return
	}
	needle := strings.ToLower(m.filter)
	filtered := make([]int, 0, m.itemCount())
	for i := 0; i < m.itemCount(); i++ {
		if strings.Contains(strings.ToLower(m.itemName(i)), needle) {
			filtered = append(filtered, i)
		}
	}
	m.filtered = filtered
	m.clampCursor()
}

func (m BranchesPopup) visibleCount() int {
	if m.filtered != nil {
		return len(m.filtered)
	}
	return m.itemCount()
}

// index maps a visible row to an index into the current view's list.
func (m BranchesPopup) index(i int) (int, bool) {
	if m.filtered != nil {
		if i < 0 || i >= len(m.filtered) {
			return 0, false
		}
		return m.filtered[i], true
	}
	if i < 0 || i >= m.itemCount() {
		return 0, false
	}
	return i, true
}

func (m BranchesPopup) cursor() int { return m.cursors[m.view] }

func (m BranchesPopup) selected() (Branch, bool) {
	if m.view != viewBranches {
		return Branch{}, false
	}
	i, ok := m.index(m.cursor())
	if !ok {
		return Branch{}, false
	}
	return m.branches[i], true
}

func (m BranchesPopup) selectedRemote() (Remote, bool) {
	if m.view != viewRemotes {
		return Remote{}, false
	}
	i, ok := m.index(m.cursor())
	if !ok {
		return Remote{}, false
	}
	return m.remotes[i], true
}

func (m BranchesPopup) selectedTag() (Tag, bool) {
	if m.view != viewTags {
		return Tag{}, false
	}
	i, ok := m.index(m.cursor())
	if !ok {
		return Tag{}, false
	}
	return m.tags[i], true
}

func (m BranchesPopup) branchByName(name string) (Branch, bool) {
	for _, b := range m.branches {
		if b.Name == name {
			return b, true
		}
	}
	return Branch{}, false
}

func (m *BranchesPopup) setView(v branchesView) {
	if m.view == v {
		return
	}
	m.view = v
	m.filter = ""
	m.filtered = nil
	m.clampCursor()
}

// ── Branch operations ─────────────────────────────────────────────────

func (m *BranchesPopup) doSwitch() {
	b, ok := m.selected()
	if !ok {
		return
	}
	if b.IsCurrent {
		m.setStatus("already on "+b.Name, false)
		return
	}
	name, err := SwitchTo(m.repo, b)
	if err != nil {
		m.setStatus(shortGitError(err), true)
		return
	}
	if b.IsRemote {
		m.setStatus("switched to "+name+" (tracking "+b.Name+")", false)
	} else {
		m.setStatus("switched to "+name, false)
	}
	m.reload()
}

func (m *BranchesPopup) doCreate(name, from string) {
	name = strings.TrimSpace(name)
	if name == "" {
		m.setStatus("empty branch name", true)
		return
	}
	var err error
	if from == "" {
		err = CreateAndSwitch(m.repo, name)
	} else {
		err = CreateBranchFrom(m.repo, name, from)
	}
	if err != nil {
		m.setStatus(shortGitError(err), true)
		return
	}
	if from == "" {
		m.setStatus("created and switched to "+name, false)
	} else {
		m.setStatus("created "+name+" from "+from+" and switched to it", false)
	}
	m.reload()
}

func (m *BranchesPopup) doRename(oldName, newName string) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		m.setStatus("empty new name", true)
		return
	}
	b, ok := m.branchByName(oldName)
	if !ok {
		return
	}
	from := ""
	if !b.IsCurrent {
		from = b.Name
	}
	if err := RenameBranch(m.repo, from, newName); err != nil {
		m.setStatus(shortGitError(err), true)
		return
	}
	m.setStatus("renamed "+oldName+" to "+newName, false)
	m.reload()
}

// doDelete deletes the local branch `name`. A checked-out branch is left
// first (preferring main / master / develop); if git then refuses the
// delete, the original branch is checked out again so a failed delete
// never leaves the user on another branch.
func (m *BranchesPopup) doDelete(name string, force bool) {
	b, ok := m.branchByName(name)
	if !ok || b.IsRemote {
		return
	}
	switchedFrom := ""
	if b.IsCurrent {
		target := m.pickFallbackBranch(b.Name)
		if target == "" {
			m.setStatus("cannot delete the only local branch", true)
			return
		}
		if err := Switch(m.repo, target); err != nil {
			m.setStatus("switch to "+target+": "+shortGitError(err), true)
			return
		}
		switchedFrom = b.Name
	}

	if err := DeleteBranch(m.repo, b.Name, force); err != nil {
		if switchedFrom != "" {
			_ = Switch(m.repo, switchedFrom)
		}
		if !force && strings.Contains(err.Error(), "not fully merged") {
			m.pending = pendingForceDelete
			m.pendingName = b.Name
			m.setStatus(fmt.Sprintf("%q is not fully merged. Press D or y to force-delete, n to cancel.", b.Name), true)
			return
		}
		m.setStatus(shortGitError(err), true)
		m.reload()
		return
	}
	if switchedFrom != "" {
		m.setStatus("deleted "+b.Name+" (now on "+m.pickFallbackBranch(b.Name)+")", false)
	} else {
		m.setStatus("deleted "+b.Name, false)
	}
	m.reload()
}

// pickFallbackBranch returns the name of a local branch to check out
// before deleting `avoid`. Prefers main / master / develop over an
// arbitrary other branch so the user lands somewhere sensible.
func (m *BranchesPopup) pickFallbackBranch(avoid string) string {
	prefer := []string{"main", "master", "develop"}
	for _, name := range prefer {
		for _, b := range m.branches {
			if !b.IsRemote && !b.Unborn && b.Name == name && name != avoid {
				return name
			}
		}
	}
	for _, b := range m.branches {
		if !b.IsRemote && !b.Unborn && b.Name != avoid {
			return b.Name
		}
	}
	return ""
}

func (m *BranchesPopup) doMerge(name string) {
	if err := MergeBranch(m.repo, name); err != nil {
		m.setStatus(shortGitError(err), true)
		m.reload()
		return
	}
	m.setStatus("merged "+name+" into "+m.head.Label(), false)
	m.reload()
}

func (m *BranchesPopup) doRebase(name string) {
	if err := RebaseOnto(m.repo, name); err != nil {
		m.setStatus(shortGitError(err), true)
		m.reload()
		return
	}
	m.setStatus("rebased "+m.head.Label()+" onto "+name, false)
	m.reload()
}

// doTrack makes the selected remote branch the upstream of the current
// branch, or — on a local branch — removes that branch's upstream.
func (m *BranchesPopup) doTrack() {
	b, ok := m.selected()
	if !ok {
		return
	}
	if !b.IsRemote {
		if b.Upstream == "" {
			m.setStatus("pick a remote branch to track it from "+m.head.Branch, true)
			return
		}
		if err := UnsetUpstream(m.repo, b.Name); err != nil {
			m.setStatus(shortGitError(err), true)
			return
		}
		m.setStatus(b.Name+" no longer tracks "+b.Upstream, false)
		m.reload()
		return
	}
	if m.head.Branch == "" || m.head.Unborn {
		m.setStatus("check out a branch with commits first", true)
		return
	}
	if err := SetUpstream(m.repo, m.head.Branch, b.Name); err != nil {
		m.setStatus(shortGitError(err), true)
		return
	}
	m.setStatus(m.head.Branch+" now tracks "+b.Name, false)
	m.reload()
}

func (m *BranchesPopup) doContinue() {
	out, err := ContinueOp(m.repo, m.head.Op)
	if err != nil {
		m.setStatus(shortGitError(err), true)
		m.reload()
		return
	}
	m.setStatus(m.head.Op+" continued: "+firstLine(out), false)
	m.reload()
}

func (m *BranchesPopup) doAbort() {
	op := m.head.Op
	if _, err := AbortOp(m.repo, op); err != nil {
		m.setStatus(shortGitError(err), true)
		m.reload()
		return
	}
	m.setStatus(op+" aborted", false)
	m.reload()
}

func (m *BranchesPopup) doInit() {
	root, err := Init(m.path)
	if err != nil {
		m.setStatus(shortGitError(err), true)
		return
	}
	m.open()
	m.setStatus("initialized a repository in "+root, false)
}

// ── Remote operations ─────────────────────────────────────────────────

func (m *BranchesPopup) doSetRemoteURL(name, url string) {
	url = strings.TrimSpace(url)
	if url == "" {
		m.setStatus("empty URL", true)
		return
	}
	if err := SetRemoteURL(m.repo, name, url); err != nil {
		m.setStatus(shortGitError(err), true)
		return
	}
	m.setStatus(name+" → "+url, false)
	m.reload()
}

func (m *BranchesPopup) doRenameRemote(from, to string) {
	to = strings.TrimSpace(to)
	if to == "" {
		m.setStatus("empty remote name", true)
		return
	}
	if err := RenameRemote(m.repo, from, to); err != nil {
		m.setStatus(shortGitError(err), true)
		return
	}
	m.setStatus("renamed remote "+from+" to "+to, false)
	m.reload()
}

func (m *BranchesPopup) doRemoveRemote(name string) {
	if err := RemoveRemote(m.repo, name); err != nil {
		m.setStatus(shortGitError(err), true)
		return
	}
	m.setStatus("removed remote "+name, false)
	m.reload()
}

func (m *BranchesPopup) doOpenRemote() {
	r, ok := m.selectedRemote()
	if !ok {
		return
	}
	url := WebURL(r.FetchURL)
	if url == "" {
		m.setStatus(r.Name+" has no web page ("+r.FetchURL+")", true)
		return
	}
	if err := exec.Command("open", url).Start(); err != nil {
		m.setStatus("open: "+err.Error(), true)
		return
	}
	m.setStatus("opened "+url, false)
}

// ── Tag operations ────────────────────────────────────────────────────

func (m *BranchesPopup) doCreateTag(name, message string) {
	if err := CreateTag(m.repo, name, message); err != nil {
		m.setStatus(shortGitError(err), true)
		return
	}
	kind := "lightweight"
	if strings.TrimSpace(message) != "" {
		kind = "annotated"
	}
	m.setStatus("created "+kind+" tag "+name+" at "+m.head.ShortSHA+" (P to push)", false)
	m.reload()
}

func (m *BranchesPopup) doDeleteTag(name string) {
	if err := DeleteTag(m.repo, name); err != nil {
		m.setStatus(shortGitError(err), true)
		return
	}
	m.setStatus("deleted tag "+name+" (local only)", false)
	m.reload()
}

// ── Async operations ──────────────────────────────────────────────────

func (m *BranchesPopup) startOp(op opKind, target string, fn func() (string, error)) tea.Cmd {
	m.busy = opBusyLabels[op]
	m.setStatus(m.busy+"...", false)
	return opCmd(op, target, fn)
}

func (m *BranchesPopup) finishOp(r opDoneMsg) {
	m.busy = ""
	if r.err != nil {
		m.setStatus(shortGitError(r.err), true)
		m.reload()
		return
	}
	switch r.op {
	case opPush:
		m.setStatus(parsePushOutput(r.out), false)
	case opPull, opPullRebase:
		m.setStatus(parsePullOutput(r.out), false)
	case opFetch:
		m.setStatus(parseFetchOutput(r.out), false)
	case opFetchRemote:
		m.setStatus(r.target+": "+parseFetchOutput(r.out), false)
	case opAddRemote:
		m.setStatus("added remote "+r.target+" and fetched it", false)
	case opPushTag:
		m.setStatus("pushed tag "+r.target, false)
	case opDeleteRemoteBranch:
		m.setStatus("deleted remote "+r.target, false)
	}
	m.reload()
}

// ── Output parsers ────────────────────────────────────────────────────

// parsePushOutput distils `git push` combined output into one short line.
func parsePushOutput(out string) string {
	lower := strings.ToLower(out)
	if strings.Contains(lower, "everything up-to-date") ||
		strings.Contains(lower, "up to date") {
		return "nothing to push – already up-to-date"
	}
	// "  branch1 -> origin/branch1" or similar ref-update lines
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "->") {
			return "pushed: " + line
		}
	}
	if out != "" {
		return firstLine(strings.TrimSpace(out))
	}
	return "pushed"
}

// shortGitError boils a multi-line git failure (such as the wall of text
// pull/push prints when there is no upstream) down to a single status-bar
// line. Unknown errors fall back to git's first meaningful line.
func shortGitError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrNoRemote):
		return "no remote yet — add one in the Remotes view (Tab, then a)"
	case errors.Is(err, ErrNotARepo):
		return "not a git repository"
	}
	msg := err.Error()
	var gitErr *Error
	if errors.As(err, &gitErr) {
		msg = gitErr.Output
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "no tracking information"),
		strings.Contains(lower, "no upstream branch"):
		return "no upstream — push first with P, or pick a remote branch and press t"
	case strings.Contains(lower, "divergent branches"),
		strings.Contains(lower, "not possible to fast-forward"):
		return "branches diverged — U pulls with rebase"
	case strings.Contains(lower, "would be overwritten by"),
		strings.Contains(lower, "your local changes"),
		strings.Contains(lower, "unstaged changes"):
		return "uncommitted changes block this — stash or commit first"
	case strings.Contains(lower, "conflict"):
		return "conflicts — resolve and stage the files, then C to continue (A aborts)"
	case strings.Contains(lower, "[rejected]"),
		strings.Contains(lower, "non-fast-forward"),
		strings.Contains(lower, "fetch first"):
		return "rejected — the remote has new commits, pull first (p / U)"
	case strings.Contains(lower, "terminal prompts disabled"),
		strings.Contains(lower, "could not read username"):
		return "the remote wants a login — set up a credential helper or SSH key"
	case strings.Contains(lower, "authentication failed"),
		strings.Contains(lower, "permission denied"):
		return "auth failed — check your remote credentials"
	case strings.Contains(lower, "couldn't find remote"),
		strings.Contains(lower, "could not read from remote"),
		strings.Contains(lower, "does not appear to be a git repository"),
		strings.Contains(lower, "could not resolve host"):
		return "remote unreachable"
	case strings.Contains(lower, "not fully merged"):
		return "branch not fully merged (force-delete with D)"
	}
	return firstMeaningfulLine(msg)
}

// firstMeaningfulLine returns git's first line that is not a hint, with
// the "error: " / "fatal: " prefix dropped.
func firstMeaningfulLine(msg string) string {
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "hint:") {
			continue
		}
		for _, p := range []string{"error: ", "fatal: ", "warning: "} {
			line = strings.TrimPrefix(line, p)
		}
		return line
	}
	return strings.TrimSpace(msg)
}

// parsePullOutput distils `git pull` combined output into one short line.
func parsePullOutput(out string) string {
	lower := strings.ToLower(out)
	if strings.Contains(lower, "already up to date") || strings.Contains(lower, "is up to date") {
		return "already up to date"
	}
	if strings.Contains(lower, "successfully rebased") {
		return "pulled and rebased"
	}
	// "Updating abc1234..def5678" → show that line directly
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Updating ") {
			return strings.TrimSpace(line)
		}
	}
	// "N files changed, M insertions(+), K deletions(-)"
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "changed") || strings.Contains(line, "insertion") {
			return strings.TrimSpace(line)
		}
	}
	if out != "" {
		return firstLine(strings.TrimSpace(out))
	}
	return "pulled"
}

// parseFetchOutput distils `git fetch` combined output into one short line.
func parseFetchOutput(out string) string {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return "nothing new – already up to date"
	}
	// Each fetched ref prints a line like " a1b2c3..d4e5f6  main -> origin/main"
	var updates []string
	for _, line := range strings.Split(trimmed, "\n") {
		if strings.Contains(line, "->") {
			updates = append(updates, strings.TrimSpace(line))
		}
	}
	switch {
	case len(updates) == 1:
		return "fetched: " + updates[0]
	case len(updates) > 1:
		return fmt.Sprintf("fetched %d refs", len(updates))
	}
	if strings.HasPrefix(trimmed, "Fetching ") && !strings.Contains(trimmed, "\n") {
		return "nothing new – already up to date"
	}
	return firstLine(trimmed)
}

func (m *BranchesPopup) setStatus(s string, isErr bool) {
	m.status = s
	m.statusErr = isErr
}

// ── Input ─────────────────────────────────────────────────────────────

// Update handles keyboard input for the popup. Returns the new model and
// any commands the runtime should execute (e.g. textinput blink).
func (m BranchesPopup) Update(msg tea.Msg) (BranchesPopup, tea.Cmd) {
	if !m.active {
		return m, nil
	}

	// Async results can arrive while the user is in any mode (filter,
	// pending confirm, regular nav), so they live outside the key switch.
	// The busy flag is cleared here so the "Pushing..." lock is released
	// exactly when the underlying git command has finished.
	if r, ok := msg.(opDoneMsg); ok {
		m.finishOp(r)
		return m, changed
	}

	if m.inputMode != branchesInputNone {
		return m.updateInput(msg)
	}
	if m.pending != pendingNone {
		return m.updatePending(msg)
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	key := keyMsg.String()

	// Keys that work in every state, including busy and outside a repo.
	switch key {
	case "esc", "ctrl+c", "ctrl+b":
		m.Stop()
		return m, nil
	case "up", "k":
		if m.cursors[m.view] > 0 {
			m.cursors[m.view]--
		}
		return m, nil
	case "down", "j":
		if m.cursors[m.view] < m.visibleCount()-1 {
			m.cursors[m.view]++
		}
		return m, nil
	case "home", "g":
		m.cursors[m.view] = 0
		return m, nil
	case "end", "G":
		m.cursors[m.view] = max(0, m.visibleCount()-1)
		return m, nil
	case "tab", "right":
		m.setView((m.view + 1) % branchesView(len(branchesViewTitles)))
		return m, nil
	case "shift+tab", "left":
		m.setView((m.view + branchesView(len(branchesViewTitles)) - 1) % branchesView(len(branchesViewTitles)))
		return m, nil
	case "1", "2", "3":
		m.setView(branchesView(key[0] - '1'))
		return m, nil
	case "/":
		return m, m.prompt(branchesInputFilter, m.filter, "filter "+strings.ToLower(branchesViewTitles[m.view]))
	}

	if m.loadErr != nil {
		if errors.Is(m.loadErr, ErrNotARepo) && key == "i" {
			m.pending = pendingInit
			m.setStatus("Run git init in "+m.path+"? (y/n)", false)
		}
		return m, nil
	}

	// While a network op is in flight, block every key that would mutate
	// the repo so we don't start a second `git` process.
	if m.busy != "" {
		m.setStatus(m.busy+"... please wait", false)
		return m, nil
	}

	// Keys shared by all views.
	switch key {
	case "ctrl+r":
		m.reload()
		m.setStatus("reloaded", false)
		return m, nil
	case "F":
		return m, m.startOp(opFetch, "", func() (string, error) { return Fetch(m.repo) })
	case "C":
		if m.head.Op != "" {
			m.doContinue()
			return m, changed
		}
	case "A":
		if m.head.Op != "" {
			m.pending = pendingAbort
			m.setStatus(fmt.Sprintf("Abort the %s in progress? (y/n)", m.head.Op), true)
			return m, nil
		}
	}

	switch m.view {
	case viewRemotes:
		return m.updateRemotesKey(key)
	case viewTags:
		return m.updateTagsKey(key)
	}
	return m.updateBranchesKey(key)
}

// changed tells the app that HEAD or the branch list may have changed.
func changed() tea.Msg { return BranchesChangedMsg{} }

func (m BranchesPopup) updateBranchesKey(key string) (BranchesPopup, tea.Cmd) {
	switch key {
	case "enter", "l":
		m.doSwitch()
		return m, changed
	case "n":
		return m, m.prompt(branchesInputCreate, "", "new branch name (from "+m.head.Label()+")")
	case "N":
		if b, ok := m.selected(); ok {
			m.draft = b.Name
			return m, m.prompt(branchesInputCreateFrom, b.LocalName(), "new branch name (from "+b.Name+")")
		}
	case "r":
		if b, ok := m.selected(); ok {
			if b.IsRemote {
				m.setStatus("cannot rename a remote branch", true)
				return m, nil
			}
			m.draft = b.Name
			return m, m.prompt(branchesInputRename, b.Name, "new name")
		}
	case "d", "D":
		b, ok := m.selected()
		if !ok {
			return m, nil
		}
		if b.IsRemote {
			m.pending = pendingDeleteRemoteBranch
			m.pendingItem = b
			m.setStatus(fmt.Sprintf("Delete %q on %s for everyone? (y/n)", b.LocalName(), b.Remote), true)
			return m, nil
		}
		m.doDelete(b.Name, key == "D")
		return m, changed
	case "m", "R":
		b, ok := m.selected()
		if !ok {
			return m, nil
		}
		if b.IsCurrent {
			m.setStatus("pick another branch than the current one", true)
			return m, nil
		}
		m.pendingName = b.Name
		if key == "m" {
			m.pending = pendingMerge
			m.setStatus(fmt.Sprintf("Merge %q into %s? (y/n)", b.Name, m.head.Label()), false)
		} else {
			m.pending = pendingRebase
			m.setStatus(fmt.Sprintf("Rebase %s onto %q? (y/n)", m.head.Label(), b.Name), false)
		}
	case "t":
		m.doTrack()
	case "P":
		return m, m.startOp(opPush, "", func() (string, error) { return Push(m.repo) })
	case "p":
		return m, m.startOp(opPull, "", func() (string, error) { return Pull(m.repo) })
	case "U":
		return m, m.startOp(opPullRebase, "", func() (string, error) { return PullRebase(m.repo) })
	}
	return m, nil
}

func (m BranchesPopup) updateRemotesKey(key string) (BranchesPopup, tea.Cmd) {
	switch key {
	case "a", "n":
		name := ""
		if len(m.remotes) == 0 {
			name = "origin"
		}
		return m, m.prompt(branchesInputRemoteName, name, "remote name")
	case "e":
		if r, ok := m.selectedRemote(); ok {
			m.draft = r.Name
			return m, m.prompt(branchesInputRemoteSetURL, r.FetchURL, "URL")
		}
	case "r":
		if r, ok := m.selectedRemote(); ok {
			m.draft = r.Name
			return m, m.prompt(branchesInputRemoteRename, r.Name, "new remote name")
		}
	case "d", "D":
		if r, ok := m.selectedRemote(); ok {
			m.pending = pendingRemoveRemote
			m.pendingName = r.Name
			m.setStatus(fmt.Sprintf("Remove remote %q and its remote branches? (y/n)", r.Name), true)
		}
	case "f":
		if r, ok := m.selectedRemote(); ok {
			name := r.Name
			return m, m.startOp(opFetchRemote, name, func() (string, error) { return FetchRemote(m.repo, name) })
		}
	case "o":
		m.doOpenRemote()
	case "enter", "l":
		// Show this remote's branches.
		if r, ok := m.selectedRemote(); ok {
			m.setView(viewBranches)
			m.filter = r.Name + "/"
			m.applyFilter()
			m.cursors[viewBranches] = 0
		}
	}
	return m, nil
}

func (m BranchesPopup) updateTagsKey(key string) (BranchesPopup, tea.Cmd) {
	switch key {
	case "n", "a":
		if m.head.Unborn {
			m.setStatus("nothing to tag — make the first commit", true)
			return m, nil
		}
		return m, m.prompt(branchesInputTagName, "", "tag name (at "+m.head.ShortSHA+")")
	case "d", "D":
		if t, ok := m.selectedTag(); ok {
			m.pending = pendingDeleteTag
			m.pendingName = t.Name
			m.setStatus(fmt.Sprintf("Delete tag %q locally? (y/n)", t.Name), true)
		}
	case "P":
		if t, ok := m.selectedTag(); ok {
			name := t.Name
			return m, m.startOp(opPushTag, name, func() (string, error) { return PushTag(m.repo, name) })
		}
	}
	return m, nil
}

// prompt focuses the text input for mode, prefilled with value.
func (m *BranchesPopup) prompt(mode branchesInputMode, value, placeholder string) tea.Cmd {
	m.inputMode = mode
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.input.Placeholder = placeholder
	return m.input.Focus()
}

func (m BranchesPopup) updateInput(msg tea.Msg) (BranchesPopup, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			mode := m.inputMode
			m.inputMode = branchesInputNone
			m.input.Blur()
			m.draft = ""
			if mode == branchesInputFilter {
				m.filter = ""
				m.applyFilter()
			}
			return m, nil
		case "enter":
			val := m.input.Value()
			mode := m.inputMode
			draft := m.draft
			m.inputMode = branchesInputNone
			m.input.Blur()
			m.draft = ""
			switch mode {
			case branchesInputCreate:
				m.doCreate(val, "")
				return m, changed
			case branchesInputCreateFrom:
				m.doCreate(val, draft)
				return m, changed
			case branchesInputRename:
				m.doRename(draft, val)
				return m, changed
			case branchesInputFilter:
				// already applied live; keep the filter
			case branchesInputRemoteName:
				name := strings.TrimSpace(val)
				if name == "" {
					m.setStatus("empty remote name", true)
					return m, nil
				}
				m.draft = name
				return m, m.prompt(branchesInputRemoteURL, "", "URL of "+name+" (git@host:user/repo.git or https://…)")
			case branchesInputRemoteURL:
				url := strings.TrimSpace(val)
				if url == "" {
					m.setStatus("empty URL", true)
					return m, nil
				}
				return m, tea.Batch(
					m.startOp(opAddRemote, draft, func() (string, error) { return AddRemote(m.repo, draft, url) }),
					changed,
				)
			case branchesInputRemoteSetURL:
				m.doSetRemoteURL(draft, val)
			case branchesInputRemoteRename:
				m.doRenameRemote(draft, val)
				return m, changed
			case branchesInputTagName:
				name := strings.TrimSpace(val)
				if name == "" {
					m.setStatus("empty tag name", true)
					return m, nil
				}
				m.draft = name
				return m, m.prompt(branchesInputTagMessage, "", "message (empty = lightweight tag)")
			case branchesInputTagMessage:
				m.doCreateTag(draft, val)
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	prev := m.input.Value()
	m.input, cmd = m.input.Update(msg)
	if m.inputMode == branchesInputFilter && m.input.Value() != prev {
		m.filter = m.input.Value()
		m.applyFilter()
	}
	return m, cmd
}

func (m BranchesPopup) updatePending(msg tea.Msg) (BranchesPopup, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	pending, name := m.pending, m.pendingName
	confirm := func() { m.pending = pendingNone; m.pendingName = "" }

	switch keyMsg.String() {
	case "y", "Y":
		confirm()
		switch pending {
		case pendingMerge:
			m.doMerge(name)
		case pendingRebase:
			m.doRebase(name)
		case pendingForceDelete:
			m.doDelete(name, true)
		case pendingDeleteRemoteBranch:
			b := m.pendingItem
			repo := m.repo
			return m, m.startOp(opDeleteRemoteBranch, b.Name, func() (string, error) { return DeleteRemoteBranch(repo, b) })
		case pendingRemoveRemote:
			m.doRemoveRemote(name)
		case pendingDeleteTag:
			m.doDeleteTag(name)
		case pendingAbort:
			m.doAbort()
		case pendingInit:
			m.doInit()
		}
		return m, changed
	case "D":
		if pending == pendingForceDelete {
			confirm()
			m.doDelete(name, true)
			return m, changed
		}
		return m, nil
	case "n", "N", "esc":
		confirm()
		m.setStatus("cancelled", false)
		return m, nil
	}
	return m, nil
}

// ── Rendering ─────────────────────────────────────────────────────────

// View renders the popup. The caller is expected to overlay this on top
// of its main UI (similar to other overlays in the app).
func (m BranchesPopup) View() string {
	if !m.active {
		return ""
	}

	w := clamp(m.width-4, 50, 110)
	h := clamp(m.height-2, 10, m.height)

	header := m.renderHeader(w)
	tabs := m.renderTabs(w)
	footer := m.renderFooter(w)
	// header(1) + tabs(1) + box top/bottom(2) + footer
	body := m.renderBody(w, h-4-lipgloss.Height(footer))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.AccentBlue).
		Width(w + 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, header, tabs, body, footer))
	return box
}

func (m BranchesPopup) renderHeader(w int) string {
	headerStyle := lipgloss.NewStyle().
		Background(theme.AccentBlue).
		Foreground(theme.BgColor).
		Bold(true)

	left := "  Git"
	if m.repo != "" {
		left += "   " + m.head.Label()
	}
	right := ""
	if m.head.Op != "" {
		right = " " + strings.ToUpper(m.head.Op) + " IN PROGRESS · C continue · A abort "
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
		right = truncatePlain(right, max(0, w-lipgloss.Width(left)-1))
	}
	out := headerStyle.Render(left + strings.Repeat(" ", gap))
	if right != "" {
		out += lipgloss.NewStyle().Background(theme.AccentRed).Foreground(theme.BgColor).Bold(true).Render(right)
	}
	return out
}

func (m BranchesPopup) renderTabs(w int) string {
	active := lipgloss.NewStyle().Foreground(theme.AccentBlue).Bold(true).Underline(true)
	inactive := lipgloss.NewStyle().Foreground(theme.FgDimColor)
	counts := []int{len(m.branches), len(m.remotes), len(m.tags)}

	var parts []string
	for i, t := range branchesViewTitles {
		label := fmt.Sprintf(" %s %d ", t, counts[i])
		if branchesView(i) == m.view {
			parts = append(parts, active.Render(label))
		} else {
			parts = append(parts, inactive.Render(label))
		}
	}
	return lipgloss.NewStyle().Width(w).Render(" " + strings.Join(parts, theme.Dim.Render("·")))
}

func (m BranchesPopup) renderBody(w, h int) string {
	if m.loadErr != nil {
		var lines []string
		if errors.Is(m.loadErr, ErrNotARepo) {
			lines = []string{
				"",
				theme.ErrorMsg.Render("  Not inside a git repository."),
				theme.Dim.Render("  Press i to run git init in " + truncatePlain(m.path, max(10, w-31)) + "."),
			}
		} else {
			lines = []string{"", theme.ErrorMsg.Render("  " + m.loadErr.Error())}
		}
		for len(lines) < h-2 {
			lines = append(lines, strings.Repeat(" ", w))
		}
		return strings.Join(append(lines[:max(0, h-2)], m.renderFilter(w), m.renderStatus(w)), "\n")
	}

	// Reserve 2 lines for filter + status / input (rendered after the list).
	listH := max(1, h-2)

	lines := []string{}
	total := m.visibleCount()
	if total == 0 {
		lines = append(lines, theme.Dim.Render("  "+m.emptyText()))
	} else {
		start, end := windowAround(m.cursor(), total, listH)
		for i := start; i < end; i++ {
			idx, _ := m.index(i)
			lines = append(lines, m.renderItem(idx, i == m.cursor(), w))
		}
	}

	for len(lines) < listH {
		lines = append(lines, strings.Repeat(" ", w))
	}
	if len(lines) > listH {
		lines = lines[:listH]
	}
	return strings.Join(append(lines, m.renderFilter(w), m.renderStatus(w)), "\n")
}

func (m BranchesPopup) emptyText() string {
	if m.filter != "" {
		return "(nothing matches)"
	}
	switch m.view {
	case viewRemotes:
		return "No remotes yet. Press a to add one (e.g. origin → git@github.com:you/repo.git)."
	case viewTags:
		return "No tags yet. Press n to tag HEAD."
	}
	return "(no branches)"
}

func (m BranchesPopup) renderItem(idx int, isCursor bool, w int) string {
	switch m.view {
	case viewRemotes:
		r := m.remotes[idx]
		right := r.FetchURL
		if r.PushURL != "" && r.PushURL != r.FetchURL {
			right += "  (push: " + r.PushURL + ")"
		}
		return renderRow(isCursor, "  ", r.Name, theme.AccentCyan, right, w)
	case viewTags:
		t := m.tags[idx]
		right := t.Date
		if t.Subject != "" {
			right = t.Subject + " · " + t.Date
		}
		return renderRow(isCursor, "  ", t.Name, theme.AccentYellow, right, w)
	}

	b := m.branches[idx]
	mark := "  "
	if b.IsCurrent {
		mark = " *"
	}
	nameFg := theme.FgColor
	if b.IsCurrent {
		nameFg = theme.AccentGreen
	} else if b.IsRemote {
		nameFg = theme.AccentCyan
	}
	var right []string
	if b.Upstream != "" {
		right = append(right, "→ "+b.Upstream)
	}
	switch {
	case b.Gone:
		right = append(right, "gone")
	case b.Ahead > 0 || b.Behind > 0:
		right = append(right, fmt.Sprintf("↑%d ↓%d", b.Ahead, b.Behind))
	}
	if b.Unborn {
		right = append(right, "no commits yet")
	}
	return renderRow(isCursor, mark, b.Name, nameFg, strings.Join(right, "  "), w)
}

// renderRow lays out one list row: mark, name, and dim right-hand info
// taking at most a third of the width.
func renderRow(isCursor bool, mark, name string, nameFg lipgloss.TerminalColor, rightInfo string, w int) string {
	apply := func(s lipgloss.Style) lipgloss.Style {
		if isCursor {
			return s.Background(theme.BgSelected).Bold(true)
		}
		return s
	}

	const (
		spaceLead  = 1
		markW      = 2
		sep1       = 1
		spaceTrail = 1
	)
	rightW := lipgloss.Width(rightInfo)
	if rightW > w/2 {
		rightW = w / 2
	}
	sep2 := 0
	if rightInfo != "" {
		sep2 = 2
	}
	nameW := w - (spaceLead + markW + sep1 + sep2 + rightW + spaceTrail)
	if nameW < 1 {
		nameW = 1
	}

	cells := []string{
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", spaceLead)),
		apply(lipgloss.NewStyle().Foreground(theme.AccentGreen).Bold(true)).
			Width(markW).Render(mark),
		apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", sep1)),
		apply(lipgloss.NewStyle().Foreground(nameFg)).
			Width(nameW).Render(truncatePlain(name, nameW)),
	}
	if rightInfo != "" {
		cells = append(cells,
			apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", sep2)),
			apply(lipgloss.NewStyle().Foreground(theme.FgDimColor)).
				Width(rightW).Render(truncatePlain(rightInfo, rightW)),
		)
	}
	cells = append(cells, apply(lipgloss.NewStyle()).Render(strings.Repeat(" ", spaceTrail)))
	return strings.Join(cells, "")
}

func (m BranchesPopup) renderFilter(w int) string {
	if m.inputMode == branchesInputFilter {
		return lipgloss.NewStyle().
			Foreground(theme.AccentYellow).
			Bold(true).
			Width(w).
			Render(" /" + m.input.View())
	}
	if m.filter != "" {
		return lipgloss.NewStyle().
			Foreground(theme.AccentYellow).
			Width(w).
			Render(" filter: " + m.filter)
	}
	return strings.Repeat(" ", w)
}

var inputLabels = map[branchesInputMode]string{
	branchesInputCreate:       " Create branch: ",
	branchesInputCreateFrom:   " Create branch: ",
	branchesInputRename:       " New name: ",
	branchesInputRemoteName:   " Remote name: ",
	branchesInputRemoteURL:    " Remote URL: ",
	branchesInputRemoteSetURL: " New URL: ",
	branchesInputRemoteRename: " New remote name: ",
	branchesInputTagName:      " Tag name: ",
	branchesInputTagMessage:   " Tag message: ",
}

func (m BranchesPopup) renderStatus(w int) string {
	if label, ok := inputLabels[m.inputMode]; ok {
		return lipgloss.NewStyle().
			Foreground(theme.AccentYellow).
			Bold(true).
			Width(w).
			Render(label + m.input.View() + theme.Dim.Render("    Enter · Esc"))
	}
	if m.status == "" {
		return strings.Repeat(" ", w)
	}
	style := lipgloss.NewStyle().Foreground(theme.AccentGreen)
	if m.statusErr {
		style = lipgloss.NewStyle().Foreground(theme.AccentRed)
	}
	return style.Width(w).Render(" " + truncatePlain(m.status, max(1, w-1)))
}

func (m BranchesPopup) renderFooter(w int) string {
	var hints []string
	switch {
	case m.loadErr != nil:
		hints = []string{"i git init", "Esc close"}
	case m.view == viewRemotes:
		hints = []string{"a add", "e edit URL", "r rename", "d remove", "f fetch", "o open in browser", "Enter its branches", "Tab view", "Esc close"}
	case m.view == viewTags:
		hints = []string{"n new tag", "d delete", "P push tag", "F fetch all", "/ filter", "Tab view", "Esc close"}
	default:
		hints = []string{"Enter checkout", "n/N new", "r rename", "d/D delete", "m merge", "R rebase", "t track", "P push", "p/U pull", "F fetch", "/ filter", "Tab view", "Esc close"}
	}

	// Wrap the hints onto as many lines as the width needs.
	var lines []string
	line := ""
	for _, h := range hints {
		next := line + " " + h + " ·"
		if line != "" && lipgloss.Width(next) > w {
			lines = append(lines, strings.TrimSuffix(line, " ·"))
			next = " " + h + " ·"
		}
		line = next
	}
	lines = append(lines, strings.TrimSuffix(line, " ·"))

	style := lipgloss.NewStyle().
		Foreground(theme.FgDimColor).
		Background(theme.BgDarkColor).
		Width(w)
	for i, l := range lines {
		lines[i] = style.Render(truncatePlain(l, w))
	}
	return strings.Join(lines, "\n")
}
