package app

import (
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// runExternal hands the terminal to cmd — the editor, the shell, a
// reader such as bookokrat — and takes it back when cmd exits.
//
// Bubble Tea restores the alternate screen and bracketed paste after an
// exec, but not mouse reporting, and full-screen programs (nvim among
// them) switch mouse reporting off when they quit. Without switching it
// back on here the mouse would stay dead until tuiple restarts.
func runExternal(cmd *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	return tea.Sequence(tea.ExecProcess(cmd, fn), tea.EnableMouseCellMotion)
}
