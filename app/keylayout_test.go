package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// Every Cyrillic rune must translate to exactly one QWERTY key, and no
// two runes may claim the same one — a duplicate would silently shadow a
// shortcut, which is the exact failure this table exists to prevent.
func TestCyrillicMapIsOneToOne(t *testing.T) {
	seen := make(map[rune]rune, len(cyrillicToLatin))
	for cyr, lat := range cyrillicToLatin {
		if prev, dup := seen[lat]; dup {
			t.Errorf("%q and %q both map to %q", prev, cyr, lat)
		}
		seen[lat] = cyr
	}
	// Spot-check the three rows and the shifted punctuation, which is
	// where a transcription slip would be easiest to miss.
	for _, tc := range []struct{ in, want rune }{
		{'ф', 'a'}, {'ы', 's'}, {'в', 'd'}, // home row
		{'й', 'q'}, {'з', 'p'}, // top row
		{'я', 'z'}, {'ю', '.'}, // bottom row
		{'Ж', ':'}, {'Э', '"'}, {'Ё', '~'}, // shifted punctuation
	} {
		if got := cyrillicToLatin[tc.in]; got != tc.want {
			t.Errorf("%q maps to %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A Cyrillic keystroke must reach the command handlers as its QWERTY
// equivalent: ":" (typed as Ж) opens the go-to-path prompt.
func TestCyrillicKeyTriggersShortcut(t *testing.T) {
	m := New(t.TempDir())
	updated, _ := m.Update(key('Ж'))
	got := updated.(Model)
	if got.dialogMode != DialogGoToPath {
		t.Fatalf("dialogMode = %v, want DialogGoToPath", got.dialogMode)
	}
}

// While a prompt is open the same keystrokes are text, not commands:
// typing Cyrillic into the path prompt must arrive verbatim.
func TestTextInputKeepsCyrillic(t *testing.T) {
	m := New(t.TempDir())
	updated, _ := m.Update(key('Ж')) // open the prompt
	model := updated.(Model)

	for _, r := range []rune{'п', 'а', 'п', 'к', 'а'} {
		updated, _ = model.Update(key(r))
		model = updated.(Model)
	}
	if got := model.textInput.Value(); got != "папка" {
		t.Fatalf("textInput = %q, want %q", got, "папка")
	}
}

// The "/" key types "." on a Cyrillic layout, which is indistinguishable
// from an English ".". It may only be re-mapped once a Cyrillic letter
// has established which layout is in use.
func TestAmbiguousPunctuationFollowsLayout(t *testing.T) {
	m := New(t.TempDir())

	// English layout: "," stays "," and does not open help.
	updated, _ := m.Update(key('j')) // a Latin letter: layout is English
	model := updated.(Model)
	updated, _ = model.Update(key(','))
	if updated.(Model).showHelp {
		t.Fatal("','  opened help while the layout was English")
	}

	// Cyrillic layout: the same physical key types "," and must reach
	// the handlers as "?".
	updated, _ = m.Update(key('о')) // a Cyrillic letter: layout is RU
	model = updated.(Model)
	updated, _ = model.Update(key(','))
	if !updated.(Model).showHelp {
		t.Fatal("',' did not open help while the layout was Cyrillic")
	}
}

// Named keys and control combinations are layout-independent already and
// must pass through untouched.
func TestNonRuneKeysUnchanged(t *testing.T) {
	m := New(t.TempDir())
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyEnter},
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlG},
		{Type: tea.KeyUp},
	} {
		before := k.String()
		if after := m.normalizeKey(k).String(); after != before {
			t.Errorf("%s became %s", before, after)
		}
	}
	// Alt-combinations keep their modifier while the rune is translated.
	alt := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'ф'}, Alt: true}
	if got := m.normalizeKey(alt).String(); got != "alt+a" {
		t.Errorf("alt+ф became %q, want %q", got, "alt+a")
	}
}

// Settings rows are built from live config, so every tab must render
// without a nil accessor sneaking in.
func TestSettingsRowsRender(t *testing.T) {
	p := NewSettingsPopup()
	p.SetSize(100, 30)
	p.Start()
	for tab := tabDelete; tab <= tabSystem; tab++ {
		p.setTab(tab)
		for _, r := range p.rows() {
			if r.kind == settingInfo {
				continue
			}
			if r.label == "" {
				t.Errorf("tab %d: selectable row without a label", tab)
			}
			if r.value == nil {
				t.Errorf("tab %d: row %q has no value renderer", tab, r.label)
			}
		}
		if view := p.View(); strings.TrimSpace(view) == "" {
			t.Errorf("tab %d rendered empty", tab)
		}
	}
}

// Paths reach open-with commands through `sh -c`, so quoting has to
// survive spaces and quotes in file names.
func TestShellQuote(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"/tmp/a.txt", "'/tmp/a.txt'"},
		{"/tmp/my file.txt", "'/tmp/my file.txt'"},
		{"/tmp/it's.txt", `'/tmp/it'\''s.txt'`},
	} {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
