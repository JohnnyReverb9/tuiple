// Package mediarender converts images, video thumbnails and PDF pages
// into colored Unicode half-block (▀) strings suitable for terminal display.
package mediarender

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// ── File type detection ────────────────────────────────────────────────

// MediaKind describes the category of a media file.
type MediaKind int

const (
	KindNone  MediaKind = iota
	KindImage           // png, jpg, gif, bmp, webp, tiff
	KindVideo           // mp4, avi, mkv, mov, webm
	KindPDF             // pdf
)

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".bmp": true, ".webp": true, ".tiff": true, ".tif": true,
}

var videoExts = map[string]bool{
	".mp4": true, ".avi": true, ".mkv": true, ".mov": true,
	".webm": true, ".m4v": true, ".wmv": true,
}

// Classify returns the media kind based on file extension.
func Classify(ext string) MediaKind {
	ext = strings.ToLower(ext)
	if imageExts[ext] {
		return KindImage
	}
	if videoExts[ext] {
		return KindVideo
	}
	if ext == ".pdf" {
		return KindPDF
	}
	return KindNone
}

// ── Public render functions ────────────────────────────────────────────

// RenderImage reads an image file and returns a half-block string
// that fits within the given cell dimensions (cols × rows).
func RenderImage(path string, cols, rows int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open image: %w", err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}

	return imageToHalfBlocks(img, cols, rows), nil
}

// RenderVideoThumb extracts a frame from a video file using ffmpeg
// and renders it as half-blocks.  Returns an info string if ffmpeg
// is not installed.
func RenderVideoThumb(path string, cols, rows int) (string, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return formatMediaPlaceholder("📹", "Video file", filepath.Base(path),
			"Install ffmpeg for thumbnail preview"), nil
	}

	// Create a temporary file for the extracted frame
	tmp, err := os.CreateTemp("", "tuiple-vthumb-*.png")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	// Extract a frame at 1 second into the video
	cmd := exec.Command(ffmpeg,
		"-y",           // overwrite output
		"-ss", "1",     // seek to 1s
		"-i", path,     // input
		"-frames:v", "1", // extract 1 frame
		"-q:v", "2",    // good quality
		tmpPath,
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		// Try at 0s if 1s fails (very short video)
		cmd2 := exec.Command(ffmpeg,
			"-y", "-ss", "0", "-i", path,
			"-frames:v", "1", "-q:v", "2", tmpPath,
		)
		cmd2.Stdout = nil
		cmd2.Stderr = nil
		if err2 := cmd2.Run(); err2 != nil {
			return formatMediaPlaceholder("📹", "Video file", filepath.Base(path),
				"Could not extract thumbnail"), nil
		}
	}

	return RenderImage(tmpPath, cols, rows)
}

// RenderPDFThumb renders the first page of a PDF using macOS qlmanage.
// Returns an info string on non-macOS or if the tool is missing.
func RenderPDFThumb(path string, cols, rows int) (string, error) {
	qlmanage, err := exec.LookPath("qlmanage")
	if err != nil {
		return formatMediaPlaceholder("📄", "PDF document", filepath.Base(path),
			"PDF preview available only on macOS"), nil
	}

	// Create a temp directory for qlmanage output
	tmpDir, err := os.MkdirTemp("", "tuiple-pdf-")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// qlmanage -t -s 800 -o /tmp/dir file.pdf
	// Generates a thumbnail PNG in the output directory
	cmd := exec.Command(qlmanage, "-t", "-s", "800", "-o", tmpDir, path)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return formatMediaPlaceholder("📄", "PDF document", filepath.Base(path),
			"Could not generate PDF thumbnail"), nil
	}

	// qlmanage creates <filename>.pdf.png in the output dir
	base := filepath.Base(path)
	thumbPath := filepath.Join(tmpDir, base+".png")
	if _, err := os.Stat(thumbPath); err != nil {
		// Try to find any PNG in the output dir
		entries, _ := os.ReadDir(tmpDir)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".png") {
				thumbPath = filepath.Join(tmpDir, e.Name())
				break
			}
		}
	}

	return RenderImage(thumbPath, cols, rows)
}

// ── Half-block renderer ────────────────────────────────────────────────

// pixel holds an 8-bit RGB color extracted from the scaled image.
type pixel struct{ r, g, b uint8 }

// imageToHalfBlocks converts an image to a colored half-block string.
// Each character cell ▀ encodes two vertical pixels:
//   - foreground color = top pixel
//   - background color = bottom pixel
//
// Uses lipgloss colors (not raw ANSI) so the output is fully compatible
// with Bubbletea's diff-renderer and lipgloss.Place layout engine.
func imageToHalfBlocks(img image.Image, cols, rows int) string {
	if cols <= 0 || rows <= 0 {
		return ""
	}

	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return ""
	}

	// Each row of text encodes 2 pixel rows, so target pixel height = rows*2
	pixelRows := rows * 2

	// Calculate aspect-preserving dimensions
	fitW, fitH := fitDimensions(srcW, srcH, cols, pixelRows)
	if fitW <= 0 || fitH <= 0 {
		return ""
	}

	// Ensure fitH is even (we need pairs of pixel rows)
	if fitH%2 != 0 {
		fitH++
	}

	// Scale the image using high-quality bilinear interpolation.
	// draw.Src copies source pixels directly (no alpha compositing
	// against transparent black), which preserves colors for opaque
	// formats like JPEG.
	scaled := image.NewRGBA(image.Rect(0, 0, fitW, fitH))
	draw.BiLinear.Scale(scaled, scaled.Bounds(), img, bounds, draw.Src, nil)

	// Read all pixels into a flat array for fast indexed access.
	// scaled.At() goes through an interface and allocates; this avoids that.
	pixels := make([]pixel, fitW*fitH)
	for y := 0; y < fitH; y++ {
		for x := 0; x < fitW; x++ {
			off := y*scaled.Stride + x*4
			pixels[y*fitW+x] = pixel{
				r: scaled.Pix[off+0],
				g: scaled.Pix[off+1],
				b: scaled.Pix[off+2],
			}
		}
	}

	// Center the image horizontally
	padLeft := (cols - fitW) / 2
	if padLeft < 0 {
		padLeft = 0
	}
	leftPad := strings.Repeat(" ", padLeft)

	// Build the half-block string line by line.
	// Each line is assembled by batching consecutive pixels that share
	// the same fg+bg color into a single lipgloss.Render() call.
	// This keeps the escape-sequence count manageable and ensures
	// lipgloss can correctly measure visible widths.
	textRows := fitH / 2
	lines := make([]string, textRows)

	for row := 0; row < textRows; row++ {
		var lb strings.Builder
		lb.Grow(fitW*4 + len(leftPad))
		lb.WriteString(leftPad)

		// Walk columns, batching runs of identical color pairs.
		col := 0
		for col < fitW {
			top := pixels[row*2*fitW+col]
			bot := pixels[(row*2+1)*fitW+col]

			// Count how many consecutive columns share this exact color pair.
			runLen := 1
			for col+runLen < fitW {
				nt := pixels[row*2*fitW+col+runLen]
				nb := pixels[(row*2+1)*fitW+col+runLen]
				if nt != top || nb != bot {
					break
				}
				runLen++
			}

			// Render the run using lipgloss (automatic color profile support).
			fgHex := fmt.Sprintf("#%02x%02x%02x", top.r, top.g, top.b)
			bgHex := fmt.Sprintf("#%02x%02x%02x", bot.r, bot.g, bot.b)

			style := lipgloss.NewStyle().
				Foreground(lipgloss.Color(fgHex)).
				Background(lipgloss.Color(bgHex))

			lb.WriteString(style.Render(strings.Repeat("▀", runLen)))
			col += runLen
		}

		lines[row] = lb.String()
	}

	return strings.Join(lines, "\n")
}

// fitDimensions calculates dimensions that fit within maxW×maxH
// while preserving aspect ratio.
func fitDimensions(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return 0, 0
	}

	ratioW := float64(maxW) / float64(srcW)
	ratioH := float64(maxH) / float64(srcH)

	ratio := ratioW
	if ratioH < ratioW {
		ratio = ratioH
	}

	w := int(float64(srcW) * ratio)
	h := int(float64(srcH) * ratio)

	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	return w, h
}

// ── Placeholder helper ─────────────────────────────────────────────────

func formatMediaPlaceholder(icon, kind, name, hint string) string {
	return fmt.Sprintf("\n  %s  %s\n\n  %s\n\n  %s",
		icon, kind, name, hint)
}
