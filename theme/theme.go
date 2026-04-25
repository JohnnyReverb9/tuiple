// Package theme provides the Tokyo Night inspired color palette
// and Lip Gloss styles for tuiple TUI file manager.
package theme

import "github.com/charmbracelet/lipgloss"

// ── Color Palette (Tokyo Night) ────────────────────────────────────────

var (
	// Backgrounds (used sparingly to preserve terminal transparency where possible)
	BgColor      = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#1a1b26"}
	BgDarkColor  = lipgloss.AdaptiveColor{Light: "#f1f5f9", Dark: "#16161e"} // transparent native or slight off-white
	BgLightColor = lipgloss.AdaptiveColor{Light: "#e2e8f0", Dark: "#24283b"}
	BgHighlight  = lipgloss.AdaptiveColor{Light: "#f8fafc", Dark: "#292e42"}
	BgSelected   = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#33467c"}

	// Foregrounds
	FgColor      = lipgloss.AdaptiveColor{Light: "#0f172a", Dark: "#c0caf5"}
	FgDimColor   = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#565f89"}
	FgMutedColor = lipgloss.AdaptiveColor{Light: "#94a3b8", Dark: "#737aa2"}

	// Accents (deep, vivid colors for Light mode / bright neon for Dark mode)
	AccentBlue    = lipgloss.AdaptiveColor{Light: "#1d4ed8", Dark: "#7aa2f7"} // Strong deep blue
	AccentCyan    = lipgloss.AdaptiveColor{Light: "#0891b2", Dark: "#2ac3de"}
	AccentGreen   = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#9ece6a"}
	AccentRed     = lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#f7768e"}
	AccentYellow  = lipgloss.AdaptiveColor{Light: "#b45309", Dark: "#e0af68"}
	AccentMagenta = lipgloss.AdaptiveColor{Light: "#7e22ce", Dark: "#bb9af7"}
	AccentOrange  = lipgloss.AdaptiveColor{Light: "#c2410c", Dark: "#ff9e64"}

	// Separators & borders
	SeparatorColor = lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#3b3d54"}
	BorderColor    = lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#3b3d54"}
	BorderActive   = lipgloss.AdaptiveColor{Light: "#1d4ed8", Dark: "#7aa2f7"}
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
			Foreground(FgDimColor).
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
)

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

var (
	Separator = lipgloss.NewStyle().
			Foreground(SeparatorColor)
)
