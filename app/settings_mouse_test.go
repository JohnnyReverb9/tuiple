package app

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/JohnnyReverb9/tuiple/config"
)

func TestSettingsMouse(t *testing.T) {
	t.Setenv("TUIPLE_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("NVIM_IDE_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))
	config.Load()
	t.Cleanup(func() { config.Load() })

	p := NewSettingsPopup()
	p.SetSize(110, 30)
	p.Start()
	click := func(x, y int) {
		p, _ = p.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	}

	// Tabs: " Settings" + 4 spaces, then " Delete ·" " Files ·" …
	click(1+len(" Settings")+4+len(" Delete ·")+2, 1)
	if p.tab != tabFiles {
		t.Fatalf("tab = %d", p.tab)
	}

	// Find the "Show hidden files"-style first toggle on this tab and
	// click its value: it must flip.
	rows := p.rows()
	idx := -1
	for i, r := range rows {
		if r.kind == settingToggle {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Skip("no toggle on the Files tab")
	}
	before := rows[idx].value(config.Get())
	click(settingsValueX+1, 4+idx)
	if p.cursor != idx {
		t.Fatalf("cursor = %d, want %d", p.cursor, idx)
	}
	if after := p.rows()[idx].value(config.Get()); after == before {
		t.Fatalf("value did not change: %s", after)
	}
	// A click on the label only selects.
	before = p.rows()[idx].value(config.Get())
	click(5, 4+idx)
	if p.rows()[idx].value(config.Get()) != before {
		t.Fatal("a click on the label changed the value")
	}
}
