package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/JohnnyReverb9/tuiple/config"
)

// nvimRows is the Neovim tab: the settings of the nvim-ide config, stored
// in their own file (config.NvimSettingsPath) that Neovim reads at start.
// The rows mirror the ones backed by config.json, but save through
// apply / resetTo instead of step / reset.
func (h SettingsPopup) nvimRows() []settingRow {
	nv := h.nv
	def := config.NvimDefaults()
	save := config.UpdateNvim

	toggle := func(label, desc string, get func(*config.NvimSettings) *bool) settingRow {
		return settingRow{
			kind:    settingToggle,
			label:   label,
			desc:    desc,
			value:   func(config.Config) string { return onOff(*get(&nv)) },
			apply:   func(int) error { return save(func(s *config.NvimSettings) { *get(s) = !*get(s) }) },
			resetTo: func() error { return save(func(s *config.NvimSettings) { *get(s) = *get(&def) }) },
		}
	}
	choice := func(label, desc string, opts []string, get func(*config.NvimSettings) *string) settingRow {
		return settingRow{
			kind:    settingChoice,
			label:   label,
			desc:    desc,
			value:   func(config.Config) string { return strings.ReplaceAll(*get(&nv), "_", " ") },
			apply:   func(dir int) error { return save(func(s *config.NvimSettings) { *get(s) = cycle(*get(s), opts, dir) }) },
			resetTo: func() error { return save(func(s *config.NvimSettings) { *get(s) = *get(&def) }) },
		}
	}
	number := func(label, desc string, opts []int, show func(int) string, get func(*config.NvimSettings) *int) settingRow {
		strs := make([]string, len(opts))
		for i, o := range opts {
			strs[i] = strconv.Itoa(o)
		}
		return settingRow{
			kind:  settingChoice,
			label: label,
			desc:  desc,
			value: func(config.Config) string { return show(*get(&nv)) },
			apply: func(dir int) error {
				return save(func(s *config.NvimSettings) {
					*get(s), _ = strconv.Atoi(cycle(strconv.Itoa(*get(s)), strs, dir))
				})
			},
			resetTo: func() error { return save(func(s *config.NvimSettings) { *get(s) = *get(&def) }) },
		}
	}
	section := func(title string) []settingRow {
		return []settingRow{{kind: settingInfo}, {kind: settingInfo, label: title}}
	}

	rows := []settingRow{{kind: settingInfo, label: "Look"}}
	rows = append(rows,
		choice("Color scheme", "tokyonight and catppuccin in all their variants, or Vim's habamax",
			config.NvimColorschemes, func(s *config.NvimSettings) *string { return &s.Colorscheme }),
		choice("Line numbers", "absolute like the IDE gutter, relative for vim jumps (5j), or none",
			config.NvimLineNumbers, func(s *config.NvimSettings) *string { return &s.LineNumbers }),
		choice("Cursor in insert mode", "a block everywhere, or a thin bar while typing",
			config.NvimCursors, func(s *config.NvimSettings) *string { return &s.InsertCursor }),
		toggle("Highlight current line", "a band behind the line the cursor is on",
			func(s *config.NvimSettings) *bool { return &s.Cursorline }),
		number("Right margin line", "a vertical guide at this column",
			config.NvimMargins, func(v int) string {
				if v == 0 {
					return "off"
				}
				return fmt.Sprintf("at %d", v)
			}, func(s *config.NvimSettings) *int { return &s.Margin }),
	)

	rows = append(rows, section("Editing")...)
	rows = append(rows,
		number("Tab width", "columns per indent level; formatters still follow each language's style",
			config.NvimTabWidths, strconv.Itoa, func(s *config.NvimSettings) *int { return &s.TabWidth }),
		settingRow{
			kind:  settingChoice,
			label: "Indent with",
			desc:  "real tab characters, or spaces",
			value: func(config.Config) string {
				if nv.ExpandTab {
					return "spaces"
				}
				return "tabs"
			},
			apply:   func(int) error { return save(func(s *config.NvimSettings) { s.ExpandTab = !s.ExpandTab }) },
			resetTo: func() error { return save(func(s *config.NvimSettings) { s.ExpandTab = def.ExpandTab }) },
		},
		toggle("Wrap long lines", "soft-wrap lines wider than the window",
			func(s *config.NvimSettings) *bool { return &s.Wrap }),
		toggle("Autosave", "write files when switching buffers, like the IDE",
			func(s *config.NvimSettings) *bool { return &s.Autosave }),
		toggle("macOS clipboard", "y and p use the system clipboard",
			func(s *config.NvimSettings) *bool { return &s.Clipboard }),
		toggle("Format on save", "run the language's formatter on every write",
			func(s *config.NvimSettings) *bool { return &s.FormatOnSave }),
	)

	rows = append(rows, section("Completion")...)
	enter := choice("Enter accepts", "", config.NvimEnterAccepts,
		func(s *config.NvimSettings) *string { return &s.EnterAccepts })
	enter.dyn = func(config.Config) string {
		switch nv.EnterAccepts {
		case "always":
			return "Enter takes the highlighted suggestion, like PhpStorm (Tab always does)"
		case "never":
			return "Enter is always a new line; Tab accepts"
		}
		return "Enter accepts only after you picked a suggestion with the arrows; Tab always does"
	}
	rows = append(rows,
		enter,
		toggle("Inline preview", "the suggestion shown in grey right in the text",
			func(s *config.NvimSettings) *bool { return &s.GhostText }),
		toggle("Documentation popup", "docs next to the suggestion list",
			func(s *config.NvimSettings) *bool { return &s.AutoDocs }),
		toggle("Parameter hints", "the signature while typing a call's arguments",
			func(s *config.NvimSettings) *bool { return &s.SignatureHelp }),
	)

	rows = append(rows, section("Languages  (servers install on the next start)")...)
	for _, l := range config.NvimLanguages {
		key := l.Key
		rows = append(rows, settingRow{
			kind:  settingToggle,
			label: l.Label,
			desc:  "language server, formatter, linter and debugger for " + l.Label,
			value: func(config.Config) string { return onOff(nv.Languages[key]) },
			apply: func(int) error {
				return save(func(s *config.NvimSettings) { s.Languages[key] = !s.Languages[key] })
			},
			resetTo: func() error {
				return save(func(s *config.NvimSettings) { s.Languages[key] = def.Languages[key] })
			},
		})
	}
	php := choice("PHP server", "intelephense is closest to PhpStorm's analysis; phpactor is fully open source",
		config.NvimPHPLSPs, func(s *config.NvimSettings) *string { return &s.PHPLSP })
	php.muted = !nv.Languages["php"]
	py := choice("Python server", "basedpyright (stricter, more features) or pyright",
		config.NvimPythonLSPs, func(s *config.NvimSettings) *string { return &s.PythonLSP })
	py.muted = !nv.Languages["python"]
	rows = append(rows, php, py)

	rows = append(rows,
		settingRow{kind: settingInfo},
		settingRow{kind: settingInfo, label: "Saved to " + config.NvimSettingsPath()},
		settingRow{kind: settingInfo, label: "Neovim reads it on start: restart it, or run :restart inside."},
	)
	return rows
}
