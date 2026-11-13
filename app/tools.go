package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JohnnyReverb9/tuiple/components/filelist"
	"github.com/JohnnyReverb9/tuiple/components/picker"
	"github.com/JohnnyReverb9/tuiple/filesystem"
	"github.com/JohnnyReverb9/tuiple/theme"
)

// ── Get info ───────────────────────────────────────────────────────────

// fileInfoBox renders what Finder's Get Info shows, as a popup. It is
// built once, when asked for; the box is a snapshot, not a live view.
func fileInfoBox(path, gitCode string, maxWidth int) string {
	type row struct{ label, value string }
	var rows []row
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, row{label, value})
		}
	}

	info, err := os.Lstat(path)
	if err != nil {
		add("Error", err.Error())
	} else {
		kind := "File"
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			kind = "Symbolic link"
		case info.IsDir():
			kind = "Folder"
		case info.Mode()&0111 != 0:
			kind = "Executable"
		}
		if ext := filepath.Ext(path); ext != "" && kind == "File" {
			kind = strings.ToUpper(ext[1:]) + " file"
		}
		add("Kind", kind)
		add("Where", filesystem.ShortenPath(filepath.Dir(path)))
		if target, err := os.Readlink(path); err == nil {
			add("Points to", target)
		}
		if info.IsDir() {
			add("Contains", fmt.Sprintf("%d items", filesystem.DirItemCount(path)))
		} else {
			add("Size", fmt.Sprintf("%s (%d bytes)", filesystem.FormatSize(info.Size()), info.Size()))
		}
		add("Modified", info.ModTime().Format("2 Jan 2006, 15:04:05"))
		add("Permissions", fmt.Sprintf("%s (%04o)", info.Mode().String(), info.Mode().Perm()))
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			owner := strconv.Itoa(int(st.Uid))
			if u, err := user.LookupId(owner); err == nil {
				owner = u.Username
			}
			group := strconv.Itoa(int(st.Gid))
			if g, err := user.LookupGroupId(group); err == nil {
				group = g.Name
			}
			add("Owner", owner+" : "+group)
		}
		switch {
		case gitCode == "??":
			add("Git", "untracked")
		case gitCode != "":
			add("Git", "changed ("+strings.TrimSpace(gitCode)+")")
		}
	}

	// A value too long for the window keeps its end: for a path that is
	// the part that says where the thing actually is.
	room := max(maxWidth-18, 12)
	label := lipgloss.NewStyle().Foreground(theme.FgMutedColor).Width(13)
	lines := []string{
		" " + lipgloss.NewStyle().Foreground(theme.FgColor).Bold(true).MaxWidth(room+13).Render(filepath.Base(path)),
		"",
	}
	for _, r := range rows {
		value := []rune(r.value)
		if lipgloss.Width(r.value) > room {
			for len(value) > 1 && lipgloss.Width(string(value)) > room-1 {
				value = value[1:]
			}
			value = append([]rune("…"), value...)
		}
		lines = append(lines, " "+label.Render(r.label)+theme.Normal.Render(string(value)))
	}
	lines = append(lines, "", " "+theme.Muted.Render("any key closes"))

	width := 0
	for _, l := range lines {
		width = max(width, lipgloss.Width(l))
	}
	for i, l := range lines {
		lines[i] = l + strings.Repeat(" ", width-lipgloss.Width(l)+1)
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.BorderActive).
		Render(strings.Join(lines, "\n"))
}

// ── Run ────────────────────────────────────────────────────────────────

var makeTarget = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*)\s*:([^=]|$)`)

// runCommands finds what a project knows how to run, by reading the
// files that say so: Makefile targets, package.json and composer.json
// scripts, and the standard verbs of a Go or Rust module.
func runCommands(root string) []picker.Item {
	var items []picker.Item
	add := func(cmd, from string) {
		items = append(items, picker.Item{ID: cmd, Title: cmd, Detail: from})
	}
	read := func(name string) []byte {
		data, _ := os.ReadFile(filepath.Join(root, name))
		return data
	}
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}

	for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
		data := read(name)
		if data == nil {
			continue
		}
		seen := map[string]bool{}
		for _, line := range strings.Split(string(data), "\n") {
			if m := makeTarget.FindStringSubmatch(line); m != nil && !seen[m[1]] {
				seen[m[1]] = true
				add("make "+m[1], name)
			}
		}
		break
	}

	scripts := func(file string) []string {
		var doc struct {
			Scripts map[string]json.RawMessage `json:"scripts"`
		}
		if json.Unmarshal(read(file), &doc) != nil {
			return nil
		}
		names := make([]string, 0, len(doc.Scripts))
		for name := range doc.Scripts {
			names = append(names, name)
		}
		sort.Strings(names)
		return names
	}
	runner := "npm run"
	switch {
	case exists("pnpm-lock.yaml"):
		runner = "pnpm run"
	case exists("yarn.lock"):
		runner = "yarn run"
	case exists("bun.lockb"), exists("bun.lock"):
		runner = "bun run"
	}
	for _, name := range scripts("package.json") {
		add(runner+" "+name, "package.json")
	}
	for _, name := range scripts("composer.json") {
		add("composer run-script "+name, "composer.json")
	}
	if exists("go.mod") {
		for _, cmd := range []string{"go build ./...", "go test ./...", "go vet ./...", "go run ."} {
			add(cmd, "go.mod")
		}
	}
	if exists("Cargo.toml") {
		for _, cmd := range []string{"cargo build", "cargo test", "cargo run"} {
			add(cmd, "Cargo.toml")
		}
	}
	return items
}

// openRun lists the project's commands in the picker.
func (m *Model) openRun() tea.Cmd {
	root := m.projectRoot()
	items := runCommands(root)
	if len(items) == 0 {
		m.statusMsg = "Nothing to run here — no Makefile, package.json, composer.json, go.mod or Cargo.toml"
		return nil
	}
	return m.picker.Open(picker.Options{
		Kind:        pickRun,
		Title:       "Run",
		Placeholder: "in " + filepath.Base(root) + "…",
		Footer:      "Enter run  ·  Esc close",
		Items:       items,
	})
}

// runInTerminal hands the screen to a command run in dir and waits for
// Enter afterwards, so its output can be read before tuiple redraws.
func runInTerminal(dir, command string) tea.Cmd {
	script := "clear; printf '$ %s\\n\\n' \"$1\"; sh -c \"$1\"; code=$?; " +
		"printf '\\n[exit %s] press Enter to return ' \"$code\"; read -r _"
	cmd := exec.Command("sh", "-c", script, "tuiple-run", command)
	cmd.Dir = dir
	return tea.ExecProcess(cmd, func(error) tea.Msg { return filelist.RefreshListMsg{} })
}
