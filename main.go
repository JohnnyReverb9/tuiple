package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"tuiple/app"
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
		startDir = os.Args[1]
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
