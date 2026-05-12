// Package preview implements the right panel that shows file/dir content.
package preview

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
	Path      string
	Content   string
	IsDir     bool
	IsMedia   bool // true when content is half-block rendered media
	IsAudio   bool // true when file is an audio file
	AudioMeta mediarender.AudioInfo
	Info      FileInfo
}

// AudioTickMsg fires every second while audio is playing to update elapsed time.
type AudioTickMsg time.Time

// AudioSeekFinishedMsg is sent when the async ffmpeg seek completes.
type AudioSeekFinishedMsg struct {
	PlayPath string
	Position float64
	Err      error
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
	isAudio      bool
	path         string
	scrollOffset int
	contentLines int
	width        int
	height       int
	focused      bool

	// Audio player state
	audioMeta    mediarender.AudioInfo
	audioCmd     *exec.Cmd // running afplay process
	audioPlaying bool
	audioStart   time.Time  // when playback started
	audioElapsed    float64    // current position in seconds
	audioOffset     float64    // position where current playback started
	audioTmpFile    string     // temp file for seeked playback
	audioWarning    string     // warning message (e.g., missing ffmpeg)
	audioSeeking    bool       // true while ffmpeg is processing a seek
	audioSeekTarget float64    // the target position for the current seek
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

		// ── Media preview (images, video, PDF, audio) ───────────────
		mediaKind := mediarender.Classify(entry.Extension)
		if mediaKind != mediarender.KindNone {
			// Audio files: parse metadata, show player UI
			if mediaKind == mediarender.KindAudio {
				meta := mediarender.ParseAudioInfo(entry.Path)
				return ContentLoadedMsg{
					Path: entry.Path, IsAudio: true,
					AudioMeta: meta, Info: info,
				}
			}

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
		// Stop any playing audio and reset position when switching files
		m.resetAudio()
		m.audioWarning = ""

		m.path = msg.Path
		m.content = msg.Content
		m.contentLines = strings.Count(msg.Content, "\n") + 1
		m.info = msg.Info
		m.isDir = msg.IsDir
		m.isMedia = msg.IsMedia
		m.isAudio = msg.IsAudio
		m.audioMeta = msg.AudioMeta
		m.scrollOffset = 0
		return m, nil

	case AudioSeekFinishedMsg:
		if m.audioSeekTarget != msg.Position {
			// This is a stale seek result; ignore it because a newer seek was requested
			if msg.Err == nil && msg.PlayPath != m.path {
				os.Remove(msg.PlayPath)
			}
			return m, nil
		}
		
		m.audioSeeking = false
		if msg.Err != nil {
			m.audioWarning = "ffmpeg failed to seek"
			return m, m.playFile(m.path, 0)
		}

		if m.audioTmpFile != "" && m.audioTmpFile != msg.PlayPath {
			os.Remove(m.audioTmpFile)
		}
		m.audioTmpFile = msg.PlayPath
		return m, m.playFile(msg.PlayPath, msg.Position)

	case AudioTickMsg:
		if !m.audioPlaying {
			return m, nil
		}
		// Check if process is still running
		if m.audioCmd != nil && m.audioCmd.ProcessState != nil {
			// Process finished
			m.audioPlaying = false
			m.audioCmd = nil
			m.audioElapsed = 0
			return m, nil
		}
		m.audioElapsed = m.audioOffset + time.Since(m.audioStart).Seconds()
		// Cap at duration if known
		if m.audioMeta.Duration > 0 && m.audioElapsed > m.audioMeta.Duration {
			m.stopAudio()
			m.audioOffset = 0
			m.audioElapsed = 0
			return m, nil
		}
		return m, m.audioTickCmd()

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

		// Audio controls (play/stop + seek)
		if m.isAudio {
			switch msg.String() {
			case "l":
				m.audioWarning = ""
				if m.audioPlaying {
					m.stopAudio()
				} else {
					return m, m.startAudio()
				}
				return m, nil
			}
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

// ── Audio API ──────────────────────────────────────────────────────────

func (m Model) IsAudio() bool {
	return m.isAudio
}

func (m Model) Path() string {
	return m.path
}

func (m *Model) ToggleAudio() tea.Cmd {
	if m.audioPlaying {
		m.stopAudio()
		return nil
	}
	return m.startAudio()
}

func (m *Model) SeekAudio(delta float64) tea.Cmd {
	return m.seekAudio(delta)
}

// ── Audio playback ─────────────────────────────────────────────────────

func (m *Model) startAudio() tea.Cmd {
	return m.startAudioAt(m.audioOffset)
}

func (m *Model) startAudioAt(position float64) tea.Cmd {
	m.audioWarning = ""

	// If playing from start, just play immediately
	if position <= 0.5 {
		return m.playFile(m.path, position)
	}

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		m.audioWarning = "Seeking requires 'ffmpeg' (brew install ffmpeg)"
		return m.playFile(m.path, 0)
	}

	m.audioSeeking = true
	m.audioSeekTarget = position

	return func() tea.Msg {
		ext := filepath.Ext(m.path)
		tmpFile := filepath.Join(os.TempDir(), "tuiple-seek"+ext)
		seekCmd := exec.Command(ffmpeg,
			"-y",
			"-ss", fmt.Sprintf("%.2f", position),
			"-i", m.path,
			"-c", "copy",
			tmpFile,
		)
		seekCmd.Stdout = nil
		seekCmd.Stderr = nil
		err := seekCmd.Run()
		return AudioSeekFinishedMsg{
			PlayPath: tmpFile,
			Position: position,
			Err:      err,
		}
	}
}

func (m *Model) playFile(playPath string, position float64) tea.Cmd {
	cmd := exec.Command("afplay", playPath)
	if err := cmd.Start(); err != nil {
		return nil
	}
	m.audioCmd = cmd
	m.audioPlaying = true
	m.audioStart = time.Now()
	m.audioOffset = position
	m.audioElapsed = position

	go func() {
		cmd.Wait()
	}()

	return m.audioTickCmd()
}

func (m *Model) seekAudio(delta float64) tea.Cmd {
	newPos := m.audioElapsed + delta
	if newPos < 0 {
		newPos = 0
	}
	if m.audioMeta.Duration > 0 && newPos >= m.audioMeta.Duration {
		newPos = m.audioMeta.Duration - 0.5
		if newPos < 0 {
			newPos = 0
		}
	}

	if m.audioPlaying {
		m.stopAudio()
		m.audioOffset = newPos
		m.audioElapsed = newPos
		return m.startAudioAt(newPos)
	}

	// When paused, just update position
	m.audioOffset = newPos
	m.audioElapsed = newPos
	return nil
}

func (m *Model) stopAudio() {
	if m.audioCmd != nil && m.audioCmd.Process != nil {
		m.audioCmd.Process.Kill()
	}
	m.audioCmd = nil
	m.audioPlaying = false
	m.audioSeeking = false
	// Preserve position for resume
	m.audioOffset = m.audioElapsed
}

func (m *Model) resetAudio() {
	m.stopAudio()
	m.audioElapsed = 0
	m.audioOffset = 0
	m.audioSeeking = false
	if m.audioTmpFile != "" {
		os.Remove(m.audioTmpFile)
		m.audioTmpFile = ""
	}
}

func (m Model) audioTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return AudioTickMsg(t)
	})
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

	// ── Audio player UI ────────────────────────────────────────────
	if m.isAudio {
		sections = append(sections, m.renderAudioPlayer()...)
		return strings.Join(sections, "\n")
	}

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

	return padPanelLines(strings.Join(sections, "\n"), m.width, m.height)
}

// padPanelLines normalises a panel's output so every terminal row is exactly
// w cells wide and the whole block is at most h rows. Without this, when
// switching to a file whose preview lines are shorter than the previous
// file's lines, Bubble Tea's differential renderer leaves the unwritten
// trailing cells with stale characters from the previous frame — which is
// what causes "previous preview bleeds into the new one" artefacts.
func padPanelLines(s string, w, h int) string {
	if w <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		// One logical row must equal one terminal row.
		if strings.ContainsAny(line, "\r") {
			line = strings.ReplaceAll(line, "\r", "")
		}
		lw := lipgloss.Width(line)
		switch {
		case lw < w:
			line += strings.Repeat(" ", w-lw)
		case lw > w:
			line = lipgloss.NewStyle().MaxWidth(w).Render(line)
		}
		lines[i] = line
	}
	// Ensure the block has at least h rows so trailing rows from a previous
	// (taller) frame get fully overwritten too.
	if h > 0 {
		blank := strings.Repeat(" ", w)
		for len(lines) < h {
			lines = append(lines, blank)
		}
		if len(lines) > h {
			lines = lines[:h]
		}
	}
	return strings.Join(lines, "\n")
}

// renderAudioPlayer builds the audio player UI lines.
func (m Model) renderAudioPlayer() []string {
	var lines []string
	meta := m.audioMeta

	// ── Audio icon ────────────────────────────────────────────────
	lines = append(lines, theme.PreviewInfo.Render("  🎵  Audio File"))
	lines = append(lines, "")

	// ── Metadata ──────────────────────────────────────────────────
	if meta.Codec != "" {
		codec := strings.ToUpper(meta.Codec)
		lines = append(lines, theme.PreviewInfo.Render(
			fmt.Sprintf("  Codec:   %s", codec)))
	}
	if meta.Duration > 0 {
		lines = append(lines, theme.PreviewInfo.Render(
			fmt.Sprintf("  Length:  %s", mediarender.FormatDuration(meta.Duration))))
	}
	if meta.BitRate > 0 {
		kbps := meta.BitRate / 1000
		lines = append(lines, theme.PreviewInfo.Render(
			fmt.Sprintf("  Rate:    %d kbps", kbps)))
	}
	if meta.Sample > 0 {
		sampleKHz := float64(meta.Sample) / 1000.0
		lines = append(lines, theme.PreviewInfo.Render(
			fmt.Sprintf("  Sample:  %.1f kHz", sampleKHz)))
	}
	if meta.Channels > 0 {
		ch := "Mono"
		if meta.Channels >= 2 {
			ch = "Stereo"
		}
		lines = append(lines, theme.PreviewInfo.Render(
			fmt.Sprintf("  Audio:   %s", ch)))
	}

	lines = append(lines, "")

	// ── Progress bar ──────────────────────────────────────────────
	barWidth := max(10, m.width-8)
	elapsed := m.audioElapsed
	total := meta.Duration

	elapsedStr := mediarender.FormatDuration(elapsed)
	totalStr := "--:--"
	if total > 0 {
		totalStr = mediarender.FormatDuration(total)
	}

	// Build the progress bar
	progress := 0.0
	if total > 0 {
		progress = elapsed / total
		if progress > 1 {
			progress = 1
		}
	}
	filled := int(float64(barWidth) * progress)
	empty := barWidth - filled

	bar := "  " + strings.Repeat("▓", filled) + strings.Repeat("░", empty)
	lines = append(lines, theme.PreviewInfo.Render(bar))
	lines = append(lines, theme.PreviewInfo.Render(
		fmt.Sprintf("  %s / %s", elapsedStr, totalStr)))

	lines = append(lines, "")

	// ── Controls ──────────────────────────────────────────────────
	if m.audioSeeking {
		lines = append(lines, theme.PreviewTitle.Render("  ~  Seeking..."))
		lines = append(lines, theme.Dim.Render("  l  stop"))
	} else if m.audioPlaying {
		lines = append(lines, theme.PreviewTitle.Render("  ■  Playing..."))
		lines = append(lines, theme.Dim.Render("  l  stop"))
	} else {
		lines = append(lines, theme.PreviewTitle.Render("  ▶  Paused"))
		lines = append(lines, theme.Dim.Render("  l  play"))
	}
	lines = append(lines, theme.Dim.Render("  -/=  ±5s   _/+  ±30s"))

	if m.audioWarning != "" {
		lines = append(lines, "")
		lines = append(lines, theme.ErrorMsg.Render("  ⚠ "+m.audioWarning))
	}

	return lines
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
