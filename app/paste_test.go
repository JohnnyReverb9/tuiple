package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnnyReverb9/tuiple/clipboard"
	"github.com/JohnnyReverb9/tuiple/config"
	"github.com/JohnnyReverb9/tuiple/filesystem"
)

// tempConfig points the config package at a throwaway file so tests
// never touch the settings of whoever runs them.
func tempConfig(t *testing.T, apply func(*config.Config)) {
	t.Helper()
	t.Setenv("TUIPLE_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := config.Update(apply); err != nil {
		t.Fatalf("update: %v", err)
	}
	t.Cleanup(func() {
		t.Setenv("TUIPLE_CONFIG", "")
		_ = config.Load()
	})
}

// Pasting onto a name that is already taken is the one file operation
// that can destroy data, so each policy gets checked against the disk.
func TestPasteConflictPolicies(t *testing.T) {
	for _, tc := range []struct {
		policy      config.PasteConflict
		wantTarget  string   // contents of the existing file afterwards
		wantExtra   []string // additional files expected in the directory
		wantHistory bool
	}{
		{config.PasteSkip, "old", nil, false},
		{config.PasteOverwrite, "new", nil, true},
		{config.PasteRename, "old", []string{"note_1.txt"}, true},
	} {
		t.Run(string(tc.policy), func(t *testing.T) {
			tempConfig(t, func(c *config.Config) { c.Files.PasteConflict = tc.policy })

			src := t.TempDir()
			dst := t.TempDir()
			if err := os.WriteFile(filepath.Join(src, "note.txt"), []byte("new"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dst, "note.txt"), []byte("old"), 0644); err != nil {
				t.Fatal(err)
			}

			m := New(dst)
			m.currentPath = dst
			entry := filesystem.FileEntry{
				Name: "note.txt",
				Path: filepath.Join(src, "note.txt"),
			}

			undoBefore, _ := HistorySnapshot()
			updated, _ := m.handlePaste([]clipboard.Item{{Entry: entry}}, clipboard.OpCopy)
			got := updated.(Model)

			content, err := os.ReadFile(filepath.Join(dst, "note.txt"))
			if err != nil {
				t.Fatalf("target file: %v", err)
			}
			if string(content) != tc.wantTarget {
				t.Errorf("note.txt = %q, want %q (status: %s)",
					content, tc.wantTarget, got.statusMsg)
			}
			for _, name := range tc.wantExtra {
				if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
					t.Errorf("expected %s to exist: %v", name, err)
				}
			}

			undoAfter, _ := HistorySnapshot()
			if pushed := len(undoAfter) > len(undoBefore); pushed != tc.wantHistory {
				t.Errorf("history pushed = %v, want %v", pushed, tc.wantHistory)
			}
		})
	}
}
