package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"tuiple/app"
)

func main() {
	startDir, err := os.Getwd()
	if err != nil {
		startDir = "/"
	}
	if len(os.Args) > 1 {
		startDir = os.Args[1]
	}

	p := tea.NewProgram(
		app.New(startDir),
		tea.WithAltScreen(),
		tea.WithMouseAllMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
