package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ── Types ──────────────────────────────────────────────────────────────

// FileChange represents a single entry from `git status --porcelain=v1`.
//
// IndexStatus is the staged half of the two-letter porcelain code, and
// WorktreeStatus is the unstaged half. Either may be ' ' (clean).
// '?' on both halves means untracked; '!' means ignored.
type FileChange struct {
	Path           string
	OldPath        string // populated for renames/copies
	IndexStatus    byte
	WorktreeStatus byte
}

// Staged reports whether the change has any staged component.
func (c FileChange) Staged() bool {
	return c.IndexStatus != ' ' && c.IndexStatus != '?'
}

// Unstaged reports whether the change has any worktree-only component.
func (c FileChange) Unstaged() bool {
	return c.WorktreeStatus != ' '
}

// Untracked reports whether the file is untracked.
func (c FileChange) Untracked() bool {
	return c.IndexStatus == '?' && c.WorktreeStatus == '?'
}

// Commit is a single entry from `git log`.
type Commit struct {
	Hash     string
	ShortSHA string
	Author   string
	RelDate  string
	Subject  string

	// Filled by Log only (the graph needs them).
	Parents []string
	Refs    []string // decorations: "HEAD -> main", "origin/main", "tag: v1"
	Date    string   // "13.05.2026 13:54"
}

// IsMerge reports whether the commit has more than one parent.
func (c Commit) IsMerge() bool { return len(c.Parents) > 1 }

// Stash is a single entry from `git stash list`.
type Stash struct {
	Index   int    // 0 == stash@{0}
	Ref     string // e.g. "stash@{0}"
	Subject string
}

// Branch is a single entry from `git for-each-ref`.
type Branch struct {
	Name      string // short name; for remote branches includes the remote (e.g. "origin/main")
	IsCurrent bool   // true for the branch currently checked out (HEAD)
	IsRemote  bool   // true for refs under refs/remotes
	Remote    string // remote name for remote branches ("origin"); empty for local ones
	Upstream  string // upstream tracking branch (short); empty if none
	Ahead     int    // commits ahead of Upstream
	Behind    int    // commits behind Upstream
	Gone      bool   // Upstream is configured but no longer exists on the remote
	Unborn    bool   // current branch of a repository with no commits yet
	Subject   string // subject of the commit pointed at by the ref
}

// LocalName strips the remote prefix from a remote branch name so it can
// be used to create a local tracking branch. For local branches it just
// returns Name.
func (b Branch) LocalName() string {
	if !b.IsRemote {
		return b.Name
	}
	if b.Remote != "" {
		return strings.TrimPrefix(b.Name, b.Remote+"/")
	}
	if i := strings.IndexByte(b.Name, '/'); i >= 0 && i < len(b.Name)-1 {
		return b.Name[i+1:]
	}
	return b.Name
}

// Head describes what HEAD points at and whether an operation that
// needs finishing (merge, rebase, cherry-pick, revert) is in progress.
type Head struct {
	Branch   string // current branch; empty when detached
	Detached bool
	Unborn   bool   // the branch has no commits yet (fresh `git init`)
	ShortSHA string // commit HEAD points at; empty when unborn
	Op       string // "merge", "rebase", "cherry-pick", "revert" or ""
}

// Label is a one-word description of HEAD for headers.
func (h Head) Label() string {
	switch {
	case h.Detached:
		return "detached at " + h.ShortSHA
	case h.Unborn:
		return h.Branch + " (no commits yet)"
	}
	return h.Branch
}

// TrackingInfo holds ahead/behind counts vs. the upstream branch.
type TrackingInfo struct {
	Upstream string
	Ahead    int
	Behind   int
}

// ── Queries ────────────────────────────────────────────────────────────

// FindRoot returns the repository top-level for path, or ErrNotARepo.
func FindRoot(path string) (string, error) {
	return run(path, "rev-parse", "--show-toplevel")
}

// CurrentBranch returns the current branch name, or "HEAD" if detached.
// A branch with no commits yet (right after `git init`) is still named.
func CurrentBranch(repo string) (string, error) {
	out, err := run(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err == nil {
		return out, nil
	}
	if name, serr := run(repo, "symbolic-ref", "--short", "-q", "HEAD"); serr == nil && name != "" {
		return name, nil
	}
	return "", err
}

// HeadState reports the branch, detached / unborn state and any
// in-progress merge, rebase, cherry-pick or revert.
func HeadState(repo string) Head {
	var h Head
	if name, err := run(repo, "symbolic-ref", "--short", "-q", "HEAD"); err == nil && name != "" {
		h.Branch = name
	} else {
		h.Detached = true
	}
	if sha, err := run(repo, "rev-parse", "--short", "-q", "--verify", "HEAD"); err == nil && sha != "" {
		h.ShortSHA = sha
	} else {
		h.Unborn = true
		h.Detached = false
	}
	markers := []struct{ path, op string }{
		{"rebase-merge", "rebase"},
		{"rebase-apply", "rebase"},
		{"MERGE_HEAD", "merge"},
		{"CHERRY_PICK_HEAD", "cherry-pick"},
		{"REVERT_HEAD", "revert"},
	}
	for _, mk := range markers {
		p, err := run(repo, "rev-parse", "--path-format=absolute", "--git-path", mk.path)
		if err != nil {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			h.Op = mk.op
			break
		}
	}
	return h
}

// AbortOp aborts the in-progress operation reported by HeadState.
func AbortOp(repo, op string) (string, error) {
	if op == "" {
		return "", fmt.Errorf("nothing to abort")
	}
	return runWithOutput(repo, op, "--abort")
}

// ContinueOp continues the in-progress operation after conflicts were
// resolved and staged. runWithOutput suppresses the editor, so git keeps
// its default message instead of hanging on an invisible editor.
func ContinueOp(repo, op string) (string, error) {
	if op == "" {
		return "", fmt.Errorf("nothing to continue")
	}
	return runWithOutput(repo, op, "--continue")
}

// Tracking returns upstream / ahead / behind for the current branch.
// If there is no upstream configured the returned Upstream is empty and
// counts are zero (no error).
func Tracking(repo string) (TrackingInfo, error) {
	upstream, err := run(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		// No upstream is not a hard error.
		return TrackingInfo{}, nil
	}
	counts, err := run(repo, "rev-list", "--left-right", "--count", "HEAD..."+upstream)
	if err != nil {
		return TrackingInfo{Upstream: upstream}, nil
	}
	parts := strings.Fields(counts)
	info := TrackingInfo{Upstream: upstream}
	if len(parts) == 2 {
		info.Ahead, _ = strconv.Atoi(parts[0])
		info.Behind, _ = strconv.Atoi(parts[1])
	}
	return info, nil
}

// Status parses `git status --porcelain=v1` into FileChange entries.
func Status(repo string) ([]FileChange, error) {
	out, err := run(repo, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var changes []FileChange
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		c := FileChange{
			IndexStatus:    line[0],
			WorktreeStatus: line[1],
		}
		rest := line[3:]
		if c.IndexStatus == 'R' || c.IndexStatus == 'C' {
			// Format: "old -> new"
			if i := strings.Index(rest, " -> "); i >= 0 {
				c.OldPath = rest[:i]
				c.Path = rest[i+4:]
			} else {
				c.Path = rest
			}
		} else {
			c.Path = rest
		}
		changes = append(changes, c)
	}
	return changes, nil
}

// Diff returns the diff for a single path. If staged is true the diff is
// taken against the index (i.e. what is staged for commit); otherwise it
// is the working-tree diff.
//
// For untracked files git would normally emit nothing, so we fall back to
// `git diff --no-index /dev/null <path>` style output by treating the file
// as fully added.
func Diff(repo, path string, staged bool) (string, error) {
	args := []string{"diff", "--no-color"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	out, err := run(repo, args...)
	return out, err
}

// DiffUntracked returns a synthetic "all-added" diff for an untracked file.
func DiffUntracked(repo, path string) (string, error) {
	// /dev/null is portable on Unix; on Windows users would need NUL.
	// tuiple is primarily a unix TUI so this is acceptable for now.
	out, err := run(repo, "diff", "--no-color", "--no-index", "--", "/dev/null", path)
	// git diff --no-index exits 1 when files differ, which is the normal case.
	if err != nil && out != "" {
		err = nil
	}
	return out, err
}

// Log returns the most recent `limit` commits, children before parents,
// with parents and ref decorations for the graph. With all set it covers
// every branch, remote branch and tag; otherwise only what HEAD reaches.
func Log(repo string, limit int, all bool) ([]Commit, error) {
	if limit <= 0 {
		limit = 200
	}
	format := "%H%x00%h%x00%P%x00%an%x00%ar%x00%ad%x00%D%x00%s"
	args := []string{"log",
		"--no-color",
		"--date-order",
		"--date=format:%d.%m.%Y %H:%M",
		"--decorate=short",
		"-n", strconv.Itoa(limit),
		"--format=" + format,
	}
	if all {
		args = append(args, "--branches", "--remotes", "--tags", "HEAD")
	}
	out, err := run(repo, args...)
	if err != nil {
		// An unborn branch has no log yet; that is not an error.
		if h := HeadState(repo); h.Unborn {
			return nil, nil
		}
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\x00", 8)
		if len(parts) < 8 {
			continue
		}
		c := Commit{
			Hash:     parts[0],
			ShortSHA: parts[1],
			Parents:  strings.Fields(parts[2]),
			Author:   parts[3],
			RelDate:  parts[4],
			Date:     parts[5],
			Subject:  parts[7],
		}
		for _, ref := range strings.Split(parts[6], ", ") {
			if ref != "" && !strings.HasSuffix(ref, "/HEAD") {
				c.Refs = append(c.Refs, ref)
			}
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// CommitDetail returns `git show` output for the given commit, including
// the stat summary and full patch. Used to render the right pane of the
// Log tab.
func CommitDetail(repo, hash string) (string, error) {
	return run(repo, "show", "--no-color", "--stat", "--patch", hash)
}

// ── Branch operations ─────────────────────────────────────────────────

// ListBranches returns all local and remote branches. Remote symbolic
// refs (origin/HEAD etc.) are filtered out. In a repository without
// commits the unborn current branch is listed too, so the popup is
// never empty right after `git init`.
func ListBranches(repo string) ([]Branch, error) {
	out, err := run(repo, "for-each-ref",
		"--format=%(refname)%00%(HEAD)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(contents:subject)",
		"refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	remotes, _ := RemoteNames(repo)

	var branches []Branch
	hasCurrent := false
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\x00", 5)
		if len(parts) < 5 {
			continue
		}
		fullName := parts[0]
		b := Branch{
			IsCurrent: parts[1] == "*",
			IsRemote:  strings.HasPrefix(fullName, "refs/remotes/"),
			Upstream:  parts[2],
			Subject:   parts[4],
		}
		if b.IsRemote {
			b.Name = strings.TrimPrefix(fullName, "refs/remotes/")
			b.Remote = remoteOf(b.Name, remotes)
			if strings.HasSuffix(b.Name, "/HEAD") || b.Name == b.Remote {
				continue
			}
		} else {
			b.Name = strings.TrimPrefix(fullName, "refs/heads/")
		}
		b.Ahead, b.Behind, b.Gone = parseTrack(parts[3])
		hasCurrent = hasCurrent || b.IsCurrent
		branches = append(branches, b)
	}

	if !hasCurrent {
		if h := HeadState(repo); h.Unborn && h.Branch != "" {
			branches = append([]Branch{{Name: h.Branch, IsCurrent: true, Unborn: true}}, branches...)
		}
	}
	return branches, nil
}

// remoteOf picks the remote a remote-tracking ref belongs to. Remote
// names may contain slashes, so the longest matching prefix wins.
func remoteOf(name string, remotes []string) string {
	best := ""
	for _, r := range remotes {
		if strings.HasPrefix(name, r+"/") && len(r) > len(best) {
			best = r
		}
	}
	if best == "" {
		if i := strings.IndexByte(name, '/'); i > 0 {
			best = name[:i]
		}
	}
	return best
}

// parseTrack reads %(upstream:track,nobracket): "ahead 1, behind 2", "gone" or "".
func parseTrack(s string) (ahead, behind int, gone bool) {
	for _, part := range strings.Split(s, ",") {
		f := strings.Fields(part)
		switch {
		case len(f) == 1 && f[0] == "gone":
			gone = true
		case len(f) == 2 && f[0] == "ahead":
			ahead, _ = strconv.Atoi(f[1])
		case len(f) == 2 && f[0] == "behind":
			behind, _ = strconv.Atoi(f[1])
		}
	}
	return ahead, behind, gone
}

// Switch checks out the local branch `name`.
func Switch(repo, name string) error {
	_, err := run(repo, "switch", name)
	return err
}

// SwitchTo checks out b. For a remote branch it switches to the local
// branch of the same name when one exists, and otherwise creates it
// tracking b — explicitly, so it also works when several remotes carry
// a branch with that name (plain `git switch` refuses then).
func SwitchTo(repo string, b Branch) (string, error) {
	if !b.IsRemote {
		return b.Name, Switch(repo, b.Name)
	}
	local := b.LocalName()
	if _, err := run(repo, "rev-parse", "-q", "--verify", "refs/heads/"+local); err == nil {
		return local, Switch(repo, local)
	}
	_, err := run(repo, "switch", "-c", local, "--track", b.Name)
	return local, err
}

// CreateBranchFrom creates `name` at `start` and switches to it.
func CreateBranchFrom(repo, name, start string) error {
	_, err := run(repo, "switch", "-c", name, start)
	return err
}

// SetUpstream makes `upstream` (e.g. "origin/main") the tracking branch
// of the local branch `branch`.
func SetUpstream(repo, branch, upstream string) error {
	_, err := run(repo, "branch", "--set-upstream-to="+upstream, branch)
	return err
}

// UnsetUpstream removes the tracking branch of `branch`.
func UnsetUpstream(repo, branch string) error {
	_, err := run(repo, "branch", "--unset-upstream", branch)
	return err
}

// CreateAndSwitch creates a new branch from the current HEAD and switches
// to it (`git switch -c <name>`).
func CreateAndSwitch(repo, name string) error {
	_, err := run(repo, "switch", "-c", name)
	return err
}

// DeleteBranch deletes a local branch. If force is true, `-D` is used and
// the branch is removed even if it is not merged.
func DeleteBranch(repo, name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := run(repo, "branch", flag, name)
	return err
}

// DeleteRemoteBranch deletes the remote branch b on its remote.
func DeleteRemoteBranch(repo string, b Branch) (string, error) {
	if !b.IsRemote || b.Remote == "" {
		return "", fmt.Errorf("not a remote branch: %q", b.Name)
	}
	return runWithOutput(repo, "push", b.Remote, "--delete", b.LocalName())
}

// RenameBranch renames a local branch. If `from` is empty the current
// branch is renamed.
func RenameBranch(repo, from, to string) error {
	args := []string{"branch", "-m"}
	if from != "" {
		args = append(args, from)
	}
	args = append(args, to)
	_, err := run(repo, args...)
	return err
}

// MergeBranch merges `name` into the current branch.
func MergeBranch(repo, name string) error {
	_, err := run(repo, "merge", "--no-edit", name)
	return err
}

// RebaseOnto rebases the current branch onto `name`.
func RebaseOnto(repo, name string) error {
	_, err := run(repo, "rebase", name)
	return err
}

// ── Per-file operations ───────────────────────────────────────────────

// StageFile stages a single file (path relative to repo root).
func StageFile(repo, path string) error {
	_, err := run(repo, "add", "--", path)
	return err
}

// UnstageFile removes a file from the staging area without discarding changes.
func UnstageFile(repo, path string) error {
	_, err := run(repo, "restore", "--staged", "--", path)
	return err
}

// DiscardFile discards working-tree changes for path (relative to repo root).
// Untracked files cannot be discarded this way.
func DiscardFile(repo, path string) error {
	_, err := run(repo, "checkout", "--", path)
	return err
}

// AddToGitIgnore toggles relPath in the repo's top-level .gitignore:
// if the path is already present as its own line it is removed,
// otherwise it is appended as a new line (ensuring the file ends with \n
// before writing so we never join to an existing non-terminated line).
//
// For directories, the entry is written with a trailing slash (e.g.
// "dist/") so that git ignores the directory and all its contents.
func AddToGitIgnore(repo, relPath string) (added bool, err error) {
	p := filepath.Join(repo, ".gitignore")

	// Normalise: directories get a trailing slash so git ignores their
	// contents too. Strip any existing trailing slash first to avoid
	// double-slashing on repeated calls.
	relPath = strings.TrimRight(relPath, "/")
	if info, statErr := os.Stat(filepath.Join(repo, relPath)); statErr == nil && info.IsDir() {
		relPath = relPath + "/"
	}

	// Read existing content (treat a missing file as empty).
	raw, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	content := string(raw)

	// Check whether relPath is already a line in the file.
	// Also match the bare path without trailing slash in case it was
	// added previously without one.
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == relPath || trimmed == strings.TrimRight(relPath, "/") {
			// Remove that line.
			lines = append(lines[:i], lines[i+1:]...)
			return false, os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0644)
		}
	}

	// Not present — append, guaranteeing a newline boundary.
	var buf strings.Builder
	buf.WriteString(content)
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		buf.WriteByte('\n')
	}
	buf.WriteString(relPath)
	buf.WriteByte('\n')
	return true, os.WriteFile(p, []byte(buf.String()), 0644)
}

// LogFile returns the most recent limit commits that touch path.
func LogFile(repo, path string, limit int) ([]Commit, error) {
	if limit <= 0 {
		limit = 200
	}
	format := "%H%x00%h%x00%an%x00%ar%x00%s"
	out, err := run(repo, "log", "--no-color", "-n", strconv.Itoa(limit),
		"--format="+format, "--", path)
	if err != nil || out == "" {
		return nil, err
	}
	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\x00", 5)
		if len(parts) < 5 {
			continue
		}
		commits = append(commits, Commit{
			Hash: parts[0], ShortSHA: parts[1],
			Author: parts[2], RelDate: parts[3], Subject: parts[4],
		})
	}
	return commits, nil
}

// BlameFile returns `git blame` output for path.
// Uses `-c color.ui=never` because `git blame` does not accept `--no-color`.
func BlameFile(repo, path string) (string, error) {
	return run(repo, "-c", "color.ui=never", "blame", path)
}

// CommitFileDetail returns `git show` filtered to a single path.
func CommitFileDetail(repo, hash, path string) (string, error) {
	return run(repo, "show", "--no-color", "--stat", "--patch", hash, "--", path)
}

// ResetTo resets HEAD to hash using mode ("soft", "mixed", or "hard").
func ResetTo(repo, hash, mode string) error {
	_, err := run(repo, "reset", "--"+mode, hash)
	return err
}

// Push pushes the current branch to its upstream remote and returns the
// combined stdout+stderr so callers can show a meaningful status message.
//
// When the current branch has no upstream configured yet (typical for a
// branch that was just created locally), `git push` errors out telling
// the user to rerun with --set-upstream.  Rather than dump that whole
// hint into the status bar we detect the case and transparently rerun
// the push with --set-upstream to the branch's push remote (see
// PushRemote), so it also works when the remote is not called "origin".
func Push(repo string) (string, error) {
	h := HeadState(repo)
	if h.Detached {
		return "", fmt.Errorf("HEAD is detached — check out a branch to push")
	}
	if h.Unborn {
		return "", fmt.Errorf("nothing to push — make the first commit")
	}
	if names, _ := RemoteNames(repo); len(names) == 0 {
		return "", ErrNoRemote
	}
	out, err := runWithOutput(repo, "push")
	if err == nil {
		return out, nil
	}
	if !strings.Contains(out, "has no upstream branch") &&
		!strings.Contains(out, "set-upstream") {
		return out, err
	}
	remote, rerr := PushRemote(repo, h.Branch)
	if rerr != nil {
		return out, rerr
	}
	return runWithOutput(repo, "push", "--set-upstream", remote, h.Branch)
}

// Pull pulls from the upstream remote (merging or rebasing, as the
// user's pull.rebase says) and returns combined output for status
// reporting.
func Pull(repo string) (string, error) {
	return runWithOutput(repo, "pull")
}

// PullRebase pulls and rebases local commits on top of the upstream —
// the way out when a plain pull reports divergent branches.
func PullRebase(repo string) (string, error) {
	return runWithOutput(repo, "pull", "--rebase")
}

// Fetch fetches from all remotes and returns combined output for status
// reporting.
func Fetch(repo string) (string, error) {
	if names, _ := RemoteNames(repo); len(names) == 0 {
		return "", ErrNoRemote
	}
	return runWithOutput(repo, "fetch", "--all", "--prune")
}

// StashShow returns the patch for a single stash entry (e.g. "stash@{0}").
func StashShow(repo, ref string) (string, error) {
	return run(repo, "stash", "show", "--no-color", "-p", ref)
}

// StashCreate stashes the current working tree changes with an optional
// message. If msg is empty, git uses its default WIP message.
func StashCreate(repo, msg string) error {
	args := []string{"stash", "push"}
	if strings.TrimSpace(msg) != "" {
		args = append(args, "-m", msg)
	}
	_, err := run(repo, args...)
	return err
}

// StashApply applies a stash without removing it from the list.
func StashApply(repo, ref string) error {
	_, err := run(repo, "stash", "apply", ref)
	return err
}

// StashPop applies a stash and removes it from the list on success.
func StashPop(repo, ref string) error {
	_, err := run(repo, "stash", "pop", ref)
	return err
}

// StashDrop removes a stash without applying it.
func StashDrop(repo, ref string) error {
	_, err := run(repo, "stash", "drop", ref)
	return err
}

// Stashes returns the stash list.
func Stashes(repo string) ([]Stash, error) {
	out, err := run(repo, "stash", "list", "--format=%gd%x00%gs")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var stashes []Stash
	for i, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\x00", 2)
		if len(parts) < 2 {
			continue
		}
		stashes = append(stashes, Stash{
			Index:   i,
			Ref:     parts[0],
			Subject: parts[1],
		})
	}
	return stashes, nil
}
