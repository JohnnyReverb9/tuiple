package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/JohnnyReverb9/tuiple/config"
)

// connectNvim makes the editor nvim with the nvim-ide config installed,
// with every file in temporary directories.
func connectNvim(t *testing.T) string {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("NVIM_APPNAME", "")
	t.Setenv("TUIPLE_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("EDITOR", "nvim")
	settings := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv("NVIM_IDE_SETTINGS", settings)
	dir := filepath.Join(xdg, "nvim", "lua", "config")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "ide.lua"), []byte("return {}"), 0o644)
	if err := config.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { config.Load() })
	return settings
}

func TestNeovimTabOnlyWhenConnected(t *testing.T) {
	t.Setenv("TUIPLE_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("EDITOR", "vim")
	config.Load()
	t.Cleanup(func() { config.Load() })
	p := NewSettingsPopup()
	if got := p.tabTitles(); len(got) != len(settingsTabTitles) {
		t.Fatalf("tabs with vim = %q", got)
	}

	connectNvim(t)
	if got := p.tabTitles(); got[len(got)-1] != "Neovim" {
		t.Fatalf("tabs with nvim = %q", got)
	}
}

func TestNeovimTabChangesSettingsFile(t *testing.T) {
	path := connectNvim(t)
	p := NewSettingsPopup()
	p.SetSize(110, 30)
	p.Start()
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("7")})
	if p.tab != tabNeovim {
		t.Fatalf("tab = %d", p.tab)
	}
	if !strings.Contains(p.View(), "Color scheme") {
		t.Fatalf("view:\n%s", p.View())
	}

	// First row is the colour scheme: → steps it.
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRight})
	s, _ := config.LoadNvim()
	if s.Colorscheme != "tokyonight-storm" {
		t.Fatalf("colorscheme = %q, status %q", s.Colorscheme, p.status)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("settings file not written")
	}

	// Walk down to the Go language row and switch it off with Enter.
	for i := 0; i < 60 && p.currentRow().label != "Go"; i++ {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if p.currentRow().label != "Go" {
		t.Fatal("Go row not reachable")
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s, _ := config.LoadNvim(); s.Languages["go"] {
		t.Fatal("Go still on")
	}
	// The row scrolled into view and shows the new value.
	if !strings.Contains(p.View(), "[ ] off") {
		t.Fatalf("view after toggle:\n%s", p.View())
	}
	// r restores the default.
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if s, _ := config.LoadNvim(); !s.Languages["go"] {
		t.Fatal("reset did not restore Go")
	}
}
