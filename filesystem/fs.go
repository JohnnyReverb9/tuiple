// Package filesystem provides file system operations for tuiple.
package filesystem

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── FileEntry ──────────────────────────────────────────────────────────

// FileEntry represents a single file system entry with metadata.
type FileEntry struct {
	Name       string
	Path       string
	Size       int64
	ModTime    time.Time
	IsDir      bool
	IsHidden   bool
	IsExec     bool
	IsSymlink  bool
	LinkTarget string
	Mode       fs.FileMode
	Extension  string
}

// ── Display and sort options ───────────────────────────────────────────
//
// These are set once from the settings (see app.applySettings) rather
// than threaded through every call: sorting and formatting happen deep
// inside rendering paths that have no business knowing about config, and
// the alternative — passing options through a dozen signatures — buys
// nothing. Guarded by a mutex because previews format sizes and dates
// from background goroutines.

var (
	optMu        sync.RWMutex
	optDirsFirst = true
	optReverse   = false
	optTimeStyle = "short"
	optBinary    = true
)

// SetSortOptions controls how SortEntries orders a directory.
func SetSortOptions(dirsFirst, reverse bool) {
	optMu.Lock()
	optDirsFirst, optReverse = dirsFirst, reverse
	optMu.Unlock()
}

// SetFormatOptions controls how sizes and times are rendered.
// timeStyle is "short", "iso" or "relative"; binaryUnits picks KiB over kB.
func SetFormatOptions(timeStyle string, binaryUnits bool) {
	optMu.Lock()
	optTimeStyle, optBinary = timeStyle, binaryUnits
	optMu.Unlock()
}

func sortOptions() (dirsFirst, reverse bool) {
	optMu.RLock()
	defer optMu.RUnlock()
	return optDirsFirst, optReverse
}

func formatOptions() (timeStyle string, binaryUnits bool) {
	optMu.RLock()
	defer optMu.RUnlock()
	return optTimeStyle, optBinary
}

// ── Sorting ────────────────────────────────────────────────────────────

// SortMode determines how files are sorted.
type SortMode int

const (
	SortByName SortMode = iota
	SortBySize
	SortByDate
	SortByType
)

// SortEntries orders a directory listing. Directories come first and the
// order can be reversed, both per the current settings.
func SortEntries(entries []FileEntry, mode SortMode) {
	dirsFirst, reverse := sortOptions()

	sort.SliceStable(entries, func(i, j int) bool {
		// Directories first, when asked. This grouping is deliberately
		// immune to the reverse flag below: "reversed" should mean
		// Z→A within the groups, not files above folders.
		if dirsFirst && entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		if reverse {
			i, j = j, i
		}
		switch mode {
		case SortBySize:
			return entries[i].Size > entries[j].Size
		case SortByDate:
			return entries[i].ModTime.After(entries[j].ModTime)
		case SortByType:
			if entries[i].Extension != entries[j].Extension {
				return entries[i].Extension < entries[j].Extension
			}
			return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
		default: // SortByName
			return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
		}
	})
}

// ── Directory Reading ──────────────────────────────────────────────────

// ReadDir reads and returns entries for a directory.
func ReadDir(path string, showHidden bool) ([]FileEntry, error) {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory: %w", err)
	}

	var entries []FileEntry
	for _, de := range dirEntries {
		name := de.Name()
		isHidden := strings.HasPrefix(name, ".")
		if !showHidden && isHidden {
			continue
		}
		// Always hide tuiple trash files
		if strings.Contains(name, ".tuiple_trash_") {
			continue
		}

		fullPath := filepath.Join(path, name)

		entry := FileEntry{
			Name:     name,
			Path:     fullPath,
			IsHidden: isHidden,
		}

		// Detect symlinks via Lstat
		linfo, err := os.Lstat(fullPath)
		if err != nil {
			continue
		}
		if linfo.Mode()&os.ModeSymlink != 0 {
			entry.IsSymlink = true
			target, linkErr := os.Readlink(fullPath)
			if linkErr == nil {
				entry.LinkTarget = target
			}
		}

		// Stat follows symlinks; fall back to lstat for broken links
		info, err := os.Stat(fullPath)
		if err != nil {
			info = linfo
		}

		entry.Size = info.Size()
		entry.ModTime = info.ModTime()
		entry.IsDir = info.IsDir()
		entry.Mode = info.Mode()
		entry.Extension = strings.ToLower(filepath.Ext(name))

		if !info.IsDir() && info.Mode()&0111 != 0 {
			entry.IsExec = true
		}

		entries = append(entries, entry)
	}

	SortEntries(entries, SortByName)
	return entries, nil
}

// ── Formatting helpers ─────────────────────────────────────────────────

// FormatSize formats a byte count into a human-readable string.
func FormatSize(size int64) string {
	_, binaryUnits := formatOptions()

	unit := int64(1000)
	names := []string{"kB", "MB", "GB", "TB"}
	if binaryUnits {
		unit = 1024
		names = []string{"KiB", "MiB", "GiB", "TiB"}
	}

	step := unit
	for i, name := range names {
		next := step * unit
		if size < next || i == len(names)-1 {
			if size < step {
				break
			}
			value := float64(size) / float64(step)
			// Three significant digits, never more: the file list gives
			// this column eight cells, and "918.2 KiB" needs nine —
			// which lipgloss would wrap into the Modified column rather
			// than truncate. "918 KiB" is also simply easier to read.
			if value >= 100 {
				return fmt.Sprintf("%.0f %s", value, name)
			}
			return fmt.Sprintf("%.1f %s", value, name)
		}
		step = next
	}
	return fmt.Sprintf("%d B", size)
}

// FormatTime formats a modification timestamp.
func FormatTime(t time.Time) string {
	style, _ := formatOptions()
	switch style {
	case "iso":
		return t.Format("2006-01-02 15:04")
	case "relative":
		return relativeTime(t)
	}
	now := time.Now()
	if t.Year() == now.Year() {
		return t.Format("Jan 02 15:04")
	}
	return t.Format("Jan 02  2006")
}

// relativeTime renders an age rather than a date: closer to how one
// actually thinks about a working directory ("edited 5m ago").
func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 0:
		return t.Format("Jan 02 15:04")
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	}
	return fmt.Sprintf("%dy ago", int(d.Hours())/24/365)
}

// DirItemCount returns the number of direct children in a directory.
func DirItemCount(path string) int {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	return len(entries)
}

// DirSize returns the recursive sum of all regular file sizes inside path.
// Symbolic links are not followed (we count the size of the link target's
// dentry, not what it points to) and unreadable subtrees are silently
// skipped so a single permission error doesn't sabotage the total.
func DirSize(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep walking
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// ── Path helpers ───────────────────────────────────────────────────────

// HomeDir returns the current user's home directory.
func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/"
	}
	return home
}

// ShortenPath replaces the home directory prefix with ~.
func ShortenPath(path string) string {
	home := HomeDir()
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
