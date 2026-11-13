package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/JohnnyReverb9/tuiple/config"
)

// TestMain points the settings file — and with it the action log, the
// recent list and the saved session, which all live beside it — at a
// throwaway directory for the whole package. No test here may touch the
// state of whoever runs it.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tuiple-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot create a temp dir:", err)
		os.Exit(1)
	}
	os.Setenv("TUIPLE_CONFIG", filepath.Join(dir, "config.json"))
	_ = config.Load()

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
