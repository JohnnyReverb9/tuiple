package git

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoRemote is returned by network operations in a repository that has
// no remote configured yet.
var ErrNoRemote = errors.New("no remote configured")

// ── Init ───────────────────────────────────────────────────────────────

// Init runs `git init` in dir and returns the new repository root. The
// initial branch name comes from the user's init.defaultBranch.
func Init(dir string) (string, error) {
	if _, err := run(dir, "init"); err != nil {
		return "", err
	}
	return FindRoot(dir)
}

// ── Remotes ────────────────────────────────────────────────────────────

// Remote is a configured remote with its fetch and push URLs.
type Remote struct {
	Name     string
	FetchURL string
	PushURL  string // differs from FetchURL only when set separately
}

// RemoteNames lists the configured remote names.
func RemoteNames(repo string) ([]string, error) {
	out, err := run(repo, "remote")
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// Remotes lists the configured remotes with their URLs.
func Remotes(repo string) ([]Remote, error) {
	out, err := run(repo, "remote", "-v")
	if err != nil || out == "" {
		return nil, err
	}
	var remotes []Remote
	index := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		// "origin\tgit@github.com:me/repo.git (fetch)"
		name, rest, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		url, kind, _ := strings.Cut(rest, " ")
		i, seen := index[name]
		if !seen {
			i = len(remotes)
			index[name] = i
			remotes = append(remotes, Remote{Name: name})
		}
		switch kind {
		case "(fetch)":
			remotes[i].FetchURL = url
		case "(push)":
			remotes[i].PushURL = url
		}
	}
	return remotes, nil
}

// AddRemote adds a remote and fetches it, so its branches show up at once.
func AddRemote(repo, name, url string) (string, error) {
	if _, err := run(repo, "remote", "add", name, url); err != nil {
		return "", err
	}
	return runWithOutput(repo, "fetch", "--prune", name)
}

// SetRemoteURL changes the URL of a remote (fetch and push).
func SetRemoteURL(repo, name, url string) error {
	_, err := run(repo, "remote", "set-url", name, url)
	return err
}

// RenameRemote renames a remote together with its remote-tracking branches.
func RenameRemote(repo, from, to string) error {
	_, err := run(repo, "remote", "rename", from, to)
	return err
}

// RemoveRemote removes a remote and its remote-tracking branches.
func RemoveRemote(repo, name string) error {
	_, err := run(repo, "remote", "remove", name)
	return err
}

// FetchRemote fetches a single remote, pruning deleted branches.
func FetchRemote(repo, name string) (string, error) {
	return runWithOutput(repo, "fetch", "--prune", name)
}

// PushRemote picks the remote a new branch is pushed to: the branch's own
// pushRemote, remote.pushDefault, "origin", or the only remote there is.
func PushRemote(repo, branch string) (string, error) {
	names, err := RemoteNames(repo)
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", ErrNoRemote
	}
	for _, key := range []string{"branch." + branch + ".pushRemote", "remote.pushDefault", "branch." + branch + ".remote"} {
		if v, err := run(repo, "config", "--get", key); err == nil && v != "" {
			return v, nil
		}
	}
	for _, n := range names {
		if n == "origin" {
			return n, nil
		}
	}
	if len(names) == 1 {
		return names[0], nil
	}
	return "", fmt.Errorf("several remotes (%s) — set one with `git config remote.pushDefault <name>`", strings.Join(names, ", "))
}

// WebURL turns a remote URL into the https address of the repository's web
// page, e.g. git@github.com:me/repo.git → https://github.com/me/repo.
// It returns "" for URLs that have no web page (local paths, file://).
func WebURL(remoteURL string) string {
	u := strings.TrimSpace(remoteURL)
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")
	switch {
	case strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "http://"):
		// Drop credentials: https://user:token@host/path
		scheme, rest, _ := strings.Cut(u, "://")
		if at := strings.LastIndexByte(rest, '@'); at >= 0 && at < strings.IndexByte(rest+"/", '/') {
			rest = rest[at+1:]
		}
		return scheme + "://" + rest
	case strings.HasPrefix(u, "ssh://"), strings.HasPrefix(u, "git://"):
		_, rest, _ := strings.Cut(u, "://")
		if at := strings.IndexByte(rest, '@'); at >= 0 {
			rest = rest[at+1:]
		}
		host, path, _ := strings.Cut(rest, "/")
		host, _, _ = strings.Cut(host, ":") // drop port
		return "https://" + host + "/" + path
	case strings.Contains(u, ":") && !strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "file:"):
		// scp-like: [user@]host:path
		host, path, _ := strings.Cut(u, ":")
		if at := strings.IndexByte(host, '@'); at >= 0 {
			host = host[at+1:]
		}
		return "https://" + host + "/" + strings.TrimPrefix(path, "/")
	}
	return ""
}

// ── Tags ───────────────────────────────────────────────────────────────

// Tag is a single entry from `git for-each-ref refs/tags`.
type Tag struct {
	Name      string
	Annotated bool
	Subject   string // tag message for annotated tags, commit subject otherwise
	Date      string // relative creation date
}

// ListTags returns tags, newest first.
func ListTags(repo string) ([]Tag, error) {
	out, err := run(repo, "for-each-ref", "--sort=-creatordate",
		"--format=%(refname:short)%00%(objecttype)%00%(contents:subject)%00%(creatordate:relative)",
		"refs/tags")
	if err != nil || out == "" {
		return nil, err
	}
	var tags []Tag
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\x00", 4)
		if len(parts) < 4 {
			continue
		}
		tags = append(tags, Tag{
			Name:      parts[0],
			Annotated: parts[1] == "tag",
			Subject:   parts[2],
			Date:      parts[3],
		})
	}
	return tags, nil
}

// CreateTag tags HEAD. A non-empty message makes it an annotated tag.
func CreateTag(repo, name, message string) error {
	args := []string{"tag"}
	if strings.TrimSpace(message) != "" {
		args = append(args, "-a", name, "-m", message)
	} else {
		args = append(args, name)
	}
	_, err := run(repo, args...)
	return err
}

// DeleteTag deletes a local tag.
func DeleteTag(repo, name string) error {
	_, err := run(repo, "tag", "-d", name)
	return err
}

// PushTag pushes one tag to the current branch's push remote.
func PushTag(repo, name string) (string, error) {
	branch, _ := CurrentBranch(repo)
	remote, err := PushRemote(repo, branch)
	if err != nil {
		return "", err
	}
	return runWithOutput(repo, "push", remote, "refs/tags/"+name)
}
