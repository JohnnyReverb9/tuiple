package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/JohnnyReverb9/tuiple/filesystem"
)

// NvimSettings mirrors the settings file of the nvim-ide config shipped in
// this repository (nvim-ide/nvim/lua/config/ide.lua reads the same file).
// The settings screen shows them on its Neovim tab when the editor is
// that config; Neovim picks changes up on its next start.
type NvimSettings struct {
	// Look
	Colorscheme  string `json:"colorscheme"`
	LineNumbers  string `json:"line_numbers"`  // absolute | relative | off
	InsertCursor string `json:"insert_cursor"` // block | bar
	Cursorline   bool   `json:"cursorline"`
	Margin       int    `json:"margin"` // right-margin column, 0 = none

	// Editing
	TabWidth     int  `json:"tab_width"`
	ExpandTab    bool `json:"expand_tab"`
	Wrap         bool `json:"wrap"`
	Autosave     bool `json:"autosave"`
	Clipboard    bool `json:"clipboard"`
	FormatOnSave bool `json:"format_on_save"`

	// Completion
	EnterAccepts  string `json:"enter_accepts"` // after_arrows | always | never
	GhostText     bool   `json:"ghost_text"`
	AutoDocs      bool   `json:"auto_docs"`
	SignatureHelp bool   `json:"signature_help"`

	// Languages
	Languages map[string]bool `json:"languages"`
	PHPLSP    string          `json:"php_lsp"`    // intelephense | phpactor
	PythonLSP string          `json:"python_lsp"` // basedpyright | pyright
}

// NvimLanguage is one language switch on the Neovim tab.
type NvimLanguage struct{ Key, Label string }

// NvimLanguages lists the language switches in display order. The keys
// match `languages` in ide.lua.
var NvimLanguages = []NvimLanguage{
	{"go", "Go"},
	{"python", "Python"},
	{"php", "PHP"},
	{"typescript", "JavaScript / TypeScript"},
	{"web", "HTML / CSS / Emmet"},
	{"tailwind", "Tailwind CSS"},
	{"c", "C / C++ / CMake"},
	{"shell", "Shell"},
	{"sql", "SQL"},
	{"docker", "Docker"},
	{"json", "JSON"},
	{"yaml", "YAML"},
	{"toml", "TOML"},
	{"markdown", "Markdown"},
	{"git", "Git files"},
}

// Choice lists for the Neovim tab, in stepping order.
var (
	NvimColorschemes = []string{
		"tokyonight-day", "tokyonight-storm", "tokyonight-moon", "tokyonight-night",
		"catppuccin-latte", "catppuccin-frappe", "catppuccin-macchiato", "catppuccin-mocha",
		"habamax",
	}
	NvimLineNumbers  = []string{"absolute", "relative", "off"}
	NvimCursors      = []string{"block", "bar"}
	NvimMargins      = []int{0, 80, 100, 120}
	NvimTabWidths    = []int{2, 4, 8}
	NvimEnterAccepts = []string{"after_arrows", "always", "never"}
	NvimPHPLSPs      = []string{"intelephense", "phpactor"}
	NvimPythonLSPs   = []string{"basedpyright", "pyright"}
)

// NvimDefaults returns the defaults ide.lua uses for a missing file.
func NvimDefaults() NvimSettings {
	langs := make(map[string]bool, len(NvimLanguages))
	for _, l := range NvimLanguages {
		langs[l.Key] = true
	}
	return NvimSettings{
		Colorscheme:   "tokyonight-day",
		LineNumbers:   "absolute",
		InsertCursor:  "block",
		Cursorline:    true,
		TabWidth:      4,
		Autosave:      true,
		Clipboard:     true,
		FormatOnSave:  true,
		EnterAccepts:  "after_arrows",
		GhostText:     true,
		AutoDocs:      true,
		SignatureHelp: true,
		Languages:     langs,
		PHPLSP:        "intelephense",
		PythonLSP:     "basedpyright",
	}
}

var nvimMu sync.Mutex

// NvimSettingsPath is where the settings live ($NVIM_IDE_SETTINGS wins,
// as it does in ide.lua).
func NvimSettingsPath() string {
	if p := strings.TrimSpace(os.Getenv("NVIM_IDE_SETTINGS")); p != "" {
		return p
	}
	return filepath.Join(filesystem.HomeDir(), ".config", "nvim-ide", "settings.json")
}

// LoadNvim reads the settings file over the defaults. A missing file is
// just the defaults.
func LoadNvim() (NvimSettings, error) {
	nvimMu.Lock()
	defer nvimMu.Unlock()
	return loadNvim()
}

func loadNvim() (NvimSettings, error) {
	s := NvimDefaults()
	data, err := os.ReadFile(NvimSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	defaultLangs := s.Languages
	if err := json.Unmarshal(data, &s); err != nil {
		return NvimDefaults(), err
	}
	// A partial "languages" object keeps the defaults for the rest.
	if s.Languages == nil {
		s.Languages = map[string]bool{}
	}
	for k, v := range defaultLangs {
		if _, ok := s.Languages[k]; !ok {
			s.Languages[k] = v
		}
	}
	return s, nil
}

// UpdateNvim applies fn to the current settings and writes them back.
func UpdateNvim(fn func(*NvimSettings)) error {
	nvimMu.Lock()
	defer nvimMu.Unlock()
	s, err := loadNvim()
	if err != nil {
		// A corrupt file is replaced rather than blocking every change.
		s = NvimDefaults()
	}
	fn(&s)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := NvimSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// NvimConnected reports whether editor launches Neovim with the nvim-ide
// config, i.e. whether the Neovim settings have any effect.
func NvimConnected(editor string) bool {
	fields := strings.Fields(editor)
	if len(fields) == 0 || filepath.Base(fields[0]) != "nvim" {
		return false
	}
	_, err := os.Stat(filepath.Join(nvimConfigDir(), "lua", "config", "ide.lua"))
	return err == nil
}

// nvimConfigDir is Neovim's stdpath("config").
func nvimConfigDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(filesystem.HomeDir(), ".config")
	}
	app := os.Getenv("NVIM_APPNAME")
	if app == "" {
		app = "nvim"
	}
	return filepath.Join(base, app)
}
