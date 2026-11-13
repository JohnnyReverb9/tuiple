// Package icons maps file types to Unicode symbols and colors.
// Designed to work with SF Mono and standard macOS Terminal
// (no Nerd Font required).
package icons

import "github.com/charmbracelet/lipgloss"

// Icon pairs a display symbol with a terminal color.
type Icon struct {
	Symbol string
	Color  lipgloss.AdaptiveColor
}

// Render returns the icon symbol with its color applied.
func (i Icon) Render() string {
	return lipgloss.NewStyle().Foreground(i.Color).Render(i.Symbol)
}

// ── Predefined icons ───────────────────────────────────────────────────
//
// Each colour comes as a pair. The dark half is the Tokyo Night accent
// the icons have always had; on a white background those pastels wash
// out to nothing, so the light half is the same hue taken a good deal
// deeper.

func tint(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

var (
	blue   = tint("#2563eb", "#7aa2f7")
	slate  = tint("#64748b", "#a9b1d6")
	green  = tint("#15803d", "#9ece6a")
	amber  = tint("#b45309", "#e0af68")
	violet = tint("#7e22ce", "#bb9af7")
	rose   = tint("#be123c", "#f7768e")
	cyan   = tint("#0e7490", "#2ac3de")

	DirIcon     = Icon{"▸", blue}
	FileDefault = Icon{"·", slate}
	FileText    = Icon{"≡", slate}
	FileCode    = Icon{"◇", green}
	FileConfig  = Icon{"◈", amber}
	FileImage   = Icon{"◉", violet}
	FileVideo   = Icon{"▶", rose}
	FileAudio   = Icon{"♪", cyan}
	FileArchive = Icon{"◆", rose}
	FileExec    = Icon{"*", green}
	FileLink    = Icon{"→", cyan}
	FileGit     = Icon{"●", rose}
	FileMD      = Icon{"¶", blue}
	FilePDF     = Icon{"□", rose}
	FileDB      = Icon{"⊞", amber}
	FileLock    = Icon{"▣", rose}
	FileJSON    = Icon{"{", amber}
)

// ── Extension → Icon mapping ───────────────────────────────────────────

var extMap = map[string]Icon{
	// Text
	".txt": FileText, ".log": FileText, ".csv": FileText,

	// Code
	".go": FileCode, ".py": FileCode, ".js": FileCode, ".ts": FileCode,
	".rs": FileCode, ".c": FileCode, ".cpp": FileCode, ".h": FileCode,
	".java": FileCode, ".rb": FileCode, ".php": FileCode, ".swift": FileCode,
	".kt": FileCode, ".lua": FileCode, ".sh": FileCode, ".bash": FileCode,
	".zsh": FileCode, ".fish": FileCode, ".html": FileCode, ".css": FileCode,
	".scss": FileCode, ".vue": FileCode, ".jsx": FileCode, ".tsx": FileCode,
	".sql": FileCode, ".r": FileCode, ".dart": FileCode, ".zig": FileCode,

	// Config
	".toml": FileConfig, ".yaml": FileConfig, ".yml": FileConfig,
	".ini": FileConfig, ".conf": FileConfig, ".env": FileConfig,
	".cfg": FileConfig, ".xml": FileConfig, ".plist": FileConfig,

	// Data
	".json": FileJSON, ".jsonl": FileJSON,

	// Markdown
	".md": FileMD, ".markdown": FileMD, ".mdx": FileMD, ".rst": FileMD,

	// Images
	".png": FileImage, ".jpg": FileImage, ".jpeg": FileImage, ".gif": FileImage,
	".bmp": FileImage, ".svg": FileImage, ".webp": FileImage, ".ico": FileImage,
	".tiff": FileImage, ".heic": FileImage,

	// Video
	".mp4": FileVideo, ".avi": FileVideo, ".mkv": FileVideo, ".mov": FileVideo,
	".wmv": FileVideo, ".webm": FileVideo, ".m4v": FileVideo,

	// Audio
	".mp3": FileAudio, ".wav": FileAudio, ".flac": FileAudio, ".aac": FileAudio,
	".ogg": FileAudio, ".m4a": FileAudio, ".wma": FileAudio,

	// Archives
	".zip": FileArchive, ".tar": FileArchive, ".gz": FileArchive,
	".bz2": FileArchive, ".xz": FileArchive, ".7z": FileArchive,
	".rar": FileArchive, ".dmg": FileArchive, ".iso": FileArchive,

	// PDF
	".pdf": FilePDF,

	// Database
	".db": FileDB, ".sqlite": FileDB, ".sqlite3": FileDB,

	// Lock
	".lock": FileLock,
}

// ── Filename → Icon mapping ────────────────────────────────────────────

var nameMap = map[string]Icon{
	"Makefile":            FileConfig,
	"Dockerfile":          FileConfig,
	"docker-compose.yml":  FileConfig,
	"docker-compose.yaml": FileConfig,
	".gitignore":          FileGit,
	".gitmodules":         FileGit,
	".gitattributes":      FileGit,
	"go.mod":              FileConfig,
	"go.sum":              FileLock,
	"package.json":        FileJSON,
	"package-lock.json":   FileLock,
	"tsconfig.json":       FileJSON,
	"Cargo.toml":          FileConfig,
	"Cargo.lock":          FileLock,
	"LICENSE":             FileText,
	"README":              FileMD,
	"README.md":           FileMD,
	"CHANGELOG":           FileText,
	"CHANGELOG.md":        FileMD,
	".env":                FileConfig,
	".env.local":          FileConfig,
	".dockerignore":       FileConfig,
	".editorconfig":       FileConfig,
	"flake.nix":           FileConfig,
	"flake.lock":          FileLock,
}

// GetIcon returns the appropriate icon for a file entry.
func GetIcon(name, ext string, isDir, isExec, isSymlink bool) Icon {
	if isSymlink {
		return FileLink
	}
	if isDir {
		return DirIcon
	}
	if icon, ok := nameMap[name]; ok {
		return icon
	}
	if icon, ok := extMap[ext]; ok {
		return icon
	}
	if isExec {
		return FileExec
	}
	return FileDefault
}
