package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolate keeps the user's git config (signing, hooks, aliases) out of
// the tests and gives commits a fixed identity.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
}

func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo creates a repository on branch main with one commit.
func newRepo(t *testing.T) string {
	t.Helper()
	isolate(t)
	dir := t.TempDir()
	sh(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "a.txt", "one\n")
	sh(t, dir, "add", ".")
	sh(t, dir, "commit", "-q", "-m", "first")
	return dir
}

// newBare creates a bare repository to act as a remote.
func newBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sh(t, dir, "init", "-q", "--bare", "-b", "main")
	return dir
}

func current(t *testing.T, dir string) string {
	t.Helper()
	return sh(t, dir, "symbolic-ref", "--short", "HEAD")
}

func TestInitAndUnbornBranch(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	if _, err := FindRoot(dir); !errors.Is(err, ErrNotARepo) {
		t.Fatalf("FindRoot before init: %v", err)
	}
	root, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if root == "" {
		t.Fatal("empty root")
	}

	name, err := CurrentBranch(root)
	if err != nil || name == "" {
		t.Fatalf("CurrentBranch on unborn branch = %q, %v", name, err)
	}
	h := HeadState(root)
	if !h.Unborn || h.Detached || h.Branch != name {
		t.Fatalf("HeadState = %+v", h)
	}
	branches, err := ListBranches(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || !branches[0].Unborn || !branches[0].IsCurrent || branches[0].Name != name {
		t.Fatalf("ListBranches = %+v", branches)
	}
	if _, err := Push(root); err == nil {
		t.Fatal("Push on unborn branch should fail")
	}
}

func TestDetachedHead(t *testing.T) {
	dir := newRepo(t)
	sh(t, dir, "switch", "-q", "--detach", "HEAD")
	h := HeadState(dir)
	if !h.Detached || h.Unborn || h.ShortSHA == "" {
		t.Fatalf("HeadState = %+v", h)
	}
	if !strings.HasPrefix(h.Label(), "detached at ") {
		t.Fatalf("Label = %q", h.Label())
	}
}

func TestNoRemote(t *testing.T) {
	dir := newRepo(t)
	if _, err := Push(dir); !errors.Is(err, ErrNoRemote) {
		t.Fatalf("Push: %v", err)
	}
	if _, err := Fetch(dir); !errors.Is(err, ErrNoRemote) {
		t.Fatalf("Fetch: %v", err)
	}
	if got := shortGitError(ErrNoRemote); !strings.Contains(got, "Remotes") {
		t.Fatalf("shortGitError = %q", got)
	}
}

func TestRemotesCRUD(t *testing.T) {
	dir := newRepo(t)
	bare := newBare(t)

	if _, err := AddRemote(dir, "upstream", bare); err != nil {
		t.Fatal(err)
	}
	remotes, err := Remotes(dir)
	if err != nil || len(remotes) != 1 || remotes[0].Name != "upstream" || remotes[0].FetchURL != bare {
		t.Fatalf("Remotes = %+v, %v", remotes, err)
	}
	if err := SetRemoteURL(dir, "upstream", "git@github.com:me/repo.git"); err != nil {
		t.Fatal(err)
	}
	if err := RenameRemote(dir, "upstream", "gh"); err != nil {
		t.Fatal(err)
	}
	remotes, _ = Remotes(dir)
	if len(remotes) != 1 || remotes[0].Name != "gh" || remotes[0].FetchURL != "git@github.com:me/repo.git" {
		t.Fatalf("after edit = %+v", remotes)
	}
	if err := RemoveRemote(dir, "gh"); err != nil {
		t.Fatal(err)
	}
	if names, _ := RemoteNames(dir); len(names) != 0 {
		t.Fatalf("after remove = %v", names)
	}
}

// A new branch pushed to a remote that is not called "origin" gets its
// upstream set to that remote.
func TestPushNewBranchToNonOriginRemote(t *testing.T) {
	dir := newRepo(t)
	bare := newBare(t)
	sh(t, dir, "remote", "add", "upstream", bare)
	sh(t, dir, "switch", "-q", "-c", "feature")

	if _, err := Push(dir); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if got := sh(t, dir, "rev-parse", "--abbrev-ref", "@{u}"); got != "upstream/feature" {
		t.Fatalf("upstream = %q", got)
	}
	info, _ := Tracking(dir)
	if info.Ahead != 0 || info.Behind != 0 {
		t.Fatalf("tracking = %+v", info)
	}
}

// Switching to a remote branch that exists on two remotes creates a local
// branch tracking the one picked (plain `git switch` refuses here).
func TestSwitchToRemoteBranchOnTwoRemotes(t *testing.T) {
	src := newRepo(t)
	sh(t, src, "switch", "-q", "-c", "feature")
	write(t, src, "b.txt", "feature\n")
	sh(t, src, "add", ".")
	sh(t, src, "commit", "-q", "-m", "feature")

	dir := newRepo(t)
	sh(t, dir, "remote", "add", "a", src)
	sh(t, dir, "remote", "add", "b", src)
	sh(t, dir, "fetch", "-q", "--all")

	branches, err := ListBranches(dir)
	if err != nil {
		t.Fatal(err)
	}
	var target Branch
	for _, b := range branches {
		if b.Name == "b/feature" {
			target = b
		}
	}
	if target.Remote != "b" || target.LocalName() != "feature" {
		t.Fatalf("remote branch = %+v", target)
	}

	local, err := SwitchTo(dir, target)
	if err != nil {
		t.Fatalf("SwitchTo: %v", err)
	}
	if local != "feature" || current(t, dir) != "feature" {
		t.Fatalf("on %q, want feature", current(t, dir))
	}
	if got := sh(t, dir, "rev-parse", "--abbrev-ref", "@{u}"); got != "b/feature" {
		t.Fatalf("upstream = %q", got)
	}

	// Second time: the local branch exists, so it is simply checked out.
	sh(t, dir, "switch", "-q", "main")
	if _, err := SwitchTo(dir, target); err != nil || current(t, dir) != "feature" {
		t.Fatalf("SwitchTo existing: %v, on %q", err, current(t, dir))
	}
}

func TestAheadBehindAndGone(t *testing.T) {
	dir := newRepo(t)
	bare := newBare(t)
	sh(t, dir, "remote", "add", "origin", bare)
	sh(t, dir, "push", "-q", "-u", "origin", "main")
	write(t, dir, "a.txt", "two\n")
	sh(t, dir, "commit", "-q", "-am", "second")

	sh(t, dir, "switch", "-q", "-c", "old")
	sh(t, dir, "push", "-q", "-u", "origin", "old")
	sh(t, dir, "push", "-q", "origin", "--delete", "old")
	sh(t, dir, "fetch", "-q", "--prune")

	branches, _ := ListBranches(dir)
	got := map[string]Branch{}
	for _, b := range branches {
		got[b.Name] = b
	}
	if b := got["main"]; b.Upstream != "origin/main" || b.Ahead != 1 || b.Behind != 0 {
		t.Fatalf("main = %+v", b)
	}
	if b := got["old"]; !b.Gone {
		t.Fatalf("old = %+v", b)
	}
	if _, ok := got["origin/old"]; ok {
		t.Fatal("pruned remote branch still listed")
	}
}

func TestMergeConflictAbort(t *testing.T) {
	dir := newRepo(t)
	sh(t, dir, "switch", "-q", "-c", "other")
	write(t, dir, "a.txt", "other\n")
	sh(t, dir, "commit", "-q", "-am", "other")
	sh(t, dir, "switch", "-q", "main")
	write(t, dir, "a.txt", "main\n")
	sh(t, dir, "commit", "-q", "-am", "main")

	err := MergeBranch(dir, "other")
	if err == nil {
		t.Fatal("expected a conflict")
	}
	if msg := shortGitError(err); !strings.Contains(msg, "conflicts") {
		t.Fatalf("shortGitError = %q (raw %v)", msg, err)
	}
	if h := HeadState(dir); h.Op != "merge" {
		t.Fatalf("Op = %q", h.Op)
	}
	if _, err := AbortOp(dir, "merge"); err != nil {
		t.Fatal(err)
	}
	if h := HeadState(dir); h.Op != "" {
		t.Fatalf("Op after abort = %q", h.Op)
	}
}

func TestRebaseConflictContinue(t *testing.T) {
	dir := newRepo(t)
	sh(t, dir, "switch", "-q", "-c", "other")
	write(t, dir, "a.txt", "other\n")
	sh(t, dir, "commit", "-q", "-am", "other")
	sh(t, dir, "switch", "-q", "main")
	write(t, dir, "a.txt", "main\n")
	sh(t, dir, "commit", "-q", "-am", "main")
	sh(t, dir, "switch", "-q", "other")

	if err := RebaseOnto(dir, "main"); err == nil {
		t.Fatal("expected a conflict")
	}
	if h := HeadState(dir); h.Op != "rebase" {
		t.Fatalf("Op = %q", h.Op)
	}
	write(t, dir, "a.txt", "resolved\n")
	sh(t, dir, "add", "a.txt")
	if _, err := ContinueOp(dir, "rebase"); err != nil {
		t.Fatalf("ContinueOp: %v", err)
	}
	if h := HeadState(dir); h.Op != "" || h.Branch != "other" {
		t.Fatalf("after continue = %+v", h)
	}
}

func TestTags(t *testing.T) {
	dir := newRepo(t)
	if err := CreateTag(dir, "v1", ""); err != nil {
		t.Fatal(err)
	}
	if err := CreateTag(dir, "v2", "release two"); err != nil {
		t.Fatal(err)
	}
	tags, err := ListTags(dir)
	if err != nil || len(tags) != 2 {
		t.Fatalf("ListTags = %+v, %v", tags, err)
	}
	byName := map[string]Tag{}
	for _, tg := range tags {
		byName[tg.Name] = tg
	}
	if byName["v1"].Annotated || !byName["v2"].Annotated || byName["v2"].Subject != "release two" {
		t.Fatalf("tags = %+v", tags)
	}

	bare := newBare(t)
	sh(t, dir, "remote", "add", "origin", bare)
	if _, err := PushTag(dir, "v2"); err != nil {
		t.Fatal(err)
	}
	if got := sh(t, bare, "tag"); got != "v2" {
		t.Fatalf("remote tags = %q", got)
	}
	if err := DeleteTag(dir, "v1"); err != nil {
		t.Fatal(err)
	}
	if tags, _ := ListTags(dir); len(tags) != 1 {
		t.Fatalf("after delete = %+v", tags)
	}
}

// Deleting the checked-out branch fails when it is not merged; the popup
// must then put the user back on that branch instead of leaving them on
// the fallback it switched to.
func TestPopupDeleteCurrentUnmergedSwitchesBack(t *testing.T) {
	dir := newRepo(t)
	sh(t, dir, "switch", "-q", "-c", "wip")
	write(t, dir, "a.txt", "wip\n")
	sh(t, dir, "commit", "-q", "-am", "wip")

	p := NewBranches()
	p.Start(dir)
	p.doDelete("wip", false)

	if got := current(t, dir); got != "wip" {
		t.Fatalf("on %q after failed delete, want wip", got)
	}
	if p.pending != pendingForceDelete || p.pendingName != "wip" {
		t.Fatalf("pending = %v %q", p.pending, p.pendingName)
	}

	p.doDelete("wip", true)
	if got := current(t, dir); got != "main" {
		t.Fatalf("on %q after force delete, want main", got)
	}
	if _, ok := p.branchByName("wip"); ok {
		t.Fatal("wip still listed")
	}
}

func TestWebURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:me/repo.git":             "https://github.com/me/repo",
		"https://github.com/me/repo.git":         "https://github.com/me/repo",
		"https://user:tok@gitlab.com/g/r.git":    "https://gitlab.com/g/r",
		"ssh://git@git.example.com:2222/g/r.git": "https://git.example.com/g/r",
		"/Users/me/bare.git":                     "",
		"file:///tmp/x":                          "",
	}
	for in, want := range cases {
		if got := WebURL(in); got != want {
			t.Errorf("WebURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRemoteOfAndParseTrack(t *testing.T) {
	if got := remoteOf("team/x/feature", []string{"team", "team/x"}); got != "team/x" {
		t.Errorf("remoteOf = %q", got)
	}
	a, b, gone := parseTrack("ahead 2, behind 3")
	if a != 2 || b != 3 || gone {
		t.Errorf("parseTrack = %d %d %v", a, b, gone)
	}
	if _, _, gone := parseTrack("gone"); !gone {
		t.Error("gone not parsed")
	}
}

func TestShortGitErrorStripsPrefixes(t *testing.T) {
	err := &Error{Args: []string{"switch", "nope"}, Output: "fatal: invalid reference: nope\nhint: try again"}
	if got := shortGitError(err); got != "invalid reference: nope" {
		t.Fatalf("shortGitError = %q", got)
	}
}
