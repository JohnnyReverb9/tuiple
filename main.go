package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"tuiple/app"
)

// Build-time metadata injected via Makefile's -ldflags. Default values
// kick in when the binary is built with a plain `go build`.
var (
	version = "dev"
	commit  = "unknown"
	built   = "unknown"
)

func main() {
	if os.Getenv("TUIPLE_ACTIVE") == "1" {
		fmt.Fprintf(os.Stderr, "Error: Tuiple is already running in this terminal session.\n")
		os.Exit(1)
	}
	os.Setenv("TUIPLE_ACTIVE", "1")

	startDir, err := os.UserHomeDir()
	if err != nil {
		startDir, _ = os.Getwd()
	}
	if startDir == "" {
		startDir = "/"
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Printf("tuiple %s\n  commit: %s\n  built:  %s\n", version, commit, built)
			return
		case "--help", "-h":
			fmt.Println("tuiple — a 3-panel TUI file manager with git integration")
			fmt.Println()
			fmt.Println("Usage:")
			fmt.Println("  tuiple [path]      open at path (defaults to $HOME)")
			fmt.Println("  tuiple --version   print version metadata")
			fmt.Println("  tuiple --help      show this message")
			fmt.Println()
			fmt.Println("Press ? inside the app for keyboard shortcuts.")
			return
		default:
			startDir = os.Args[1]
		}
	}

	p := tea.NewProgram(
		app.New(startDir),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
