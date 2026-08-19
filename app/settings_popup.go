package app

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuiple/config"
	"tuiple/theme"
)

// SettingsPopup is the editor for ~/.config/tuiple/config.json.
//
// Every row is backed by a live accessor into the config package rather
// than a copy held in the popup, so what the screen shows is always what
// the app is actually using. Changes are written to disk the moment they
// are made — there is no save step to forget, which matches how the
// bookmarks and history features already behave.
//
// The rows are data, not code: each one knows how to render its value,
// how to step it, and how to reset it. Adding a setting means adding a
// literal to rowsFor, not touching the key handling or the renderer.

// settingsChangedMsg is emitted after a setting is modified so the app
// can push the new values into the components that cache them (file
// list sorting, git polling, the preview panel).
type settingsChangedMsg struct{}

// settingsReloadedMsg is emitted after the config file was edited by
// hand in $EDITOR and needs re-reading.
type settingsReloadedMsg struct{}

type settingsTab int

const (
	tabDelete settingsTab = iota
	tabFiles
	tabPreview
	tabLayout
	tabOpening
	tabSystem
)

var settingsTabTitles = []string{"Delete", "Files", "Preview", "Layout", "Opening", "System"}

type settingKind int

const (
	settingToggle settingKind = iota // on / off
	settingChoice                    // one of a fixed list
	settingNumber                    // stepped integer
	settingText                      // free-form string, edited at the bottom
	settingAction                    // Enter runs something
	settingInfo                      // dim, non-selectable line
)

type settingRow struct {
	kind  settingKind
	label string
	desc  string
	// dyn describes the value currently selected, for rows where one
	// sentence per option reads better than all of them at once.
	dyn func(config.Config) string

	// value renders the right-hand column.
	value func(config.Config) string
	// step advances the value by dir (+1 / -1). Toggles ignore dir.
	step func(c *config.Config, dir int)
	// text/commit back the settingText rows.
	text   func(config.Config) string
	commit func(c *config.Config, v string)
	// action backs the settingAction rows.
	action func(h *SettingsPopup) tea.Cmd
	// reset restores this row's shipped default.
	reset func(c *config.Config)
	// muted dims the row when it has no effect in the current mode.
	muted bool
}

func (r settingRow) selectable() bool { return r.kind != settingInfo }

type SettingsPopup struct {
	active bool
	tab    settingsTab
	cursor int

	// editing is true while a settingText row is being typed into.
	editing bool
	input   textinput.Model

	// confirmReset guards the destructive "reset everything" action.
	confirmReset bool

	// status is the transient line at the bottom: a save error, or a
	// confirmation that the file was written.
	status   string
	statusOK bool

	width  int
	height int
}

func NewSettingsPopup() SettingsPopup {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 200
	ti.Width = 48
	ti.PlaceholderStyle = theme.Dim
	return SettingsPopup{input: ti}
}

func (h *SettingsPopup) SetSize(w, height int) { h.width = w; h.height = height }
func (h SettingsPopup) IsActive() bool         { return h.active }

// AcceptsText reports whether a text row is being edited, so keyboard
// layout translation stays out of the way.
func (h SettingsPopup) AcceptsText() bool { return h.active && h.editing }

// Start opens the popup on the first tab.
func (h *SettingsPopup) Start() {
	h.active = true
	h.tab = tabDelete
	h.cursor = 0
	h.editing = false
	h.confirmReset = false
	h.status = ""
	h.input.SetValue("")
	h.input.Blur()
	h.placeCursorAtFirstSelectable()
}

func (h *SettingsPopup) Stop() { h.active = false }

func (h *SettingsPopup) setTab(t settingsTab) {
	if h.tab == t {
		return
	}
	h.tab = t
	h.cursor = 0
	h.editing = false
	h.confirmReset = false
	h.input.Blur()
	h.placeCursorAtFirstSelectable()
}

func (h *SettingsPopup) placeCursorAtFirstSelectable() {
	rows := h.rows()
	for i, r := range rows {
		if r.selectable() {
			h.cursor = i
			return
		}
	}
	h.cursor = 0
}

// rows returns the row set for the current tab.
func (h SettingsPopup) rows() []settingRow {
	c := config.Get()
	def := config.Defaults()

	switch h.tab {
	case tabDelete:
		return []settingRow{
			{
				kind:  settingChoice,
				label: "Delete mode",
				dyn: func(c config.Config) string {
					switch c.Delete.Mode {
					case config.DeleteTrash:
						return "to the macOS Trash; Finder's Put Back keeps working"
					case config.DeletePermanent:
						return "removed at once — nothing to undo"
					}
					return "hidden beside itself, purged after the countdown"
				},
				value: func(c config.Config) string { return string(c.Delete.Mode) },
				step: func(c *config.Config, dir int) {
					modes := []string{string(config.DeleteTimer), string(config.DeleteTrash), string(config.DeletePermanent)}
					c.Delete.Mode = config.DeleteMode(cycle(string(c.Delete.Mode), modes, dir))
				},
				reset: func(c *config.Config) { c.Delete.Mode = def.Delete.Mode },
			},
			{
				kind:  settingNumber,
				label: "Purge countdown",
				desc:  "how long u can still bring the file back (3–600s)",
				value: func(c config.Config) string { return fmt.Sprintf("%ds", c.Delete.TimerSeconds) },
				step: func(c *config.Config, dir int) {
					c.Delete.TimerSeconds += dir * 5
				},
				reset: func(c *config.Config) { c.Delete.TimerSeconds = def.Delete.TimerSeconds },
				muted: c.Delete.Mode != config.DeleteTimer,
			},
			{
				kind:  settingToggle,
				label: "Confirm before delete",
				desc:  "ask y/N before deleting",
				value: func(c config.Config) string { return onOff(c.Delete.Confirm) },
				step:  func(c *config.Config, _ int) { c.Delete.Confirm = !c.Delete.Confirm },
				reset: func(c *config.Config) { c.Delete.Confirm = def.Delete.Confirm },
			},
			{kind: settingInfo, label: "", desc: ""},
			{
				kind:  settingInfo,
				label: "Undoing a trash delete needs Full Disk Access for your terminal.",
			},
			{
				kind:  settingInfo,
				label: "Without it macOS refuses, and tuiple opens Finder for Put Back.",
			},
		}

	case tabFiles:
		return []settingRow{
			{
				kind:  settingToggle,
				label: "Show hidden files",
				desc:  "show dotfiles (. toggles per session)",
				value: func(c config.Config) string { return onOff(c.Files.ShowHidden) },
				step:  func(c *config.Config, _ int) { c.Files.ShowHidden = !c.Files.ShowHidden },
				reset: func(c *config.Config) { c.Files.ShowHidden = def.Files.ShowHidden },
			},
			{
				kind:  settingChoice,
				label: "Sort by",
				desc:  "starting order (o n / o s / o d per session)",
				value: func(c config.Config) string { return c.Files.Sort },
				step: func(c *config.Config, dir int) {
					c.Files.Sort = cycle(c.Files.Sort, []string{"name", "size", "date", "type"}, dir)
				},
				reset: func(c *config.Config) { c.Files.Sort = def.Files.Sort },
			},
			{
				kind:  settingToggle,
				label: "Reverse order",
				desc:  "Z to A, smallest first, oldest first",
				value: func(c config.Config) string { return onOff(c.Files.Reverse) },
				step:  func(c *config.Config, _ int) { c.Files.Reverse = !c.Files.Reverse },
				reset: func(c *config.Config) { c.Files.Reverse = def.Files.Reverse },
			},
			{
				kind:  settingToggle,
				label: "Directories first",
				desc:  "folders above files whatever the sort",
				value: func(c config.Config) string { return onOff(c.Files.DirsFirst) },
				step:  func(c *config.Config, _ int) { c.Files.DirsFirst = !c.Files.DirsFirst },
				reset: func(c *config.Config) { c.Files.DirsFirst = def.Files.DirsFirst },
			},
			{
				kind:  settingChoice,
				label: "Modified column",
				desc:  "Jan 02 15:04  ·  2026-01-02 15:04  ·  5m ago",
				value: func(c config.Config) string { return c.Files.TimeFormat },
				step: func(c *config.Config, dir int) {
					c.Files.TimeFormat = cycle(c.Files.TimeFormat, []string{"short", "iso", "relative"}, dir)
				},
				reset: func(c *config.Config) { c.Files.TimeFormat = def.Files.TimeFormat },
			},
			{
				kind:  settingChoice,
				label: "Size units",
				desc:  "KiB of 1024 bytes, or kB of 1000",
				value: func(c config.Config) string {
					if c.Files.BinaryUnits {
						return "binary"
					}
					return "decimal"
				},
				step:  func(c *config.Config, _ int) { c.Files.BinaryUnits = !c.Files.BinaryUnits },
				reset: func(c *config.Config) { c.Files.BinaryUnits = def.Files.BinaryUnits },
			},
			{
				kind:  settingChoice,
				label: "Paste onto a taken name",
				desc:  "rename adds _1, _2  ·  skip keeps the old file  ·  overwrite replaces it",
				value: func(c config.Config) string { return string(c.Files.PasteConflict) },
				step: func(c *config.Config, dir int) {
					c.Files.PasteConflict = config.PasteConflict(cycle(string(c.Files.PasteConflict),
						[]string{string(config.PasteRename), string(config.PasteSkip), string(config.PasteOverwrite)}, dir))
				},
				reset: func(c *config.Config) { c.Files.PasteConflict = def.Files.PasteConflict },
			},
			{
				kind:  settingChoice,
				label: "Open in",
				desc:  "where tuiple opens when given no path",
				value: func(c config.Config) string { return string(c.Files.StartDir) },
				step: func(c *config.Config, dir int) {
					c.Files.StartDir = config.StartDir(cycle(string(c.Files.StartDir),
						[]string{string(config.StartInHome), string(config.StartInCwd)}, dir))
				},
				reset: func(c *config.Config) { c.Files.StartDir = def.Files.StartDir },
			},
			{
				kind:  settingToggle,
				label: "File icons",
				desc:  "the symbol in front of every row",
				value: func(c config.Config) string { return onOff(c.Files.Icons) },
				step:  func(c *config.Config, _ int) { c.Files.Icons = !c.Files.Icons },
				reset: func(c *config.Config) { c.Files.Icons = def.Files.Icons },
			},
		}

	case tabPreview:
		return []settingRow{
			{
				kind:  settingToggle,
				label: "Media previews",
				desc:  "images, video thumbnails, PDF covers",
				value: func(c config.Config) string { return onOff(c.Preview.Media) },
				step:  func(c *config.Config, _ int) { c.Preview.Media = !c.Preview.Media },
				reset: func(c *config.Config) { c.Preview.Media = def.Preview.Media },
			},
			{
				kind:  settingToggle,
				label: "Line numbers",
				desc:  "the gutter in front of text",
				value: func(c config.Config) string { return onOff(c.Preview.LineNumbers) },
				step:  func(c *config.Config, _ int) { c.Preview.LineNumbers = !c.Preview.LineNumbers },
				reset: func(c *config.Config) { c.Preview.LineNumbers = def.Preview.LineNumbers },
			},
			{
				kind:  settingToggle,
				label: "Hex dump for binaries",
				desc:  "binaries as hex, or just a note",
				value: func(c config.Config) string { return onOff(c.Preview.HexDump) },
				step:  func(c *config.Config, _ int) { c.Preview.HexDump = !c.Preview.HexDump },
				reset: func(c *config.Config) { c.Preview.HexDump = def.Preview.HexDump },
			},
			{
				kind:  settingToggle,
				label: "Directory sizes",
				desc:  "total up previewed directories (off on slow trees)",
				value: func(c config.Config) string { return onOff(c.Preview.DirSizes) },
				step:  func(c *config.Config, _ int) { c.Preview.DirSizes = !c.Preview.DirSizes },
				reset: func(c *config.Config) { c.Preview.DirSizes = def.Preview.DirSizes },
			},
			{
				kind:  settingNumber,
				label: "Max text preview",
				desc:  "bigger text files are not previewed",
				value: func(c config.Config) string { return fmt.Sprintf("%d KB", c.Preview.MaxTextKB) },
				step: func(c *config.Config, dir int) {
					c.Preview.MaxTextKB = scaleBy(c.Preview.MaxTextKB, dir, 2)
				},
				reset: func(c *config.Config) { c.Preview.MaxTextKB = def.Preview.MaxTextKB },
			},
			{
				kind:  settingNumber,
				label: "Max media preview",
				desc:  "bigger media files are skipped",
				value: func(c config.Config) string { return fmt.Sprintf("%d MB", c.Preview.MaxMediaMB) },
				step: func(c *config.Config, dir int) {
					c.Preview.MaxMediaMB = scaleBy(c.Preview.MaxMediaMB, dir, 2)
				},
				reset: func(c *config.Config) { c.Preview.MaxMediaMB = def.Preview.MaxMediaMB },
				muted: !c.Preview.Media,
			},
		}

	case tabLayout:
		return []settingRow{
			{
				kind:  settingToggle,
				label: "Sidebar",
				desc:  "the favourites column on the left",
				value: func(c config.Config) string { return onOff(c.UI.Sidebar) },
				step:  func(c *config.Config, _ int) { c.UI.Sidebar = !c.UI.Sidebar },
				reset: func(c *config.Config) { c.UI.Sidebar = def.UI.Sidebar },
			},
			{
				kind:  settingNumber,
				label: "Sidebar width",
				desc:  "width of the sidebar (10–40%)",
				value: func(c config.Config) string { return fmt.Sprintf("%d%%", c.UI.SidebarWidth) },
				step:  func(c *config.Config, dir int) { c.UI.SidebarWidth += dir * 5 },
				reset: func(c *config.Config) { c.UI.SidebarWidth = def.UI.SidebarWidth },
				muted: !c.UI.Sidebar,
			},
			{
				kind:  settingToggle,
				label: "Preview panel",
				desc:  "the preview column on the right",
				value: func(c config.Config) string { return onOff(c.UI.Preview) },
				step:  func(c *config.Config, _ int) { c.UI.Preview = !c.UI.Preview },
				reset: func(c *config.Config) { c.UI.Preview = def.UI.Preview },
			},
			{
				kind:  settingNumber,
				label: "Preview width",
				desc:  "width of the preview (15–60%)",
				value: func(c config.Config) string { return fmt.Sprintf("%d%%", c.UI.PreviewWidth) },
				step:  func(c *config.Config, dir int) { c.UI.PreviewWidth += dir * 5 },
				reset: func(c *config.Config) { c.UI.PreviewWidth = def.UI.PreviewWidth },
				muted: !c.UI.Preview,
			},
			{kind: settingInfo, label: "", desc: ""},
			{kind: settingInfo, label: "Tab skips panels that are switched off."},
		}

	case tabOpening:
		rows := []settingRow{
			{
				kind:   settingText,
				label:  "Editor",
				desc:   "empty falls back to $EDITOR, then vi",
				value:  func(c config.Config) string { return orEnv(c.Open.Editor, c.Editor()) },
				text:   func(c config.Config) string { return c.Open.Editor },
				commit: func(c *config.Config, v string) { c.Open.Editor = strings.TrimSpace(v) },
				reset:  func(c *config.Config) { c.Open.Editor = def.Open.Editor },
			},
			{
				kind:   settingText,
				label:  "Shell",
				desc:   "launched by S; empty falls back to $SHELL",
				value:  func(c config.Config) string { return orEnv(c.Open.Shell, c.Shell()) },
				text:   func(c config.Config) string { return c.Open.Shell },
				commit: func(c *config.Config, v string) { c.Open.Shell = strings.TrimSpace(v) },
				reset:  func(c *config.Config) { c.Open.Shell = def.Open.Shell },
			},
			handlerRow(config.HandlerImage, "Images", "chafa, full quality, any key returns"),
			handlerRow(config.HandlerVideo, "Video", "chafa on a thumbnail frame"),
			handlerRow(config.HandlerPDF, "PDF", "bookokrat, then pdftotext piped to less"),
			handlerRow(config.HandlerEbook, "E-books", "bookokrat — epub, djvu, mobi, fb2"),
			handlerRow(config.HandlerAudio, "Audio", "the built-in player in the preview panel"),
			handlerRow(config.HandlerArchive, "Archives", "the editor; Z extracts them instead"),
			handlerRow(config.HandlerDefault, "Everything else", "the editor above"),
			{kind: settingInfo, label: "", desc: ""},
			{
				kind:   settingAction,
				label:  "Edit config file",
				desc:   "edit config.json directly — that is where open-with rules live",
				value:  func(config.Config) string { return "Enter" },
				action: (*SettingsPopup).editConfigFile,
			},
			{kind: settingInfo, label: "", desc: ""},
			{kind: settingInfo, label: "Open-with rules (first match wins, then the built-in handlers):"},
		}
		if len(c.Open.Rules) == 0 {
			rows = append(rows,
				settingRow{kind: settingInfo, label: "  none — chafa, bookokrat, pdftotext and the editor handle everything"},
				settingRow{kind: settingInfo, label: "  add: {\"extensions\": [\".png\"], \"command\": \"open -a Preview {}\", \"terminal\": false}"},
			)
		} else {
			for _, r := range c.Open.Rules {
				label := fmt.Sprintf("  %s → %s", strings.Join(r.Extensions, " "), r.Command)
				if r.Terminal {
					label += "   [terminal]"
				}
				rows = append(rows, settingRow{kind: settingInfo, label: label})
			}
		}
		return rows

	default: // tabSystem
		return []settingRow{
			{
				kind:  settingToggle,
				label: "Git integration",
				desc:  "markers, branch, Ctrl+G, b — off stops background git calls",
				value: func(c config.Config) string { return onOff(c.Git.Enabled) },
				step:  func(c *config.Config, _ int) { c.Git.Enabled = !c.Git.Enabled },
				reset: func(c *config.Config) { c.Git.Enabled = def.Git.Enabled },
			},
			{
				kind:  settingNumber,
				label: "Git refresh interval",
				desc:  "how often git status is re-probed (1–60s)",
				value: func(c config.Config) string { return fmt.Sprintf("%ds", c.Git.PollSeconds) },
				step:  func(c *config.Config, dir int) { c.Git.PollSeconds += dir },
				reset: func(c *config.Config) { c.Git.PollSeconds = def.Git.PollSeconds },
			},
			{
				kind:  settingToggle,
				label: "Cyrillic layout keys",
				desc:  "ЙЦУКЕН keys act as the QWERTY key in the same place",
				value: func(c config.Config) string { return onOff(c.Keys.CyrillicLayout) },
				step:  func(c *config.Config, _ int) { c.Keys.CyrillicLayout = !c.Keys.CyrillicLayout },
				reset: func(c *config.Config) { c.Keys.CyrillicLayout = def.Keys.CyrillicLayout },
			},
			{
				kind:   settingAction,
				label:  "Reset all settings",
				desc:   "every value on every tab back to the defaults",
				value:  func(config.Config) string { return "Enter" },
				action: (*SettingsPopup).askResetAll,
			},
			{kind: settingInfo, label: "", desc: ""},
			{kind: settingInfo, label: "Settings file: " + config.Path()},
			{kind: settingInfo, label: "Rebind keys there under keys.bindings, e.g. {\"delete\": [\"d\", \"x\"]};"},
			{kind: settingInfo, label: "action names are the ones ? shows, in lower case with dashes."},
		}
	}
}

// handlerRow builds the row for one file kind. An empty value means the
// built-in behaviour, which the row spells out so the screen answers
// "what opens my PDFs right now?" without the user having to guess.
func handlerRow(kind, label, builtin string) settingRow {
	return settingRow{
		kind:  settingText,
		label: label,
		desc:  "{} is the path  ·  empty restores the built-in: " + builtin,
		value: func(c config.Config) string {
			cmd := c.Handler(kind)
			if cmd == "" {
				return "(" + builtin + ")"
			}
			if config.HandlerIsGUI(cmd) {
				return cmd + "   [background]"
			}
			return cmd
		},
		text:   func(c config.Config) string { return c.Handler(kind) },
		commit: func(c *config.Config, v string) { c.SetHandler(kind, v) },
		reset:  func(c *config.Config) { c.SetHandler(kind, "") },
	}
}

// ── Key handling ───────────────────────────────────────────────────────

func (h SettingsPopup) Update(msg tea.Msg) (SettingsPopup, tea.Cmd) {
	if !h.active {
		return h, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return h, nil
	}

	// Text editing swallows everything except Enter (commit) and Esc.
	if h.editing {
		switch keyMsg.String() {
		case "esc":
			h.editing = false
			h.input.Blur()
			return h, nil
		case "enter":
			row := h.currentRow()
			val := h.input.Value()
			h.editing = false
			h.input.Blur()
			if row.commit != nil {
				h.applyChange(func(c *config.Config) { row.commit(c, val) })
				return h, changedCmd()
			}
			return h, nil
		}
		var cmd tea.Cmd
		h.input, cmd = h.input.Update(msg)
		return h, cmd
	}

	// The reset-everything confirmation is a plain y/N, same as the
	// delete and discard prompts in the main view.
	if h.confirmReset {
		switch keyMsg.String() {
		case "y", "Y":
			h.confirmReset = false
			if err := config.Reset(); err != nil {
				h.setStatus("could not save: "+err.Error(), false)
			} else {
				h.setStatus("all settings restored to defaults", true)
			}
			return h, changedCmd()
		default:
			h.confirmReset = false
			h.setStatus("", true)
			return h, nil
		}
	}

	switch keyMsg.String() {
	case "esc", "ctrl+c":
		h.Stop()
		return h, nil
	case "tab":
		h.setTab(settingsTab((int(h.tab) + 1) % len(settingsTabTitles)))
		return h, nil
	case "shift+tab":
		h.setTab(settingsTab((int(h.tab) + len(settingsTabTitles) - 1) % len(settingsTabTitles)))
		return h, nil
	case "1", "2", "3", "4", "5", "6":
		h.setTab(settingsTab(int(keyMsg.String()[0] - '1')))
		return h, nil
	case "up", "k":
		h.moveCursor(-1)
		return h, nil
	case "down", "j":
		h.moveCursor(1)
		return h, nil
	case "left", "h":
		return h, h.stepCurrent(-1)
	case "right", "l":
		return h, h.stepCurrent(1)
	case "enter", " ":
		row := h.currentRow()
		switch row.kind {
		case settingText:
			h.beginEdit()
			return h, h.input.Focus()
		case settingAction:
			if row.action != nil {
				return h, row.action(&h)
			}
			return h, nil
		default:
			return h, h.stepCurrent(1)
		}
	case "e":
		if h.currentRow().kind == settingText {
			h.beginEdit()
			return h, h.input.Focus()
		}
		return h, nil
	case "r":
		row := h.currentRow()
		if row.reset != nil {
			h.applyChange(row.reset)
			return h, changedCmd()
		}
		return h, nil
	}
	return h, nil
}

func (h SettingsPopup) currentRow() settingRow {
	rows := h.rows()
	if h.cursor < 0 || h.cursor >= len(rows) {
		return settingRow{kind: settingInfo}
	}
	return rows[h.cursor]
}

// moveCursor walks to the next selectable row in the given direction,
// skipping the info lines entirely.
func (h *SettingsPopup) moveCursor(dir int) {
	rows := h.rows()
	i := h.cursor
	for {
		i += dir
		if i < 0 || i >= len(rows) {
			return
		}
		if rows[i].selectable() {
			h.cursor = i
			return
		}
	}
}

func (h *SettingsPopup) beginEdit() {
	row := h.currentRow()
	if row.text == nil {
		return
	}
	h.editing = true
	h.input.SetValue(row.text(config.Get()))
	h.input.Placeholder = "empty restores the built-in; {} stands for the file path"
	h.input.CursorEnd()
}

func (h *SettingsPopup) stepCurrent(dir int) tea.Cmd {
	row := h.currentRow()
	if row.step == nil {
		return nil
	}
	h.applyChange(func(c *config.Config) { row.step(c, dir) })
	return changedCmd()
}

// applyChange mutates the config and reports the outcome in the status
// line. Persisting on every keystroke is cheap (the file is a few
// hundred bytes) and means a crash can never lose a setting.
func (h *SettingsPopup) applyChange(fn func(*config.Config)) {
	if err := config.Update(fn); err != nil {
		h.setStatus("could not save: "+err.Error(), false)
		return
	}
	h.setStatus("saved to "+config.Path(), true)
}

func (h *SettingsPopup) setStatus(s string, ok bool) {
	h.status = s
	h.statusOK = ok
}

func (h *SettingsPopup) askResetAll() tea.Cmd {
	h.confirmReset = true
	return nil
}

// editConfigFile opens the settings file in the configured editor. The
// file is written first so the editor never opens on a missing path,
// and reloaded afterwards so hand-edits take effect immediately.
func (h *SettingsPopup) editConfigFile() tea.Cmd {
	if err := config.Update(func(*config.Config) {}); err != nil {
		h.setStatus("could not write config: "+err.Error(), false)
		return nil
	}
	cmd := exec.Command(config.Get().Editor(), config.Path())
	return tea.ExecProcess(cmd, func(error) tea.Msg { return settingsReloadedMsg{} })
}

func changedCmd() tea.Cmd {
	return func() tea.Msg { return settingsChangedMsg{} }
}

// ── Rendering ──────────────────────────────────────────────────────────

func (h SettingsPopup) View() string {
	if !h.active || h.width <= 0 || h.height <= 0 {
		return ""
	}

	// Same container conventions as the history popup: border only, no
	// padding, every line padded to exactly w cells by hand.
	w := h.width - 2
	contentH := h.height - 2

	var lines []string
	lines = append(lines, padLine(h.renderTitleBar(), w))
	lines = append(lines, padLine(theme.Dim.Render(
		"   up/down move  ·  left/right change  ·  e edit  ·  r reset  ·  Esc close"), w))
	lines = append(lines, padLine("", w))

	const footerLines = 3 // description (up to two lines) + status
	listH := contentH - len(lines) - footerLines
	if listH < 1 {
		listH = 1
	}

	rows := h.rows()
	c := config.Get()
	rendered := make([]string, 0, len(rows))
	for i, r := range rows {
		rendered = append(rendered, h.renderRow(r, c, i == h.cursor, w))
	}
	if len(rendered) > listH {
		rendered = rendered[:listH]
	}
	lines = append(lines, rendered...)

	blank := padLine("", w)
	for len(lines) < contentH-footerLines {
		lines = append(lines, blank)
	}
	if len(lines) > contentH-footerLines {
		lines = lines[:contentH-footerLines]
	}

	for _, descLine := range h.renderDescLines(w) {
		lines = append(lines, padLine(descLine, w))
	}
	lines = append(lines, padLine(h.renderStatusLine(), w))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.AccentCyan).
		Render(strings.Join(lines, "\n"))
}

func (h SettingsPopup) renderTitleBar() string {
	title := lipgloss.NewStyle().
		Foreground(theme.AccentCyan).Bold(true).Render(" Settings")

	activeStyle := lipgloss.NewStyle().Foreground(theme.AccentGreen).Bold(true).Underline(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(theme.FgDimColor)
	dimDot := theme.Dim.Render("·")

	var parts []string
	for i, t := range settingsTabTitles {
		label := " " + t + " "
		if settingsTab(i) == h.tab {
			parts = append(parts, activeStyle.Render(label))
		} else {
			parts = append(parts, inactiveStyle.Render(label))
		}
	}
	return title + "    " + strings.Join(parts, dimDot)
}

// renderRow lays a setting out in two columns: label on the left, the
// value in a fixed slot on the right. The same ASCII-only, cell-exact
// approach as the history popup — no ambiguous-width glyphs, every line
// clamped to w so the border never wraps.
func (h SettingsPopup) renderRow(r settingRow, c config.Config, cursor bool, w int) string {
	const (
		prefixW = 2
		labelW  = 30
		gap     = 2
	)

	applyBg := func(s lipgloss.Style) lipgloss.Style {
		if cursor {
			return s.Background(theme.BgSelected)
		}
		return s
	}

	if r.kind == settingInfo {
		return clampRowWidth(theme.Dim.Render("  "+truncateName(r.label, w-2)), w, false)
	}

	var prefix string
	if cursor {
		prefix = applyBg(lipgloss.NewStyle().Foreground(theme.AccentYellow).Bold(true)).Render("> ")
	} else {
		prefix = applyBg(lipgloss.NewStyle()).Render("  ")
	}

	labelStyle := lipgloss.NewStyle().Foreground(theme.FgColor)
	if r.muted {
		labelStyle = lipgloss.NewStyle().Foreground(theme.FgMutedColor)
	}
	if cursor {
		labelStyle = labelStyle.Bold(true)
	}
	label := applyBg(labelStyle.Width(labelW).MaxWidth(labelW)).
		Render(truncateName(r.label, labelW))

	valueW := w - prefixW - labelW - gap
	if valueW < 8 {
		valueW = 8
	}
	value := applyBg(lipgloss.NewStyle().Foreground(h.valueColor(r)).Bold(true).
		Width(valueW).MaxWidth(valueW)).
		Render(truncateName(h.renderValue(r, c), valueW))

	line := prefix + label + applyBg(lipgloss.NewStyle()).Render(strings.Repeat(" ", gap)) + value
	return clampRowWidth(line, w, cursor)
}

// renderValue decorates the raw value with affordances: angle brackets
// for anything the arrow keys can step, a checkbox for toggles.
func (h SettingsPopup) renderValue(r settingRow, c config.Config) string {
	if r.value == nil {
		return ""
	}
	v := r.value(c)
	switch r.kind {
	case settingToggle:
		box := "[ ]"
		if v == "on" {
			box = "[x]"
		}
		return box + " " + v
	case settingChoice, settingNumber:
		return "< " + v + " >"
	case settingText:
		return v
	}
	return v
}

func (h SettingsPopup) valueColor(r settingRow) lipgloss.TerminalColor {
	if r.muted {
		return theme.FgMutedColor
	}
	switch r.kind {
	case settingAction:
		return theme.AccentMagenta
	case settingText:
		return theme.AccentYellow
	}
	return theme.AccentCyan
}

// renderDescLines explains the focused row in at most two lines. Wrapping
// rather than truncating matters here: these sentences are the only
// place that says what a setting actually does, and a narrow terminal
// used to cut them off mid-word.
func (h SettingsPopup) renderDescLines(w int) []string {
	row := h.currentRow()
	desc := row.desc
	if row.dyn != nil {
		desc = row.dyn(config.Get())
	}
	if desc == "" {
		return []string{"", ""}
	}

	out := []string{"", ""}
	for i, line := range wrapWords(desc, w-2, 2) {
		out[i] = theme.Dim.Render(" " + line)
	}
	return out
}

// wrapWords breaks text on word boundaries into at most maxLines lines of
// width w, marking the end with "..." if it still does not fit.
func wrapWords(text string, w, maxLines int) []string {
	if w < 8 {
		w = 8
	}
	words := strings.Fields(text)
	var lines []string
	current := ""
	for _, word := range words {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if lipgloss.Width(candidate) <= w {
			current = candidate
			continue
		}
		lines = append(lines, current)
		current = word
		if len(lines) == maxLines {
			break
		}
	}
	if len(lines) < maxLines && current != "" {
		lines = append(lines, current)
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	// A last line that still overflows (one very long word) gets cut.
	for i := range lines {
		lines[i] = truncateName(lines[i], w)
	}
	return lines
}

func (h SettingsPopup) renderStatusLine() string {
	if h.editing {
		row := h.currentRow()
		label := lipgloss.NewStyle().
			Foreground(theme.AccentYellow).Bold(true).Render(" " + row.label + ": ")
		return label + h.input.View() +
			theme.Dim.Render("    Enter to apply · Esc to cancel")
	}
	if h.confirmReset {
		return lipgloss.NewStyle().Foreground(theme.AccentRed).Bold(true).
			Render(" Reset every setting to its default? (y/N)")
	}
	if h.status == "" {
		return ""
	}
	style := theme.Dim
	if !h.statusOK {
		style = lipgloss.NewStyle().Foreground(theme.AccentRed)
	}
	return style.Render(" " + h.status)
}

// ── Small helpers ──────────────────────────────────────────────────────

// cycle returns the neighbour of cur in opts, wrapping at both ends.
func cycle(cur string, opts []string, dir int) string {
	if len(opts) == 0 {
		return cur
	}
	idx := 0
	for i, o := range opts {
		if o == cur {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(opts)) % len(opts)
	return opts[idx]
}

// scaleBy doubles or halves v, which suits size limits far better than a
// fixed step: 1 MB → 2 → 4 rather than 1024 nudges of one.
func scaleBy(v, dir, factor int) int {
	if dir > 0 {
		return v * factor
	}
	return v / factor
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// orEnv shows what an empty setting resolves to, so "Editor: (nvim)"
// tells the user what will actually launch.
func orEnv(raw, resolved string) string {
	if strings.TrimSpace(raw) == "" {
		return "(" + resolved + ")"
	}
	return raw
}
