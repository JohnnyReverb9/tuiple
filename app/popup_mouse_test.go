package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/JohnnyReverb9/tuiple/config"
)

// A click on the screen reaches the popup under it, at the right place:
// clicking a file's [ ] box in the git panel (Ctrl+G) stages the file.
func TestClickInsideGitPanelStages(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "first")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644)

	tempConfig(t, func(c *config.Config) { c.Git.Enabled = true })
	m := New(dir)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = settle(t, next.(Model), cmd)
	if !m.gitOverlay.IsActive() {
		t.Fatal("git panel did not open")
	}

	_, bx, by, ok := m.topPopup()
	if !ok {
		t.Fatal("no popup placement")
	}
	// In the box: row 4 is the first file, columns 2–4 its [ ] box.
	x, y := bx+3, m.chromeTop()+by+4
	next, cmd = m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = settle(t, next.(Model), cmd)
	if got := git("diff", "--cached", "--name-only"); got != "a.txt" {
		t.Fatalf("staged = %q", got)
	}

	// A press outside the box does nothing (and does not close it).
	next, cmd = m.Update(tea.MouseMsg{X: 0, Y: m.chromeTop(), Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = settle(t, next.(Model), cmd)
	if !m.gitOverlay.IsActive() {
		t.Fatal("click outside closed the panel")
	}
}
