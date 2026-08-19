package app

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/JohnnyReverb9/tuiple/filesystem"
)

// Archiving shells out to the tools macOS already ships — zip, tar and
// ditto — rather than reimplementing them. They handle resource forks,
// symlinks and permissions correctly, which a stdlib archive/zip loop
// would quietly get wrong.

// archiveDoneMsg reports the result of a compress or extract run.
type archiveDoneMsg struct {
	path    string // the archive that was written, or the directory extracted into
	isDir   bool   // true when path is an extraction target
	verb    string // "Archived" / "Extracted", for the status line
	count   int
	err     error
	details string // first line of the tool's own error output
}

// archiveExtensions lists what extractArchive knows how to open. The
// two-part suffixes have to be checked before filepath.Ext, which would
// only see ".gz".
var archiveExtensions = []string{
	".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst",
	".zip", ".tar", ".tgz", ".tbz", ".txz", ".jar",
}

// IsArchive reports whether a name looks like something extractArchive
// can handle.
func IsArchive(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// archiveBaseName strips the archive suffix so foo.tar.gz extracts into
// a directory called foo rather than foo.tar.
func archiveBaseName(name string) string {
	lower := strings.ToLower(name)
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(lower, ext) {
			return name[:len(name)-len(ext)]
		}
	}
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// createArchive zips the given names (relative to dir) into dst. Running
// zip with dir as its working directory is what keeps the archive's
// internal paths relative — otherwise every entry would carry the full
// path from the volume root.
func createArchive(dir, dst string, names []string) tea.Cmd {
	args := append([]string{"-r", "-q", dst}, names...)
	cmd := exec.Command("zip", args...)
	cmd.Dir = dir
	count := len(names)

	return func() tea.Msg {
		out, err := cmd.CombinedOutput()
		return archiveDoneMsg{
			path:    dst,
			verb:    "Archived",
			count:   count,
			err:     err,
			details: firstLine(string(out)),
		}
	}
}

// extractArchive unpacks src into destDir, which the caller has already
// made unique. zip files go through ditto (it handles macOS metadata and
// is always present); everything else is a tar variant.
func extractArchive(src, destDir string) tea.Cmd {
	lower := strings.ToLower(src)
	var cmd *exec.Cmd
	switch {
	case strings.HasSuffix(lower, ".zip"), strings.HasSuffix(lower, ".jar"):
		cmd = exec.Command("ditto", "-x", "-k", src, destDir)
	default:
		// tar reads the compression itself; -C lands everything in the
		// destination directory we created for it.
		cmd = exec.Command("tar", "-xf", src, "-C", destDir)
	}

	return func() tea.Msg {
		out, err := cmd.CombinedOutput()
		return archiveDoneMsg{
			path:    destDir,
			isDir:   true,
			verb:    "Extracted",
			count:   1,
			err:     err,
			details: firstLine(string(out)),
		}
	}
}

// firstLine keeps status-bar errors to one line — tar and zip are fond
// of paragraphs.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// archiveName picks the name for a new archive: the single item's name,
// or the containing directory's name when several items are selected.
func archiveName(dir string, names []string) string {
	if len(names) == 1 {
		return archiveBaseName(names[0]) + ".zip"
	}
	base := filepath.Base(dir)
	if base == "/" || base == "." || base == "" {
		base = "archive"
	}
	return base + ".zip"
}

// archiveStatus phrases the result for the status bar.
func (msg archiveDoneMsg) status() string {
	if msg.err != nil {
		if msg.details != "" {
			return fmt.Sprintf("%s failed: %s", strings.ToLower(msg.verb), msg.details)
		}
		return fmt.Sprintf("%s failed: %v", strings.ToLower(msg.verb), msg.err)
	}
	if msg.verb == "Archived" {
		return fmt.Sprintf("Archived %d item(s) → %s", msg.count, filepath.Base(msg.path))
	}
	return "Extracted → " + filepath.Base(msg.path) + "/"
}

// dirSizeResultMsg carries a finished directory measurement.
type dirSizeResultMsg struct {
	Path string
	Size int64
}

// dirSizeCmd walks a directory off the UI goroutine. Sizes are computed
// on demand (the s key) rather than automatically: a recursive walk of
// every directory in view would stall the list on any large tree.
func dirSizeCmd(path string) tea.Cmd {
	return func() tea.Msg {
		return dirSizeResultMsg{Path: path, Size: filesystem.DirSize(path)}
	}
}
