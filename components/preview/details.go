package preview

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"image"
	_ "image/gif" // registered for image.DecodeConfig
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/JohnnyReverb9/tuiple/components/preview/mediarender"
	"github.com/JohnnyReverb9/tuiple/filesystem"
)

// Detail is one labelled row of the info block at the top of the preview.
type Detail struct {
	Label string
	Value string
}

// ── Kind ───────────────────────────────────────────────────────────────

// kindNames names file types the way Finder's "Kind" column does.
var kindNames = map[string]string{
	".go": "Go source", ".mod": "Go module", ".sum": "Go checksums",
	".py": "Python script", ".php": "PHP script", ".rb": "Ruby script",
	".js": "JavaScript", ".mjs": "JavaScript module", ".cjs": "JavaScript (CommonJS)",
	".ts": "TypeScript", ".tsx": "TypeScript JSX", ".jsx": "JavaScript JSX",
	".vue": "Vue component", ".svelte": "Svelte component",
	".html": "HTML document", ".htm": "HTML document", ".css": "CSS stylesheet",
	".scss": "Sass stylesheet", ".less": "Less stylesheet",
	".c": "C source", ".h": "C header", ".cpp": "C++ source", ".cc": "C++ source",
	".hpp": "C++ header", ".rs": "Rust source", ".java": "Java source",
	".kt": "Kotlin source", ".swift": "Swift source", ".cs": "C# source",
	".lua": "Lua script", ".sh": "Shell script", ".bash": "Bash script",
	".zsh": "Zsh script", ".fish": "Fish script", ".sql": "SQL script",
	".json": "JSON", ".yaml": "YAML", ".yml": "YAML", ".toml": "TOML",
	".xml": "XML document", ".csv": "CSV table", ".tsv": "TSV table",
	".md": "Markdown", ".txt": "Plain text", ".log": "Log file",
	".env": "Environment file", ".ini": "INI settings", ".conf": "Config file",
	".lock": "Lock file", ".proto": "Protocol Buffers",
	".png": "PNG image", ".jpg": "JPEG image", ".jpeg": "JPEG image",
	".gif": "GIF image", ".webp": "WebP image", ".heic": "HEIC image",
	".svg": "SVG image", ".bmp": "BMP image", ".tiff": "TIFF image", ".ico": "Icon",
	".mp4": "MPEG-4 video", ".mov": "QuickTime movie", ".mkv": "Matroska video",
	".webm": "WebM video", ".avi": "AVI video", ".m4v": "MPEG-4 video",
	".mp3": "MP3 audio", ".m4a": "AAC audio", ".flac": "FLAC audio",
	".wav": "WAVE audio", ".ogg": "Ogg audio", ".opus": "Opus audio", ".aiff": "AIFF audio",
	".pdf": "PDF document", ".epub": "EPUB book", ".djvu": "DjVu document",
	".fb2": "FictionBook", ".mobi": "Mobipocket book",
	".doc": "Word document", ".docx": "Word document", ".xls": "Excel sheet",
	".xlsx": "Excel sheet", ".ppt": "PowerPoint deck", ".pptx": "PowerPoint deck",
	".zip": "ZIP archive", ".tar": "Tar archive", ".gz": "Gzip archive",
	".tgz": "Gzip tarball", ".bz2": "Bzip2 archive", ".xz": "XZ archive",
	".7z": "7-Zip archive", ".rar": "RAR archive", ".dmg": "Disk image",
	".jar": "Java archive", ".iso": "Disc image",
	".ttf": "TrueType font", ".otf": "OpenType font", ".woff": "Web font", ".woff2": "Web font",
	".sqlite": "SQLite database", ".db": "Database",
}

// kindNameMap names files known by their whole name.
var kindNameMap = map[string]string{
	"Makefile": "Makefile", "Dockerfile": "Dockerfile", "LICENSE": "Licence",
	".gitignore": "Git ignore rules", ".gitattributes": "Git attributes",
	".editorconfig": "EditorConfig", "go.mod": "Go module", "go.sum": "Go checksums",
	"package.json": "npm package", "composer.json": "Composer package",
}

// kindOf names the type of entry. head is the start of the file, if read.
func kindOf(entry filesystem.FileEntry, head []byte) string {
	switch {
	case entry.IsDir:
		return "Folder"
	case entry.IsSymlink && entry.LinkTarget == "":
		return "Alias"
	}
	if k, ok := kindNameMap[entry.Name]; ok {
		return k
	}
	if k, ok := kindNames[strings.ToLower(entry.Extension)]; ok {
		return k
	}
	if k := binaryKind(head); k != "" {
		return k
	}
	if interp := shebang(head); interp != "" {
		return interp + " script"
	}
	if entry.IsExec {
		return "Executable"
	}
	if entry.Extension != "" {
		return strings.ToUpper(strings.TrimPrefix(entry.Extension, ".")) + " file"
	}
	return "File"
}

// binaryKind recognises executables by their magic number.
func binaryKind(head []byte) string {
	if len(head) < 4 {
		return ""
	}
	switch {
	case bytes.Equal(head[:4], []byte{0xcf, 0xfa, 0xed, 0xfe}),
		bytes.Equal(head[:4], []byte{0xce, 0xfa, 0xed, 0xfe}):
		return "Mach-O executable"
	case bytes.Equal(head[:4], []byte{0xca, 0xfe, 0xba, 0xbe}):
		return "Universal executable"
	case bytes.Equal(head[:4], []byte{0x7f, 'E', 'L', 'F'}):
		return "ELF executable"
	}
	return ""
}

// shebang returns the interpreter named on a "#!" first line: "bash",
// "python3", "node" ("#!/usr/bin/env node" counts too).
func shebang(head []byte) string {
	if !bytes.HasPrefix(head, []byte("#!")) {
		return ""
	}
	line, _, _ := bytes.Cut(head[2:], []byte("\n"))
	fields := strings.Fields(string(line))
	if len(fields) == 0 {
		return ""
	}
	interp := filepath.Base(fields[0])
	if interp == "env" {
		for _, f := range fields[1:] {
			if !strings.HasPrefix(f, "-") {
				return filepath.Base(f)
			}
		}
		return ""
	}
	return interp
}

// ── Per-type details ───────────────────────────────────────────────────

// textDetails describes a text file: line count, encoding, line endings.
func textDetails(data []byte) []Detail {
	if len(data) == 0 {
		return []Detail{{"Lines", "empty"}}
	}
	lines := bytes.Count(data, []byte("\n"))
	if data[len(data)-1] != '\n' {
		lines++
	}
	crlf := bytes.Count(data, []byte("\r\n"))
	lf := bytes.Count(data, []byte("\n")) - crlf
	endings := "LF"
	switch {
	case crlf > 0 && lf > 0:
		endings = "mixed LF / CRLF"
	case crlf > 0:
		endings = "CRLF"
	case lf == 0:
		endings = "no line breaks"
	}
	enc := "UTF-8"
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		enc = "UTF-8 with BOM"
	}
	return []Detail{
		{"Lines", strconv.Itoa(lines)},
		{"Encoding", enc + " · " + endings},
	}
}

// mediaDetails describes images, video and PDFs from Spotlight's index,
// with Go's decoders and ffprobe as fallbacks for unindexed places.
func mediaDetails(path string, kind mediarender.MediaKind) []Detail {
	md := spotlight(path, "kMDItemPixelWidth", "kMDItemPixelHeight",
		"kMDItemDurationSeconds", "kMDItemNumberOfPages", "kMDItemTitle")
	w, h := md["kMDItemPixelWidth"], md["kMDItemPixelHeight"]

	var out []Detail
	switch kind {
	case mediarender.KindImage:
		if w == "" {
			if f, err := os.Open(path); err == nil {
				if cfg, _, err := image.DecodeConfig(f); err == nil {
					w, h = strconv.Itoa(cfg.Width), strconv.Itoa(cfg.Height)
				}
				f.Close()
			}
		}
		if w != "" && h != "" {
			out = append(out, Detail{"Dimensions", w + " × " + h})
		}
	case mediarender.KindVideo:
		dur := md["kMDItemDurationSeconds"]
		if dur == "" || w == "" {
			if pw, ph, pd, ok := ffprobe(path); ok {
				w, h, dur = pw, ph, pd
			}
		}
		if secs, err := strconv.ParseFloat(dur, 64); err == nil && secs > 0 {
			out = append(out, Detail{"Duration", formatDuration(secs)})
		}
		if w != "" && h != "" {
			out = append(out, Detail{"Dimensions", w + " × " + h})
		}
	case mediarender.KindPDF:
		if p := md["kMDItemNumberOfPages"]; p != "" {
			out = append(out, Detail{"Pages", p})
		}
		if t := md["kMDItemTitle"]; t != "" {
			out = append(out, Detail{"Title", t})
		}
	}
	return out
}

// zipDetails counts what a zip archive holds. Only the central directory
// at the end of the file is read, so it is cheap however big the archive.
func zipDetails(path string) []Detail {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil
	}
	defer r.Close()
	files := 0
	var unpacked uint64
	for _, f := range r.File {
		if !f.FileInfo().IsDir() {
			files++
			unpacked += f.UncompressedSize64
		}
	}
	noun := "files"
	if files == 1 {
		noun = "file"
	}
	return []Detail{{"Contains", fmt.Sprintf("%d %s · %s unpacked", files, noun,
		filesystem.FormatSize(int64(unpacked)))}}
}

// dirItems describes a folder's direct contents: "14 · 3 folders, 11 files".
func dirItems(path string) string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "unreadable"
	}
	dirs := 0
	for _, e := range entries {
		if e.IsDir() {
			dirs++
		}
	}
	files := len(entries) - dirs
	if len(entries) == 0 {
		return "empty"
	}
	plural := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.Itoa(n) + " " + many
	}
	switch {
	case dirs == 0:
		return plural(files, "file", "files")
	case files == 0:
		return plural(dirs, "folder", "folders")
	}
	return fmt.Sprintf("%d · %s, %s", len(entries), plural(dirs, "folder", "folders"), plural(files, "file", "files"))
}

// ── Helpers ────────────────────────────────────────────────────────────

// spotlight reads attributes from the Spotlight index (`mdls`). Missing
// attributes, unindexed volumes and a missing mdls all just yield nothing.
func spotlight(path string, keys ...string) map[string]string {
	args := make([]string, 0, 2*len(keys)+1)
	for _, k := range keys {
		args = append(args, "-name", k)
	}
	args = append(args, path)
	out, err := exec.Command("mdls", args...).Output()
	res := map[string]string{}
	if err != nil {
		return res
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		// kMDItemPixelWidth = 2940    kMDItemTitle = "Report"
		key, val, ok := strings.Cut(sc.Text(), " = ")
		if !ok {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"`)
		if val != "(null)" && val != "" && val != "(" {
			res[strings.TrimSpace(key)] = val
		}
	}
	return res
}

// ffprobe returns width, height and duration of a video, if ffprobe is
// installed.
func ffprobe(path string) (w, h, dur string, ok bool) {
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height:format=duration",
		"-of", "default=noprint_wrappers=1", path).Output()
	if err != nil {
		return "", "", "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, _ := strings.Cut(line, "=")
		switch k {
		case "width":
			w = v
		case "height":
			h = v
		case "duration":
			dur = v
		}
	}
	return w, h, dur, w != "" || dur != ""
}

func formatDuration(secs float64) string {
	d := time.Duration(secs * float64(time.Second)).Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// gitLabel turns a porcelain status code ("XY") into words.
func gitLabel(code string) string {
	if len(code) != 2 {
		return ""
	}
	switch code {
	case "??":
		return "untracked"
	case "!!":
		return "ignored"
	}
	describe := func(c byte) string {
		switch c {
		case 'M':
			return "modified"
		case 'A':
			return "added"
		case 'D':
			return "deleted"
		case 'R':
			return "renamed"
		case 'C':
			return "copied"
		case 'U':
			return "conflicted"
		case 'T':
			return "type changed"
		}
		return ""
	}
	staged, unstaged := describe(code[0]), describe(code[1])
	switch {
	case code[0] == 'U' || code[1] == 'U':
		return "conflicted"
	case staged != "" && unstaged != "":
		return staged + " (staged), then " + unstaged
	case staged != "":
		return staged + ", staged"
	case unstaged != "":
		return unstaged + ", not staged"
	}
	return ""
}
