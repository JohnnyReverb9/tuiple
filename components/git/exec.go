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
	cmd := exec.Command("git", args...)
	cmd.Dir = repo

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		// Translate well-known errors so callers can branch on them.
		if strings.Contains(msg, "not a git repository") {
			return nil, ErrNotARepo
		}
		return stdout.Bytes(), fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}
