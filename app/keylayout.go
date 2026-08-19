package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"tuiple/config"
)

// Keyboard-layout normalisation.
//
// Every shortcut in tuiple is matched against msg.String(), which carries
// the character the terminal produced — not the physical key that was
// pressed. With a Cyrillic layout active the terminal reports "ф" where
// the binding expects "a", so nothing works until the user switches back
// to English.
//
// The fix is to translate each Cyrillic rune to whatever the key in the
// same physical position types on a QWERTY layout, before the message
// reaches any component. Text entry is exempt (see acceptsText) — a
// rename dialog or a commit message must still be able to receive
// Cyrillic verbatim.

// cyrillicToLatin maps ЙЦУКЕН runes onto the QWERTY key sharing their
// physical position: 'ф' and 'a' live on the same key, so do 'Ж' and ':'.
var cyrillicToLatin = map[rune]rune{
	// ─ Top letter row ─
	'й': 'q', 'ц': 'w', 'у': 'e', 'к': 'r', 'е': 't', 'н': 'y',
	'г': 'u', 'ш': 'i', 'щ': 'o', 'з': 'p', 'х': '[', 'ъ': ']',
	'Й': 'Q', 'Ц': 'W', 'У': 'E', 'К': 'R', 'Е': 'T', 'Н': 'Y',
	'Г': 'U', 'Ш': 'I', 'Щ': 'O', 'З': 'P', 'Х': '{', 'Ъ': '}',

	// ─ Home row ─
	'ф': 'a', 'ы': 's', 'в': 'd', 'а': 'f', 'п': 'g',
	'р': 'h', 'о': 'j', 'л': 'k', 'д': 'l', 'ж': ';', 'э': '\'',
	'Ф': 'A', 'Ы': 'S', 'В': 'D', 'А': 'F', 'П': 'G',
	'Р': 'H', 'О': 'J', 'Л': 'K', 'Д': 'L', 'Ж': ':', 'Э': '"',

	// ─ Bottom row ─
	'я': 'z', 'ч': 'x', 'с': 'c', 'м': 'v', 'и': 'b',
	'т': 'n', 'ь': 'm', 'б': ',', 'ю': '.',
	'Я': 'Z', 'Ч': 'X', 'С': 'C', 'М': 'V', 'И': 'B',
	'Т': 'N', 'Ь': 'M', 'Б': '<', 'Ю': '>',

	// ─ Number-row edges ─
	'ё': '`', 'Ё': '~', '№': '#',
}

// cyrillicPunct covers the two keys whose Cyrillic output is itself
// ASCII: on ЙЦУКЕН the physical "/" key types "." and, shifted, ",".
// Those runes are indistinguishable from a genuine English "." or ",",
// so they are only translated while the layout is known to be Cyrillic —
// see Model.cyrillicActive.
var cyrillicPunct = map[rune]rune{
	'.': '/',
	',': '?',
}

// normalizeKey rewrites a keystroke produced by a Cyrillic layout into
// its QWERTY equivalent. It also tracks which layout is currently in
// use: every letter that arrives in command mode is unambiguous evidence
// one way or the other, and that knowledge is what lets the two ASCII
// punctuation keys above be handled correctly.
func (m *Model) normalizeKey(k tea.KeyMsg) tea.KeyMsg {
	if !config.Get().Keys.CyrillicLayout {
		return k
	}
	// Ctrl-combinations and named keys (arrows, enter, esc…) arrive with
	// a dedicated Type and are layout-independent already.
	if k.Type != tea.KeyRunes || len(k.Runes) != 1 {
		return k
	}

	r := k.Runes[0]
	if latin, ok := cyrillicToLatin[r]; ok {
		m.cyrillicActive = true
		k.Runes = []rune{latin}
		return k
	}
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
		m.cyrillicActive = false
		return k
	}
	if m.cyrillicActive {
		if latin, ok := cyrillicPunct[r]; ok {
			k.Runes = []rune{latin}
		}
	}
	return k
}

// acceptsText reports whether some part of the UI is currently reading
// free-form text: a dialog prompt, the list filter, a search query, a
// commit message, a branch name, a popup filter. While that is true the
// keystrokes belong to the user, not to the keymap, and must reach the
// input untouched.
func (m Model) acceptsText() bool {
	return m.dialogMode != DialogNone ||
		m.filelist.IsFiltering() ||
		m.searchOverlay.AcceptsText() ||
		m.gitOverlay.AcceptsText() ||
		m.branchesPopup.AcceptsText() ||
		m.historyPopup.AcceptsText() ||
		m.settingsPopup.AcceptsText()
}
