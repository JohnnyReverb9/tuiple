package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/JohnnyReverb9/tuiple/theme"
)

// floatOver draws a popup on top of the panels instead of in place of
// them. What is behind the popup stays where it was but loses its
// colour, so the eye goes to the box and the place you were working in
// is still recognisable around it.
//
// x and y are the popup's top-left corner within base; width is the
// width every line of base is expected to have.
func floatOver(base, box string, width, x, y int) string {
	backdrop := lipgloss.NewStyle().Foreground(theme.BackdropColor)
	baseLines := strings.Split(base, "\n")
	boxLines := strings.Split(box, "\n")

	for i, line := range baseLines {
		// Colour is dropped by going back to the bare text. That also
		// makes cutting the line trivial: there is no style left to
		// carry across the cut.
		plain := fit(ansi.Strip(line), width)

		bi := i - y
		if bi < 0 || bi >= len(boxLines) {
			baseLines[i] = backdrop.Render(plain)
			continue
		}
		boxW := lipgloss.Width(boxLines[bi])
		left := fit(ansi.Cut(plain, 0, x), x)
		right := fit(ansi.Cut(plain, x+boxW, width), width-x-boxW)
		baseLines[i] = backdrop.Render(left) + boxLines[bi] + backdrop.Render(right)
	}
	return strings.Join(baseLines, "\n")
}

// fit makes s exactly w cells wide, padding or cutting as needed. A wide
// character straddling a cut comes out a cell short, which is why the
// padding matters even for text that was the right width going in.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := ansi.StringWidth(s)
	switch {
	case sw < w:
		return s + strings.Repeat(" ", w-sw)
	case sw > w:
		s = ansi.Truncate(s, w, "")
		if sw = ansi.StringWidth(s); sw < w {
			s += strings.Repeat(" ", w-sw)
		}
	}
	return s
}

// centred places a popup in the middle of an area of the given size and
// returns its top-left corner.
func centred(box string, width, height int) (x, y int) {
	x = max(0, (width-lipgloss.Width(box))/2)
	y = max(0, (height-lipgloss.Height(box))/2)
	return x, y
}
