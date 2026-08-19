package filesystem

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrRestoreBlocked means macOS refused to move an item back out of the
// Trash. It is a permission verdict, not a missing file: the item is
// still there, and the same undo works once the terminal running tuiple
// has Full Disk Access — or after Finder's own Put Back.
var ErrRestoreBlocked = errors.New("macOS blocked the restore")

// trashScript asks Finder to move a file to the Trash and reports where
// it landed. Going through Finder (rather than moving the file into
// ~/.Trash ourselves) is what records the "Put Back" metadata, so items
// deleted from tuiple behave exactly like items deleted from Finder.
const trashScript = `on run argv
	set theFile to POSIX file (item 1 of argv)
	tell application "Finder"
		set movedItem to (delete (theFile as alias))
		return POSIX path of (movedItem as text)
	end tell
end run`

// MoveToTrash moves src to the macOS Trash and returns its new path so
// the caller can offer undo by moving it straight back.
//
// Finder is tried first; if that fails — automation permission denied,
// Finder not running under a launchd-less session, src living on a
// volume Finder refuses to touch — we fall back to moving the item into
// ~/.Trash by hand. The fallback loses "Put Back" but still keeps the
// file recoverable, which is the property that actually matters here.
func MoveToTrash(src string) (string, error) {
	if _, err := os.Lstat(src); err != nil {
		return "", err
	}

	out, err := exec.Command("osascript", "-e", trashScript, src).Output()
	if err == nil {
		dst := strings.TrimSpace(string(out))
		// Finder reports directories with a trailing slash; strip it so
		// the path round-trips through Move() unchanged.
		dst = strings.TrimSuffix(dst, "/")
		dst = normalizeTrashPath(dst)
		if dst != "" && exists(dst) {
			return dst, nil
		}
		// Finder swallowed the file but told us nothing useful about
		// where it went; locate it by name instead of guessing.
		if found := findInTrash(filepath.Base(src)); found != "" {
			return found, nil
		}
		return "", fmt.Errorf("moved to Trash but could not determine its path")
	}

	return moveToTrashDir(src)
}

// moveToTrashDir is the Finder-less fallback: a plain move into
// ~/.Trash, with a numeric suffix when the name is already taken.
func moveToTrashDir(src string) (string, error) {
	trashDir := filepath.Join(HomeDir(), ".Trash")
	if err := os.MkdirAll(trashDir, 0700); err != nil {
		return "", err
	}
	dst := uniqueTrashPath(trashDir, filepath.Base(src))
	if err := Move(src, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// uniqueTrashPath appends " 2", " 3", … before the extension until the
// name is free — the same scheme Finder itself uses for collisions.
func uniqueTrashPath(dir, name string) string {
	candidate := filepath.Join(dir, name)
	if _, err := os.Lstat(candidate); os.IsNotExist(err) {
		return candidate
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; i < 1000; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s %d%s", base, i, ext))
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s %d%s", base, time.Now().UnixNano(), ext))
}

// findInTrash looks for the most plausible match for name in ~/.Trash.
// Finder renames colliding items, so an exact hit is preferred and a
// "name N.ext" style match is accepted as a runner-up.
func findInTrash(name string) string {
	trashDir := filepath.Join(HomeDir(), ".Trash")
	exact := filepath.Join(trashDir, name)
	if _, err := os.Lstat(exact); err == nil {
		return exact
	}
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		return ""
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	var newest string
	var newestMod time.Time
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, base) || !strings.HasSuffix(n, ext) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == "" || info.ModTime().After(newestMod) {
			newest, newestMod = filepath.Join(trashDir, n), info.ModTime()
		}
	}
	return newest
}

// normalizeTrashPath rewrites the volume-prefixed paths Finder sometimes
// reports (/System/Volumes/Data/.nofollow/Users/... instead of
// /Users/...) back into the plain form the rest of the app uses. Both
// spellings point at the same inode; only one of them is comparable to
// the paths we produce ourselves.
func normalizeTrashPath(p string) string {
	for _, prefix := range []string{"/System/Volumes/Data/.nofollow", "/System/Volumes/Data", "/.nofollow"} {
		if strings.HasPrefix(p, prefix) {
			return strings.TrimPrefix(p, prefix)
		}
	}
	return p
}

// exists reports whether a path is present. A permission error counts as
// present on purpose: ~/.Trash is protected by macOS privacy controls,
// so a process without Full Disk Access gets EPERM rather than ENOENT
// for files that are very much there.
func exists(p string) bool {
	_, err := os.Lstat(p)
	if err == nil {
		return true
	}
	return !os.IsNotExist(err)
}

// restoreScript asks Finder to move an item out of the Trash and into a
// destination folder. Finder is the only actor that reliably has the
// rights to touch the Trash, so this is the fallback when the plain
// rename below is refused by the OS.
const restoreScript = `on run argv
	set theItem to (POSIX file (item 1 of argv)) as alias
	set destText to (POSIX file (item 2 of argv)) as text
	tell application "Finder"
		move theItem to folder destText
	end tell
end run`

// RestoreFromTrash moves a trashed item back to dst, which is the path
// it occupied before deletion.
//
// Three layers, cheapest first:
//  1. a plain rename — works when the terminal running tuiple has been
//     granted Full Disk Access;
//  2. Finder, which can reach into the Trash when we cannot (it refuses
//     for hidden destination folders, hence the fallback below);
//  3. an explicit error telling the user to use Finder's Put Back —
//     macOS deliberately keeps scripted restores on a short leash, and
//     silently pretending the undo worked would be worse than saying so.
func RestoreFromTrash(trashPath, dst string) error {
	return restoreFromTrash(trashPath, dst, finderMove, reveal)
}

// restoreFromTrash is the testable core: the two shell-outs are injected
// so the fallback chain can be exercised without driving Finder.
func restoreFromTrash(trashPath, dst string,
	finder func(item, destDir string) error, show func(path string)) error {

	// Put Back may already have done the job while the error message was
	// on screen. Nothing left to restore is success, not failure.
	if !exists(trashPath) && exists(dst) {
		return nil
	}

	if err := Move(trashPath, dst); err == nil {
		return nil
	}

	destDir := filepath.Dir(dst)
	if err := finder(trashPath, destDir); err == nil {
		// Finder keeps the item name; when it collided it may have been
		// renamed, so only report success if the file really is there.
		landed := filepath.Join(destDir, filepath.Base(trashPath))
		if exists(landed) {
			if landed != dst {
				return Move(landed, dst)
			}
			return nil
		}
	}

	// Both routes are shut: macOS reserves un-trashing for Finder unless
	// the calling process holds Full Disk Access. Reveal the item so Put
	// Back is one keystroke away and report a retryable failure — the
	// file is intact, so the undo entry stays valid.
	show(trashPath)
	return fmt.Errorf("%w: give your terminal Full Disk Access, or use Put Back in Finder",
		ErrRestoreBlocked)
}

func finderMove(item, destDir string) error {
	return exec.Command("osascript", "-e", restoreScript, item, destDir).Run()
}

func reveal(path string) {
	_ = exec.Command("open", "-R", path).Start()
}

// InTrash reports whether a path lives in the user's Trash, which is how
// the undo logic tells a trash-mode delete from a timer-mode one without
// having to version the on-disk history format.
func InTrash(p string) bool {
	trashDir := filepath.Join(HomeDir(), ".Trash") + string(filepath.Separator)
	return strings.HasPrefix(p, trashDir)
}
