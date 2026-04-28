// Package filesystem provides file system operations for tuiple.
package filesystem

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// ── Sorting ────────────────────────────────────────────────────────────

// SortMode determines how files are sorted.
type SortMode int

const (
	SortByName SortMode = iota
	SortBySize
	SortByDate
	SortByType
)

// SortEntries sorts entries with directories always first.
func SortEntries(entries []FileEntry, mode SortMode) {
	sort.SliceStable(entries, func(i, j int) bool {
		// Directories always first
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
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
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)
	switch {
	case size >= TB:
		return fmt.Sprintf("%.1f TB", float64(size)/float64(TB))
	case size >= GB:
		return fmt.Sprintf("%.1f GB", float64(size)/float64(GB))
	case size >= MB:
		return fmt.Sprintf("%.1f MB", float64(size)/float64(MB))
	case size >= KB:
		return fmt.Sprintf("%.1f KB", float64(size)/float64(KB))
	default:
		return fmt.Sprintf("%d B", size)
	}
}

// FormatTime formats a modification timestamp.
func FormatTime(t time.Time) string {
	now := time.Now()
	if t.Year() == now.Year() {
		return t.Format("Jan 02 15:04")
	}
	return t.Format("Jan 02  2006")
}

// DirItemCount returns the number of direct children in a directory.
func DirItemCount(path string) int {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	return len(entries)
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
