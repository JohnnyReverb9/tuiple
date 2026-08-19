package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The fallback chain has three outcomes, and only the middle one is
// visible in normal use — so all three get pinned down here rather than
// discovered on someone's Mac.
func TestRestoreFromTrashFallbacks(t *testing.T) {
	newPair := func(t *testing.T) (trashPath, dst string) {
		t.Helper()
		dir := t.TempDir()
		trashPath = filepath.Join(dir, "trashed.txt")
		dst = filepath.Join(dir, "restored", "trashed.txt")
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(trashPath, []byte("payload"), 0644); err != nil {
			t.Fatal(err)
		}
		return trashPath, dst
	}

	neverCalled := func(t *testing.T) (func(string, string) error, func(string)) {
		t.Helper()
		return func(string, string) error {
				t.Error("Finder should not be asked when a plain move works")
				return nil
			}, func(string) {
				t.Error("nothing should be revealed on the happy path")
			}
	}

	t.Run("plain move", func(t *testing.T) {
		trashPath, dst := newPair(t)
		finder, show := neverCalled(t)
		if err := restoreFromTrash(trashPath, dst, finder, show); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if _, err := os.Stat(dst); err != nil {
			t.Errorf("file did not land at its original path: %v", err)
		}
	})

	t.Run("already put back by hand", func(t *testing.T) {
		dir := t.TempDir()
		dst := filepath.Join(dir, "back.txt")
		if err := os.WriteFile(dst, []byte("payload"), 0644); err != nil {
			t.Fatal(err)
		}
		finder, show := neverCalled(t)
		// The trash copy is gone and the original is there: Finder's Put
		// Back beat us to it, which is a success.
		if err := restoreFromTrash(filepath.Join(dir, "gone.txt"), dst, finder, show); err != nil {
			t.Errorf("restore after Put Back reported %v, want success", err)
		}
	})

	t.Run("blocked by the OS", func(t *testing.T) {
		trashPath, dst := newPair(t)
		// Make the plain move fail the way TCC makes it fail.
		if err := os.Chmod(filepath.Dir(dst), 0500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Dir(dst), 0755) })

		var revealed string
		err := restoreFromTrash(trashPath, dst,
			func(string, string) error { return errors.New("Finder says no") },
			func(p string) { revealed = p })

		if !errors.Is(err, ErrRestoreBlocked) {
			t.Fatalf("error = %v, want ErrRestoreBlocked so undo can be retried", err)
		}
		if revealed != trashPath {
			t.Errorf("revealed %q, want the trashed item", revealed)
		}
		if !exists(trashPath) {
			t.Error("the file must stay in the Trash when the restore is refused")
		}
	})
}
