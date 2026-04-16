package bookmarks

import (
	"encoding/json"
	"os"
	"path/filepath"

	"tuiple/filesystem"
)

var (
	marksPath = filepath.Join(filesystem.HomeDir(), ".config", "tuiple", "bookmarks.json")
	marks     = make(map[rune]string)
)

func init() {
	Load()
}

// EnsureConfigDir creates the configuration directory if it doesn't exist.
func EnsureConfigDir() error {
	dir := filepath.Dir(marksPath)
	return os.MkdirAll(dir, 0755)
}

// Load reads bookmarks from disk.
func Load() {
	data, err := os.ReadFile(marksPath)
	if err != nil {
		return
	}
	var stringMap map[string]string
	if err := json.Unmarshal(data, &stringMap); err == nil {
		for k, v := range stringMap {
			if len(k) > 0 {
				marks[rune(k[0])] = v
			}
		}
	}
}

// Save writes bookmarks to disk.
func Save() error {
	if err := EnsureConfigDir(); err != nil {
		return err
	}
	stringMap := make(map[string]string)
	for k, v := range marks {
		stringMap[string(k)] = v
	}
	data, err := json.MarshalIndent(stringMap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(marksPath, data, 0644)
}

// Set marks a character to a specific path.
func Set(char rune, path string) error {
	marks[char] = path
	return Save()
}

// Get returns the path for a character mark, and ok if found.
func Get(char rune) (string, bool) {
	path, ok := marks[char]
	return path, ok
}
