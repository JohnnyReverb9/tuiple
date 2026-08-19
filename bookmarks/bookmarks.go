package bookmarks

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/JohnnyReverb9/tuiple/filesystem"
)

var (
	marksPath = filepath.Join(filesystem.HomeDir(), ".config", "tuiple", "bookmarks.json")
	favorites []string
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
	// Try parsing as array of strings
	var paths []string
	if err := json.Unmarshal(data, &paths); err == nil {
		favorites = paths
		return
	}
	
	// Legacy migration: if it was a map
	var stringMap map[string]string
	if err := json.Unmarshal(data, &stringMap); err == nil {
		for _, v := range stringMap {
			favorites = append(favorites, v)
		}
		Save() // Upgrade to new format
	}
}

// Save writes bookmarks to disk.
func Save() error {
	if err := EnsureConfigDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(favorites, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(marksPath, data, 0644)
}

// Toggle toggles a path in the favorites list. Returns true if added, false if removed.
func Toggle(path string) (bool, error) {
	for i, f := range favorites {
		if f == path {
			// Remove it
			favorites = append(favorites[:i], favorites[i+1:]...)
			return false, Save()
		}
	}
	// Add it
	favorites = append(favorites, path)
	return true, Save()
}

// GetFavorites returns the current list of favorite paths.
func GetFavorites() []string {
	return favorites
}
