package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"tuiple/components/filelist"
	"tuiple/config"
	"tuiple/filesystem"
)

// Bulk rename: the list of names is handed to $EDITOR as a plain text
// file, one per line, and whatever comes back is applied. It is the
// vidir workflow — search & replace, macros, column edits, anything the
// editor can do — and it lands on the undo stack as a single event, so
// one press of u puts every name back.

// bulkRenameDoneMsg carries the outcome of an editor session back into
// the app. The editor runs through tea.ExecProcess, so the work has to
// finish in the callback rather than inline.
type bulkRenameDoneMsg struct {
	dir      string
	original []string
	editPath string
}

// renameStep is one source → destination pair.
type renameStep struct {
	src string
	dst string
}

// startBulkRename writes the current names to a temp file and opens it
// in the editor. Nothing is touched on disk until the editor exits.
func startBulkRename(dir string, names []string) tea.Cmd {
	editPath := filepath.Join(os.TempDir(),
		fmt.Sprintf("tuiple-rename-%d.txt", time.Now().UnixNano()))
	body := strings.Join(names, "\n") + "\n"
	if err := os.WriteFile(editPath, []byte(body), 0600); err != nil {
		return func() tea.Msg {
			return bulkRenameDoneMsg{dir: dir, original: names, editPath: ""}
		}
	}

	cmd := exec.Command(config.Get().Editor(), editPath)
	cmd.Dir = dir
	return tea.ExecProcess(cmd, func(error) tea.Msg {
		return bulkRenameDoneMsg{dir: dir, original: names, editPath: editPath}
	})
}

// readEditedNames reads the file back, dropping the trailing newline the
// editor is entitled to add or remove.
func readEditedNames(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

// buildRenamePlan turns the edited list into the moves to perform.
//
// Everything that could go wrong is caught here, before a single file is
// touched: a changed number of lines (the editor's line order is the only
// thing tying a new name to a file), an emptied name, a path separator
// smuggled into a name, two files renamed onto each other, or a target
// that already exists and is not itself being renamed away.
func buildRenamePlan(dir string, original, edited []string) ([]renameStep, error) {
	if len(edited) != len(original) {
		return nil, fmt.Errorf("expected %d lines, got %d — no names changed",
			len(original), len(edited))
	}

	leaving := make(map[string]bool, len(original)) // names being freed up
	for i, name := range original {
		if strings.TrimSpace(edited[i]) != name {
			leaving[name] = true
		}
	}

	var steps []renameStep
	targets := make(map[string]string, len(edited))
	for i, raw := range edited {
		name := strings.TrimSpace(raw)
		if name == original[i] {
			continue
		}
		if name == "" {
			return nil, fmt.Errorf("line %d is empty — rename, don't delete", i+1)
		}
		if strings.ContainsRune(name, os.PathSeparator) {
			return nil, fmt.Errorf("%q contains a path separator", name)
		}
		if name == "." || name == ".." {
			return nil, fmt.Errorf("line %d: %q is not a file name", i+1, name)
		}
		if prev, dup := targets[name]; dup {
			return nil, fmt.Errorf("%q and %q would both become %q", prev, original[i], name)
		}
		targets[name] = original[i]

		if !leaving[name] {
			if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
				return nil, fmt.Errorf("%q already exists", name)
			}
		}
		steps = append(steps, renameStep{
			src: filepath.Join(dir, original[i]),
			dst: filepath.Join(dir, name),
		})
	}
	return steps, nil
}

// applyRenamePlan performs the moves in two passes, via temporary names.
// The detour is what makes swaps and rotations (a→b, b→a) work: renaming
// straight through would have the first move clobber the second's source.
func applyRenamePlan(steps []renameStep) ([]HistoryItem, error) {
	if len(steps) == 0 {
		return nil, nil
	}

	stamp := time.Now().UnixNano()
	temps := make([]string, len(steps))

	// Pass one: park everything under a name nothing else can want.
	for i, st := range steps {
		tmp := filepath.Join(filepath.Dir(st.src),
			fmt.Sprintf(".tuiple_rename_%d_%d", stamp, i))
		if err := filesystem.Move(st.src, tmp); err != nil {
			// Undo the parking we have done so far so the directory is
			// left exactly as we found it.
			for j := 0; j < i; j++ {
				_ = filesystem.Move(temps[j], steps[j].src)
			}
			return nil, fmt.Errorf("%s: %w", filepath.Base(st.src), err)
		}
		temps[i] = tmp
	}

	// Pass two: park → final name.
	var items []HistoryItem
	for i, st := range steps {
		info, statErr := os.Lstat(temps[i])
		if err := filesystem.Move(temps[i], st.dst); err != nil {
			// Put back what is still parked, including this one, then
			// report. Names already renamed stay renamed and are on the
			// returned item list, so undo can reach them.
			for j := i; j < len(steps); j++ {
				_ = filesystem.Move(temps[j], steps[j].src)
			}
			return items, fmt.Errorf("%s: %w", filepath.Base(st.dst), err)
		}
		isDir := statErr == nil && info.IsDir()
		items = append(items, HistoryItem{Src: st.src, Dst: st.dst, IsDir: isDir})
	}
	return items, nil
}

// finishBulkRename applies whatever came back from the editor. The whole
// batch becomes a single history event, so one u undoes the lot.
func (m Model) finishBulkRename(msg bulkRenameDoneMsg) (tea.Model, tea.Cmd) {
	if msg.editPath == "" {
		m.statusMsg = "Rename: could not create the edit file"
		return m, nil
	}
	defer os.Remove(msg.editPath)

	edited, err := readEditedNames(msg.editPath)
	if err != nil {
		m.statusMsg = "Rename: " + err.Error()
		return m, nil
	}

	steps, err := buildRenamePlan(msg.dir, msg.original, edited)
	if err != nil {
		m.statusMsg = "Rename: " + err.Error()
		return m, nil
	}
	if len(steps) == 0 {
		m.statusMsg = "Rename: nothing changed"
		return m, nil
	}

	items, applyErr := applyRenamePlan(steps)
	if len(items) > 0 {
		PushHistory(HistoryEvent{Op: OpRename, Items: items})
	}
	if applyErr != nil {
		m.statusMsg = "Rename: " + applyErr.Error()
	} else {
		m.statusMsg = fmt.Sprintf("Renamed %d item(s) — u to undo", len(items))
	}

	m.filelist, _ = m.filelist.Update(filelist.RefreshListMsg{})
	m.filelist, _ = m.filelist.Update(filelist.ClearSelectionMsg{})

	var cmds []tea.Cmd
	if entry := m.filelist.SelectedEntry(); entry != nil {
		cmds = append(cmds, m.preview.LoadFile(*entry))
		m.stampPreview(entry)
	}
	return m, tea.Batch(cmds...)
}

// moveAll performs a batch of moves safely. A single move is a plain
// rename; a batch goes through applyRenamePlan's two passes, because a
// batch can permute names among themselves — undoing a bulk rename that
// swapped two files is exactly that case, and moving straight through
// would have the first rename destroy the second one's source.
func moveAll(steps []renameStep) error {
	if len(steps) == 1 {
		return filesystem.Move(steps[0].src, steps[0].dst)
	}
	_, err := applyRenamePlan(steps)
	return err
}
