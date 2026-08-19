package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Rules are matched on the normalised extension, and the first match
// wins — that ordering is the contract the README documents.
func TestRuleMatching(t *testing.T) {
	c := Defaults()
	c.Open.Rules = []OpenRule{
		{Extensions: []string{"PNG", ".jpg"}, Command: "open -a Preview {}"},
		{Extensions: []string{".png"}, Command: "second"},
		{Extensions: []string{".md"}, Command: ""}, // no command: skipped
	}
	sanitize(&c)

	if got := c.RuleFor(".png"); got == nil || got.Command != "open -a Preview {}" {
		t.Errorf("RuleFor(.png) = %v, want the first rule (extension normalised from PNG)", got)
	}
	if got := c.RuleFor(".md"); got != nil {
		t.Errorf("RuleFor(.md) = %v, want nil for a rule without a command", got)
	}
	if got := c.RuleFor(".txt"); got != nil {
		t.Errorf("RuleFor(.txt) = %v, want nil so the built-in handlers take over", got)
	}
}

// A hand-edited file must never be able to put the app into a state it
// cannot honour: unknown enums fall back, numbers are clamped.
func TestSanitizeClampsHandEdits(t *testing.T) {
	c := Config{
		Delete:  DeleteConfig{Mode: "shred", TimerSeconds: 0},
		Files:   FilesConfig{Sort: "colour"},
		Preview: PreviewConfig{MaxTextKB: 0, MaxMediaMB: 100000},
		Git:     GitConfig{PollSeconds: 0},
	}
	sanitize(&c)

	if c.Delete.Mode != DeleteTimer {
		t.Errorf("Mode = %q, want %q", c.Delete.Mode, DeleteTimer)
	}
	if c.Delete.TimerSeconds != 3 {
		t.Errorf("TimerSeconds = %d, want the lower bound 3", c.Delete.TimerSeconds)
	}
	if c.Files.Sort != "name" {
		t.Errorf("Sort = %q, want %q", c.Files.Sort, "name")
	}
	if c.Preview.MaxTextKB != 4 || c.Preview.MaxMediaMB != 4096 {
		t.Errorf("preview limits = %d KB / %d MB, want them clamped into range",
			c.Preview.MaxTextKB, c.Preview.MaxMediaMB)
	}
	if c.Git.PollSeconds != 1 {
		t.Errorf("PollSeconds = %d, want the lower bound 1", c.Git.PollSeconds)
	}
}

func TestHandlers(t *testing.T) {
	c := Defaults()
	if c.Handler(HandlerPDF) != "" {
		t.Error("a fresh config should leave every kind on its built-in")
	}

	c.SetHandler(HandlerPDF, "  open -a Preview {}  ")
	if got := c.Handler(HandlerPDF); got != "open -a Preview {}" {
		t.Errorf("Handler(pdf) = %q, want the trimmed command", got)
	}
	if !HandlerIsGUI(c.Handler(HandlerPDF)) {
		t.Error("an `open` command should be recognised as a GUI launcher")
	}
	if HandlerIsGUI("glow -p {}") {
		t.Error("a TUI command must not be treated as a GUI launcher")
	}

	c.SetHandler(HandlerPDF, "")
	if _, still := c.Open.Handlers[HandlerPDF]; still {
		t.Error("clearing a handler should remove it, not store an empty string")
	}

	// Every kind the settings screen shows must be addressable.
	for _, kind := range HandlerKinds {
		c.SetHandler(kind, "cmd "+kind)
	}
	if len(c.Open.Handlers) != len(HandlerKinds) {
		t.Errorf("stored %d handlers, want %d", len(c.Open.Handlers), len(HandlerKinds))
	}
}

// useTempConfig points the package at a throwaway file so a test can
// never write over the settings of whoever is running it.
func useTempConfig(t *testing.T) {
	t.Helper()
	t.Setenv("TUIPLE_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := Load(); err != nil {
		t.Fatalf("load temp config: %v", err)
	}
	t.Cleanup(func() {
		t.Setenv("TUIPLE_CONFIG", "")
		_ = Load()
	})
}

func TestTempConfigRoundTrip(t *testing.T) {
	useTempConfig(t)

	if err := Update(func(c *Config) { c.Delete.TimerSeconds = 45 }); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := os.Stat(Path()); err != nil {
		t.Fatalf("settings file was not written: %v", err)
	}
	if err := Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := Get().Delete.TimerSeconds; got != 45 {
		t.Errorf("after reload TimerSeconds = %d, want 45", got)
	}
}

// A snapshot must not share its maps or slices with the live config, or
// an edit in the settings screen would be visible half-applied.
func TestGetSnapshotIsIsolated(t *testing.T) {
	useTempConfig(t)

	if err := Update(func(c *Config) {
		c.SetHandler(HandlerPDF, "one")
		c.Open.Rules = []OpenRule{{Extensions: []string{".md"}, Command: "glow"}}
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	snap := Get()
	snap.Open.Handlers[HandlerPDF] = "two"
	snap.Open.Rules[0].Command = "changed"

	if Get().Handler(HandlerPDF) != "one" {
		t.Error("mutating a snapshot changed the live handler map")
	}
	if Get().Open.Rules[0].Command != "glow" {
		t.Error("mutating a snapshot changed the live rules")
	}
}
