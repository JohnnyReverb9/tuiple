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
}

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
	Upstream  string // upstream tracking branch (short); empty if none
	Subject   string // subject of the commit pointed at by the ref
}

// LocalName strips the remote prefix from a remote branch name so it can
// be used to create a local tracking branch. For local branches it just
// returns Name.
func (b Branch) LocalName() string {
	if !b.IsRemote {
		return b.Name
	}
	if i := strings.IndexByte(b.Name, '/'); i >= 0 && i < len(b.Name)-1 {
		return b.Name[i+1:]
	}
	return b.Name
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
func CurrentBranch(repo string) (string, error) {
	out, err := run(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return out, nil
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

// Log returns the most recent `limit` commits on the current branch.
func Log(repo string, limit int) ([]Commit, error) {
	if limit <= 0 {
		limit = 200
	}
	format := "%H%x00%h%x00%an%x00%ar%x00%s"
	out, err := run(repo, "log",
		"--no-color",
		"-n", strconv.Itoa(limit),
		"--format="+format,
	)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\x00", 5)
		if len(parts) < 5 {
			continue
		}
		commits = append(commits, Commit{
			Hash:     parts[0],
			ShortSHA: parts[1],
			Author:   parts[2],
			RelDate:  parts[3],
			Subject:  parts[4],
		})
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
// refs (origin/HEAD etc.) are filtered out.
func ListBranches(repo string) ([]Branch, error) {
	out, err := run(repo, "for-each-ref",
		"--format=%(refname)|%(HEAD)|%(upstream:short)|%(contents:subject)",
		"refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var branches []Branch
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		fullName := parts[0]
		isRemote := strings.HasPrefix(fullName, "refs/remotes/")
		var name string
		switch {
		case isRemote:
			name = strings.TrimPrefix(fullName, "refs/remotes/")
		default:
			name = strings.TrimPrefix(fullName, "refs/heads/")
		}
		if strings.HasSuffix(name, "/HEAD") {
			continue
		}
		branches = append(branches, Branch{
			Name:      name,
			IsCurrent: parts[1] == "*",
			IsRemote:  isRemote,
			Upstream:  parts[2],
			Subject:   parts[3],
		})
	}
	return branches, nil
}

// Switch checks out `name`. Uses `git switch`, which auto-creates a local
// tracking branch when `name` matches a single upstream.
func Switch(repo, name string) error {
	_, err := run(repo, "switch", name)
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

// DeleteRemoteBranch deletes a branch on the remote.  `fullName` is the
// short form as returned by ListBranches for remote refs, i.e.
// "<remote>/<branch>" (e.g. "origin/feature/foo").  Splitting it back into
// its remote and branch parts is done here so callers don't have to.
func DeleteRemoteBranch(repo, fullName string) (string, error) {
	i := strings.IndexByte(fullName, '/')
	if i <= 0 || i >= len(fullName)-1 {
		return "", fmt.Errorf("not a remote branch: %q", fullName)
	}
	remote, branch := fullName[:i], fullName[i+1:]
	return runWithOutput(repo, "push", remote, "--delete", branch)
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

// unused sentinel to satisfy the fmt import used elsewhere in this file.
var _ = fmt.Sprintf

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
// the push with --set-upstream origin <branch>.
func Push(repo string) (string, error) {
	out, err := runWithOutput(repo, "push")
	if err == nil {
		return out, nil
	}
	if !strings.Contains(out, "has no upstream branch") &&
		!strings.Contains(out, "set-upstream") {
		return out, err
	}
	branch, berr := CurrentBranch(repo)
	if berr != nil || branch == "" || branch == "HEAD" {
		return out, err
	}
	return runWithOutput(repo, "push", "--set-upstream", "origin", branch)
}

// Pull pulls (fetch + merge) from the upstream remote and returns combined
// output for status reporting.
func Pull(repo string) (string, error) {
	return runWithOutput(repo, "pull")
}

// Fetch fetches from all remotes and returns combined output for status
// reporting.
func Fetch(repo string) (string, error) {
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
