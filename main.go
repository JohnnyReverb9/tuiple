package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"tuiple/app"
	"tuiple/config"
)

// Build-time metadata injected via Makefile's -ldflags. Default values
// kick in when the binary is built with a plain `go build`.
var (
	version = "dev"
	commit  = "unknown"
	built   = "unknown"
)

// options is the parsed command line.
type options struct {
	startDir string
	cwdFile  string // written on exit so a shell wrapper can cd there
	exit     bool   // a flag printed its output; nothing left to run
}

func main() {
	if os.Getenv("TUIPLE_ACTIVE") == "1" {
		fmt.Fprintf(os.Stderr, "Error: Tuiple is already running in this terminal session.\n")
		os.Exit(1)
	}
	os.Setenv("TUIPLE_ACTIVE", "1")

	// Settings are optional: a missing file leaves the shipped defaults
	// in place, and a broken one is reported without stopping startup —
	// a typo in config.json should never lock the user out of their
	// file manager.
	if err := config.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not read %s: %v\n", config.Path(), err)
	}
	// The keymap and the list's sorting and formatting live in packages
	// that cache them; push the freshly loaded settings in before the
	// first directory is read.
	app.ApplyGlobalConfig(config.Get())

	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}
	if opts.exit {
		return
	}

	model := app.New(opts.startDir).WithExitCwdFile(opts.cwdFile)
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (options, error) {
	opts := options{startDir: defaultStartDir()}

	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "--version", "-v":
			fmt.Printf("tuiple %s\n  commit: %s\n  built:  %s\n", version, commit, built)
			opts.exit = true
			return opts, nil
		case "--help", "-h":
			printUsage()
			opts.exit = true
			return opts, nil
		case "--shell-init":
			shell := "zsh"
			if i+1 < len(args) {
				shell = args[i+1]
			}
			out, err := shellInit(shell)
			if err != nil {
				return opts, err
			}
			fmt.Print(out)
			opts.exit = true
			return opts, nil
		case "--cwd-file":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--cwd-file needs a path")
			}
			opts.cwdFile = args[i+1]
			i++
		default:
			if len(arg) > 0 && arg[0] == '-' {
				return opts, fmt.Errorf("unknown flag %q (try --help)", arg)
			}
			opts.startDir = arg
		}
	}
	return opts, nil
}

// defaultStartDir honours the files.start_dir setting: the home
// directory (the long-standing default) or wherever the shell was.
func defaultStartDir() string {
	if config.Get().Files.StartDir == config.StartInCwd {
		if cwd, err := os.Getwd(); err == nil && cwd != "" {
			return cwd
		}
	}
	dir, err := os.UserHomeDir()
	if err != nil || dir == "" {
		if cwd, err := os.Getwd(); err == nil && cwd != "" {
			return cwd
		}
		return "/"
	}
	return dir
}

func printUsage() {
	fmt.Println("tuiple — a 3-panel TUI file manager with git integration")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  tuiple [path]           open at path")
	fmt.Println("  tuiple --cwd-file PATH  write the directory you exit in to PATH")
	fmt.Println("  tuiple --shell-init     print a shell function that cds on exit")
	fmt.Println("  tuiple --version        print version metadata")
	fmt.Println("  tuiple --help           show this message")
	fmt.Println()
	fmt.Println("Press ? inside the app for keyboard shortcuts,")
	fmt.Println("and , for settings (stored in " + config.Path() + ").")
}

// shellInit prints a wrapper function that starts tuiple and changes the
// shell's directory to wherever you left off. Without it the terminal
// stays where it was — a child process cannot move its parent.
func shellInit(shell string) (string, error) {
	const posix = `# tuiple: cd to the directory you exit in.
# Add to your shell rc:  eval "$(tuiple --shell-init %s)"
tp() {
	local cwd_file
	cwd_file="$(mktemp -t tuiple-cwd)"
	command tuiple --cwd-file "$cwd_file" "$@"
	local dir
	dir="$(cat -- "$cwd_file" 2>/dev/null)"
	rm -f -- "$cwd_file"
	if [ -n "$dir" ] && [ "$dir" != "$PWD" ]; then
		cd -- "$dir" || return
	fi
}
`
	const fish = `# tuiple: cd to the directory you exit in.
# Add to your config.fish:  tuiple --shell-init fish | source
function tp
	set -l cwd_file (mktemp -t tuiple-cwd)
	command tuiple --cwd-file $cwd_file $argv
	set -l dir (cat $cwd_file 2>/dev/null)
	rm -f $cwd_file
	if test -n "$dir" -a "$dir" != "$PWD"
		cd $dir
	end
end
`
	switch shell {
	case "zsh", "bash", "sh", "":
		return fmt.Sprintf(posix, shell), nil
	case "fish":
		return fish, nil
	}
	return "", fmt.Errorf("unsupported shell %q (zsh, bash, fish)", shell)
}
