package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNvimSettingsDefaultsAndPartialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv("NVIM_IDE_SETTINGS", path)

	s, err := LoadNvim()
	if err != nil || s.Colorscheme != "tokyonight-day" || s.TabWidth != 4 || !s.Languages["go"] {
		t.Fatalf("defaults = %+v, %v", s, err)
	}

	// A hand-written partial file keeps the defaults for everything else.
	os.WriteFile(path, []byte(`{"tab_width": 2, "languages": {"php": false}}`), 0o644)
	s, err = LoadNvim()
	if err != nil || s.TabWidth != 2 || s.Languages["php"] || !s.Languages["go"] || s.EnterAccepts != "after_arrows" {
		t.Fatalf("partial = %+v, %v", s, err)
	}

	// "languages": null must not crash.
	os.WriteFile(path, []byte(`{"languages": null}`), 0o644)
	if s, err = LoadNvim(); err != nil || !s.Languages["python"] {
		t.Fatalf("null languages = %+v, %v", s, err)
	}
}

func TestUpdateNvimWritesSnakeCaseJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	t.Setenv("NVIM_IDE_SETTINGS", path)

	if err := UpdateNvim(func(s *NvimSettings) {
		s.Margin = 100
		s.Languages["go"] = false
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"margin": 100`, `"go": false`, `"enter_accepts": "after_arrows"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("file lacks %s:\n%s", want, data)
		}
	}
	// A corrupt file is replaced by the next change instead of blocking it.
	os.WriteFile(path, []byte("{oops"), 0o644)
	if err := UpdateNvim(func(s *NvimSettings) { s.Wrap = true }); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadNvim(); err != nil || !s.Wrap {
		t.Fatalf("after corrupt = %+v, %v", s, err)
	}
}

func TestNvimConnected(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("NVIM_APPNAME", "")

	if NvimConnected("nvim") {
		t.Fatal("connected without the nvim-ide config")
	}
	dir := filepath.Join(xdg, "nvim", "lua", "config")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "ide.lua"), []byte("return {}"), 0o644)

	for editor, want := range map[string]bool{
		"nvim":                      true,
		"/opt/homebrew/bin/nvim -p": true,
		"vim":                       false,
		"code --wait":               false,
		"":                          false,
	} {
		if got := NvimConnected(editor); got != want {
			t.Errorf("NvimConnected(%q) = %v, want %v", editor, got, want)
		}
	}
}
