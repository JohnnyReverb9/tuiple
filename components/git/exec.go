package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNotARepo is returned when the working directory is not inside a git repo.
var ErrNotARepo = errors.New("not a git repository")

// run executes `git <args...>` with the given working directory and returns
// trimmed stdout. On non-zero exit the returned error carries stderr.
func run(repo string, args ...string) (string, error) {
	out, err := runRaw(repo, args...)
	return strings.TrimRight(string(out), "\n"), err
}

// runRaw is like run but returns raw stdout bytes (useful for diff output
// where trailing whitespace and exact bytes matter).
func runRaw(repo string, args ...string) ([]byte, error) {
	return runRawStdin(repo, "", args...)
}

// runWithStdin runs git with stdin piped from the given string and returns
// trimmed stdout. Used for commands like `git commit -F -` that read the
// message from stdin.
func runWithStdin(repo, stdin string, args ...string) (string, error) {
	out, err := runRawStdin(repo, stdin, args...)
	return strings.TrimRight(string(out), "\n"), err
}

// runWithOutput combines stdout and stderr into a single string, which is
// needed for commands like `git push / pull / fetch` that report progress
// (and meaningful success messages like "Everything up-to-date") on stderr
// rather than stdout.
//
// The combined output is returned even when the command exits non-zero so
// that callers can inspect git's full error message (e.g. to detect the
// "no upstream branch" hint and silently retry with --set-upstream).
func runWithOutput(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined
	runErr := cmd.Run()
	out := strings.TrimSpace(combined.String())
	if runErr != nil {
		msg := out
		if msg == "" {
			msg = runErr.Error()
		}
		if strings.Contains(msg, "not a git repository") {
			return out, ErrNotARepo
		}
		return out, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

func runRawStdin(repo, stdin string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if strings.Contains(msg, "not a git repository") {
			return nil, ErrNotARepo
		}
		return stdout.Bytes(), fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}
