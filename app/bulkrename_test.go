package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildRenamePlanRejectsBadEdits(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "keep.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	original := []string{"a.txt", "b.txt"}

	for _, tc := range []struct {
		name   string
		edited []string
	}{
		{"line removed", []string{"a.txt"}},
		{"line added", []string{"a.txt", "b.txt", "c.txt"}},
		{"emptied name", []string{"", "b.txt"}},
		{"path separator", []string{"sub/a.txt", "b.txt"}},
		{"collision", []string{"same.txt", "same.txt"}},
		{"target exists", []string{"keep.txt", "b.txt"}},
	} {
		if _, err := buildRenamePlan(dir, original, tc.edited); err == nil {
			t.Errorf("%s: expected an error, got none", tc.name)
		}
	}
}

func TestBuildRenamePlanSkipsUnchanged(t *testing.T) {
	dir := t.TempDir()
	steps, err := buildRenamePlan(dir,
		[]string{"a.txt", "b.txt"},
		[]string{"a.txt", "renamed.txt"})
	if err != nil {
		t.Fatalf("buildRenamePlan: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("got %d steps, want only the changed line", len(steps))
	}
	if filepath.Base(steps[0].dst) != "renamed.txt" {
		t.Errorf("dst = %s, want renamed.txt", steps[0].dst)
	}
}

// Swapping two names is the case that forces the two-pass apply: done
// naively, the first rename would destroy the second one's source.
func TestApplyRenamePlanSwapsNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("B"), 0644); err != nil {
		t.Fatal(err)
	}

	steps, err := buildRenamePlan(dir,
		[]string{"a.txt", "b.txt"},
		[]string{"b.txt", "a.txt"})
	if err != nil {
		t.Fatalf("buildRenamePlan: %v", err)
	}
	items, err := applyRenamePlan(steps)
	if err != nil {
		t.Fatalf("applyRenamePlan: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d history items, want 2", len(items))
	}

	for name, want := range map[string]string{"a.txt": "B", "b.txt": "A"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s contains %q, want %q", name, got, want)
		}
	}

	// The recorded items must be exactly what undo needs to reverse it.
	ev := HistoryEvent{Op: OpRename, Items: items}
	if err := revertEvent(&ev); err != nil {
		t.Fatalf("revert: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(got) != "A" {
		t.Errorf("after undo a.txt = %q (%v), want %q", got, err, "A")
	}
}

func TestReadEditedNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "names.txt")
	// Trailing newline and CRLF are both normal things for an editor to
	// write; neither may turn into a phantom line.
	if err := os.WriteFile(path, []byte("one.txt\r\ntwo.txt\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := readEditedNames(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "one.txt" || got[1] != "two.txt" {
		t.Errorf("got %q, want [one.txt two.txt]", got)
	}
}
