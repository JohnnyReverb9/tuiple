// Package theme provides the Tokyo Night inspired color palette
// and Lip Gloss styles for tuiple TUI file manager.
package theme

import "github.com/charmbracelet/lipgloss"

// ── Color Palette (Tokyo Night) ────────────────────────────────────────

var (
	// Backgrounds
	BgColor      = lipgloss.Color("#1a1b26")
	BgDarkColor  = lipgloss.Color("#16161e")
	BgLightColor = lipgloss.Color("#24283b")
	BgHighlight  = lipgloss.Color("#292e42")
	BgSelected   = lipgloss.Color("#33467c")

	// Foregrounds
	FgColor      = lipgloss.Color("#c0caf5")
	FgDimColor   = lipgloss.Color("#565f89")
	FgMutedColor = lipgloss.Color("#737aa2")

	// Accents
	AccentBlue    = lipgloss.Color("#7aa2f7")
	AccentCyan    = lipgloss.Color("#2ac3de")
	AccentGreen   = lipgloss.Color("#9ece6a")
	AccentRed     = lipgloss.Color("#f7768e")
	AccentYellow  = lipgloss.Color("#e0af68")
	AccentMagenta = lipgloss.Color("#bb9af7")
	AccentOrange  = lipgloss.Color("#ff9e64")

	// Separators & borders
	SeparatorColor = lipgloss.Color("#3b3d54")
	BorderColor    = lipgloss.Color("#3b3d54")
	BorderActive   = lipgloss.Color("#7aa2f7")
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
