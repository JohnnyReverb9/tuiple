package app

import (
	"reflect"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Cmd shortcuts.
//
// A terminal never sees the Command key on its own — the terminal
// emulator keeps it. The iTerm2 profile that nvim-ide installs gets
// around that by sending each Cmd combination as a "CSI u" escape
// sequence (ESC [ 115 ; 9 u for Cmd+S), which is how the same keys reach
// Neovim. tuiple runs in that same terminal, so it can read them too,
// and Cmd+E, Cmd+Shift+O or Cmd+[ then mean here what they mean in the
// editor and in the IDE they were borrowed from.
//
// Bubble Tea v1 does not decode these sequences. It hands them over as
// an unexported "unknown CSI" message, so the bytes are fished out with
// reflection; if a later version starts decoding them itself this file
// simply stops matching anything and the plain keys carry on working.

// chordFromMsg returns the key name ("cmd+shift+o") of a CSI-u message.
func chordFromMsg(msg tea.Msg) (string, bool) {
	if msg == nil {
		return "", false
	}
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 {
		return "", false
	}
	if v.Type().Name() != "unknownCSISequenceMsg" {
		return "", false
	}
	return parseChord(v.Bytes())
}

// parseChord decodes the two shapes the iTerm profile sends:
//
//	ESC [ <codepoint> ; <modifiers> u     a key with modifiers
//	ESC [ 1 ; <modifiers> A|B|C|D         an arrow with modifiers
//
// The modifier field is 1 plus a bit set: shift 1, alt 2, ctrl 4, cmd 8.
func parseChord(seq []byte) (string, bool) {
	if len(seq) < 4 || seq[0] != 0x1b || seq[1] != '[' {
		return "", false
	}
	final := seq[len(seq)-1]
	fields := strings.Split(string(seq[2:len(seq)-1]), ";")
	if len(fields) != 2 {
		return "", false
	}
	code, err1 := strconv.Atoi(fields[0])
	mods, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil || mods < 2 {
		return "", false
	}
	mods--

	var name string
	switch final {
	case 'u':
		switch {
		case code == 9:
			name = "tab"
		case code == 13:
			name = "enter"
		case code == 27:
			name = "esc"
		case code == 32:
			name = "space"
		case code == 127 || code == 8:
			name = "backspace"
		case code > 32 && code < 127:
			name = strings.ToLower(string(rune(code)))
		default:
			return "", false
		}
	case 'A':
		name = "up"
	case 'B':
		name = "down"
	case 'C':
		name = "right"
	case 'D':
		name = "left"
	default:
		return "", false
	}

	// Modifier order is fixed, so a binding has exactly one spelling.
	var b strings.Builder
	if mods&8 != 0 {
		b.WriteString("cmd+")
	}
	if mods&4 != 0 {
		b.WriteString("ctrl+")
	}
	if mods&2 != 0 {
		b.WriteString("alt+")
	}
	if mods&1 != 0 {
		b.WriteString("shift+")
	}
	b.WriteString(name)
	return b.String(), true
}
