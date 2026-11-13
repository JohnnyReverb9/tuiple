package git

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func press(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

// Amend rewrites the last commit — message, and whatever is staged —
// without adding a new one, and switches itself off afterwards.
func TestAmendLastCommit(t *testing.T) {
	repo := newRepo(t) // one commit, "first"
	m := New()
	m.SetSize(120, 40)
	m.Start(repo)

	m, _ = m.Update(press(tea.KeyCtrlA))
	if !m.commit.amend || m.commit.message.Value() != "first" {
		t.Fatalf("amend=%v, message %q; want on with the last message filled in",
			m.commit.amend, m.commit.message.Value())
	}
	if !strings.Contains(m.View(), "Amend last commit") {
		t.Error("the panel does not say it is amending")
	}

	// Reword only: nothing is staged, and that must be enough.
	m.commit.message.SetValue("first, spelled right")
	m, _ = m.Update(press(tea.KeyCtrlS))
	if m.commit.statusErr {
		t.Fatalf("amend failed: %s", m.commit.status)
	}
	if got := sh(t, repo, "log", "--format=%s"); got != "first, spelled right" {
		t.Fatalf("history is %q, want the one commit reworded", got)
	}
	if m.commit.amend || m.commit.message.Value() != "" {
		t.Error("amend mode or the message outlived the commit")
	}

	// Staged changes go into the amended commit too.
	write(t, repo, "b.txt", "two\n")
	sh(t, repo, "add", "b.txt")
	m, _ = m.Update(press(tea.KeyCtrlA))
	m, _ = m.Update(press(tea.KeyCtrlS))
	if got := sh(t, repo, "rev-list", "--count", "HEAD"); got != "1" {
		t.Errorf("%s commits after amending, want still 1", got)
	}
	if got := sh(t, repo, "show", "--name-only", "--format=", "HEAD"); !strings.Contains(got, "b.txt") {
		t.Errorf("the staged file is not in the amended commit: %q", got)
	}
}

// Switching amend off takes back the message it filled in, but never
// one the user wrote.
func TestAmendOffRestoresTheMessageBox(t *testing.T) {
	repo := newRepo(t)
	m := New()
	m.SetSize(120, 40)
	m.Start(repo)

	m, _ = m.Update(press(tea.KeyCtrlA))
	m, _ = m.Update(press(tea.KeyCtrlA))
	if m.commit.amend || m.commit.message.Value() != "" {
		t.Errorf("after on/off: amend=%v, message %q", m.commit.amend, m.commit.message.Value())
	}

	m.commit.message.SetValue("my own words")
	m, _ = m.Update(press(tea.KeyCtrlA))
	m, _ = m.Update(press(tea.KeyCtrlA))
	if got := m.commit.message.Value(); got != "my own words" {
		t.Errorf("the user's message became %q", got)
	}
	if got := sh(t, repo, "log", "--format=%s"); got != "first" {
		t.Errorf("toggling changed history: %q", got)
	}
}
