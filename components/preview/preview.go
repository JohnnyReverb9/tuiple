// Package preview implements the right panel that shows file/dir content.
package preview

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/components/preview/mediarender"
	"tuiple/filesystem"
	"tuiple/theme"
)

const (
	maxFileSize      = 1 << 20 // 1 MB
	maxMediaFileSize = 50 << 20 // 50 MB for media files
)

// ContentLoadedMsg carries preview content back to the model.
type ContentLoadedMsg struct {
	Path    string
	Content string
	IsDir   bool
	IsMedia bool // true when content is half-block rendered media
	Info    FileInfo
}

// FileInfo holds metadata displayed at the top of the preview.
type FileInfo struct {
	Name    string
	Size    int64
	ModTime string
	Perms   string
	Items   int // dir only
}

// ── Model ──────────────────────────────────────────────────────────────

// Model is the preview panel's Bubble Tea model.
type Model struct {
	content      string
	info         FileInfo
	isDir        bool
	isMedia      bool
	path         string
	scrollOffset int
	contentLines int
	width        int
	height       int
	focused      bool
}

// New creates an empty preview model.
func New() Model { return Model{} }

// ── Size / focus setters ───────────────────────────────────────────────

func (m *Model) SetSize(w, h int) { m.width = w; m.height = h }
func (m *Model) SetFocused(f bool) { m.focused = f }

// ── LoadFile ───────────────────────────────────────────────────────────

// LoadFile returns a tea.Cmd that reads a file/dir asynchronously.
// It passes the current panel dimensions so media can be rendered at
// the correct size.
func (m Model) LoadFile(entry filesystem.FileEntry) tea.Cmd {
	previewW := m.width
	previewH := m.height
	return func() tea.Msg {
		info := FileInfo{
			Name:    entry.Name,
			Size:    entry.Size,
			ModTime: filesystem.FormatTime(entry.ModTime),
			Perms:   entry.Mode.String(),
		}

		// ── Directory preview ──────────────────────────────────────
		if entry.IsDir {
			info.Items = filesystem.DirItemCount(entry.Path)
			entries, _ := filesystem.ReadDir(entry.Path, false)
			var lines []string
			for i, e := range entries {
				if i >= 20 {
					lines = append(lines, fmt.Sprintf("  … and %d more", len(entries)-20))
					break
				}
				name := e.Name
				if e.IsDir {
					name += "/"
				}
				lines = append(lines, "  "+name)
			}
			return ContentLoadedMsg{
				Path: entry.Path, Content: strings.Join(lines, "\n"),
				IsDir: true, Info: info,
			}
		}

		// ── Media preview (images, video, PDF) ─────────────────────
		mediaKind := mediarender.Classify(entry.Extension)
		if mediaKind != mediarender.KindNone {
			// Allow larger files for media (up to 50 MB)
			if entry.Size > maxMediaFileSize {
				return ContentLoadedMsg{
					Path: entry.Path, Content: "(media file too large to preview)",
					Info: info,
				}
			}

			// Reserve space for title + metadata + spacer
			renderCols := max(1, previewW-2)
			renderRows := max(1, previewH-8)

			var content string
			var renderErr error

			switch mediaKind {
			case mediarender.KindImage:
				content, renderErr = mediarender.RenderImage(entry.Path, renderCols, renderRows)
			case mediarender.KindVideo:
				content, renderErr = mediarender.RenderVideoThumb(entry.Path, renderCols, renderRows)
			case mediarender.KindPDF:
				content, renderErr = mediarender.RenderPDFThumb(entry.Path, renderCols, renderRows)
			}

			if renderErr != nil {
				content = fmt.Sprintf("  (preview error: %v)", renderErr)
			}

			return ContentLoadedMsg{
				Path: entry.Path, Content: content,
				IsMedia: true, Info: info,
			}
		}

		// ── Large file guard ───────────────────────────────────────
		if entry.Size > maxFileSize {
			return ContentLoadedMsg{
				Path: entry.Path, Content: "(file too large to preview)",
				Info: info,
			}
		}

		// ── Read file ──────────────────────────────────────────────
		data, err := os.ReadFile(entry.Path)
		if err != nil {
			return ContentLoadedMsg{
				Path: entry.Path, Content: fmt.Sprintf("(cannot read: %v)", err),
				Info: info,
			}
		}

		// Binary → hex dump
		if !utf8.Valid(data) {
			return ContentLoadedMsg{
				Path: entry.Path, Content: formatHexDump(data),
				Info: info,
			}
		}

		// Text → full up to 1MB size limit
		content := string(data)
		return ContentLoadedMsg{
			Path: entry.Path, Content: content,
			Info: info,
		}
	}
}

// ── Update ─────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ContentLoadedMsg:
		m.path = msg.Path
		m.content = msg.Content
		m.contentLines = strings.Count(msg.Content, "\n") + 1
		m.info = msg.Info
		m.isDir = msg.IsDir
		m.isMedia = msg.IsMedia
		m.scrollOffset = 0
		return m, nil

	case tea.MouseMsg:
		maxScroll := max(0, m.contentLines-max(1, m.height-7))
		if msg.Type == tea.MouseWheelUp {
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}
		} else if msg.Type == tea.MouseWheelDown {
			if m.scrollOffset < maxScroll {
				m.scrollOffset++
			}
		}
		return m, nil

	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}
		maxScroll := max(0, m.contentLines-max(1, m.height-7))
		switch msg.String() {
		case "up", "k":
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}
		case "down", "j":
			if m.scrollOffset < maxScroll {
				m.scrollOffset++
			}
		case "g":
			m.scrollOffset = 0
		case "G":
			m.scrollOffset = maxScroll
		}
	}

	return m, nil
}

// ── View ───────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width <= 0 || m.path == "" {
		return theme.Dim.Render(" No file selected")
	}

	var sections []string

	// Title
	title := theme.PreviewTitle.Render(m.info.Name)
	sections = append(sections, title)

	// Metadata
	infoLines := []string{
		theme.PreviewInfo.Render(fmt.Sprintf("Size:  %s", filesystem.FormatSize(m.info.Size))),
		theme.PreviewInfo.Render(fmt.Sprintf("Date:  %s", m.info.ModTime)),
		theme.PreviewInfo.Render(fmt.Sprintf("Perms: %s", m.info.Perms)),
	}
	if m.isDir {
		infoLines = append(infoLines,
			theme.PreviewInfo.Render(fmt.Sprintf("Items: %d", m.info.Items)),
		)
	}
	sections = append(sections, strings.Join(infoLines, "\n"))
	sections = append(sections, "") // spacer

	// Content (scrollable)
	if m.content != "" {
		lines := strings.Split(m.content, "\n")
		availH := max(1, m.height-7) // reserve space for title+info+spacer

		// Clamp scroll efficiently to prevent jitter
		maxScroll := max(0, len(lines)-availH)
		if m.scrollOffset > maxScroll {
			m.scrollOffset = maxScroll
		}
		start := m.scrollOffset

		end := min(start+availH, len(lines))

		if start < len(lines) {
			visible := make([]string, end-start)
			for i := start; i < end; i++ {
				if m.isMedia {
					// Media content is already ANSI-colored half-blocks;
					// render without line numbers to preserve alignment.
					visible[i-start] = lines[i]
				} else {
					lineNum := theme.PreviewLineNum.
						Width(4).Align(lipgloss.Right).
						Render(fmt.Sprintf("%d", i+1))
					lineContent := theme.PreviewContent.
						MaxWidth(max(1, m.width-6)).
						Render(lines[i])
					visible[i-start] = lineNum + " " + lineContent
				}
			}
			sections = append(sections, strings.Join(visible, "\n"))
		}
	}

	return strings.Join(sections, "\n")
}

// ── Hex dump helper ────────────────────────────────────────────────────

func formatHexDump(data []byte) string {
	if len(data) > 512 {
		data = data[:512]
	}
	bytesPerLine := 16
	var lines []string
	for i := 0; i < len(data); i += bytesPerLine {
		end := i + bytesPerLine
		if end > len(data) {
			end = len(data)
		}
		hex := ""
		ascii := ""
		for j := i; j < end; j++ {
			hex += fmt.Sprintf("%02x ", data[j])
			if data[j] >= 32 && data[j] < 127 {
				ascii += string(rune(data[j]))
			} else {
				ascii += "."
			}
		}
		lines = append(lines, fmt.Sprintf("%08x  %-*s %s", i, bytesPerLine*3, hex, ascii))
	}
	return strings.Join(lines, "\n")
}
