// Package theme provides the Tokyo Night inspired color palette
// and Lip Gloss styles for tuiple TUI file manager.
package theme

import "github.com/charmbracelet/lipgloss"

// ── Color Palette (Tokyo Night) ────────────────────────────────────────

var (
	// Backgrounds (used sparingly to preserve terminal transparency where possible)
	BgColor      = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#222436"}
	BgDarkColor  = lipgloss.AdaptiveColor{Light: "#f1f5f9", Dark: "#1e2030"}
	BgLightColor = lipgloss.AdaptiveColor{Light: "#e2e8f0", Dark: "#2f334d"}
	// BgSelected is the cursor of whichever panel has the keyboard;
	// BgHighlight is the same row once the focus has moved elsewhere —
	// still findable, no longer claiming attention.
	BgHighlight = lipgloss.AdaptiveColor{Light: "#eef2f7", Dark: "#2a2e48"}
	BgSelected  = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#2d3f76"}

	// FgColor      = lipgloss.AdaptiveColor{Light: "#0f172a", Dark: "#c0caf5"}
	FgColor      = lipgloss.AdaptiveColor{Light: "#0f172a", Dark: "#c8d3f5"}
	FgDimColor   = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#828bb8"}
	FgMutedColor = lipgloss.AdaptiveColor{Light: "#94a3b8", Dark: "#636da6"}

	// Accents (deep, vivid colors for Light mode / bright neon for Dark mode)
	AccentBlue    = lipgloss.AdaptiveColor{Light: "#1d4ed8", Dark: "#82aaff"}
	AccentCyan    = lipgloss.AdaptiveColor{Light: "#0891b2", Dark: "#86e1fc"}
	AccentGreen   = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#c3e88d"}
	AccentRed     = lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#ff757f"}
	AccentYellow  = lipgloss.AdaptiveColor{Light: "#b45309", Dark: "#ffc777"}
	AccentMagenta = lipgloss.AdaptiveColor{Light: "#7e22ce", Dark: "#c099ff"}
	AccentOrange  = lipgloss.AdaptiveColor{Light: "#c2410c", Dark: "#ff966c"}

	// BackdropColor is what the panels are drawn in while a popup floats
	// over them: present, but out of the way.
	BackdropColor = lipgloss.AdaptiveColor{Light: "#c5ccd8", Dark: "#474d75"}

	// Separators & borders
	SeparatorColor = lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#444a73"}
	BorderColor    = lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#444a73"}
	BorderActive   = lipgloss.AdaptiveColor{Light: "#1d4ed8", Dark: "#82aaff"}
)

// ── Generic text styles ────────────────────────────────────────────────

var (
	Bold   = lipgloss.NewStyle().Bold(true)
	Dim    = lipgloss.NewStyle().Foreground(FgDimColor)
	Muted  = lipgloss.NewStyle().Foreground(FgMutedColor)
	Normal = lipgloss.NewStyle().Foreground(FgColor)
)

// ── Sidebar ────────────────────────────────────────────────────────────

var (
	SidebarSection = lipgloss.NewStyle().
			Foreground(FgMutedColor).
			Bold(true)

	SidebarItem = lipgloss.NewStyle().
			Foreground(FgColor)

	SidebarSelected = lipgloss.NewStyle().
			Foreground(AccentBlue).
			Bold(true)

	SidebarActive = lipgloss.NewStyle().
			Foreground(BgColor).
			Background(AccentBlue).
			Bold(true)
)

// ── File list ──────────────────────────────────────────────────────────

var (
	FileName = lipgloss.NewStyle().
			Foreground(FgColor)

	DirName = lipgloss.NewStyle().
		Foreground(AccentBlue).
		Bold(true)

	ExecName = lipgloss.NewStyle().
			Foreground(AccentGreen).
			Bold(true)

	SymlinkName = lipgloss.NewStyle().
			Foreground(AccentCyan)

	HiddenName = lipgloss.NewStyle().
			Foreground(FgDimColor)

	FileSize = lipgloss.NewStyle().
			Foreground(FgMutedColor)

	FileDate = lipgloss.NewStyle().
			Foreground(FgDimColor)

	ListCursor = lipgloss.NewStyle().
			Background(BgSelected).
			Foreground(FgColor).
			Bold(true)

	ListHeader = lipgloss.NewStyle().
			Foreground(FgDimColor).
			Bold(true)

	// ListHeaderActive marks the column the list is sorted by.
	ListHeaderActive = lipgloss.NewStyle().
				Foreground(FgColor).
				Bold(true)
)

// GitUntracked colours a file git does not know about yet — the reddish
// brown a JetBrains project tree uses for unversioned files.
var GitUntracked = lipgloss.AdaptiveColor{Light: "#b4532a", Dark: "#ff966c"}

// ── Status bar ─────────────────────────────────────────────────────────

var (
	StatusBar = lipgloss.NewStyle().
			Foreground(FgDimColor).
			Background(BgDarkColor)

	StatusPath = lipgloss.NewStyle().
			Foreground(AccentBlue).
			Bold(true)

	StatusInfo = lipgloss.NewStyle().
			Foreground(FgMutedColor)
)

// ── Breadcrumb ─────────────────────────────────────────────────────────

var (
	BreadcrumbNormal = lipgloss.NewStyle().
				Foreground(FgMutedColor)

	BreadcrumbActive = lipgloss.NewStyle().
				Foreground(AccentBlue).
				Bold(true)

	BreadcrumbSep = lipgloss.NewStyle().
			Foreground(FgDimColor)
)

// ── Preview ────────────────────────────────────────────────────────────

var (
	PreviewTitle = lipgloss.NewStyle().
			Foreground(AccentYellow).
			Bold(true)

	// PreviewName heads the preview panel: the file's name in the text
	// colour, so the one loud thing on screen stays the cursor.
	PreviewName = lipgloss.NewStyle().
			Foreground(FgColor).
			Bold(true)

	PreviewContent = lipgloss.NewStyle().
			Foreground(FgDimColor)

	PreviewLineNum = lipgloss.NewStyle().
			Foreground(FgDimColor)

	PreviewInfo = lipgloss.NewStyle().
			Foreground(FgMutedColor)

	ErrorMsg = lipgloss.NewStyle().
			Foreground(AccentRed)
)

// ── Help ───────────────────────────────────────────────────────────────

var (
	HelpKey = lipgloss.NewStyle().
		Foreground(AccentBlue).
		Bold(true)

	HelpDesc = lipgloss.NewStyle().
			Foreground(FgDimColor)
)

// ── Separator ──────────────────────────────────────────────────────────

var Separator = lipgloss.NewStyle().
	Foreground(SeparatorColor)
