package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrNotARepo is returned when the working directory is not inside a git repo.
var ErrNotARepo = errors.New("not a git repository")

// Error is a failed git invocation. Output is what git said about it:
// stderr, or stdout when stderr is empty (merge and rebase report
// conflicts on stdout).
type Error struct {
	Args   []string
	Output string
}

func (e *Error) Error() string {
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), e.Output)
}

func newError(args []string, output string, runErr error) error {
	output = strings.TrimSpace(output)
	if output == "" {
		output = runErr.Error()
	}
	if strings.Contains(output, "not a git repository") {
		return ErrNotARepo
	}
	return &Error{Args: args, Output: output}
}

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
	// Never let git take over the terminal from the TUI: no credential
	// prompts on /dev/tty (fail instead) and no editor for --continue.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true")
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined
	runErr := cmd.Run()
	out := strings.TrimSpace(combined.String())
	if runErr != nil {
		return out, newError(args, out, runErr)
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
		msg := stderr.String()
		if strings.TrimSpace(msg) == "" {
			msg = stdout.String()
		}
		gitErr := newError(args, msg, err)
		if gitErr == ErrNotARepo {
			return nil, gitErr
		}
		return stdout.Bytes(), gitErr
	}
	return stdout.Bytes(), nil
}
