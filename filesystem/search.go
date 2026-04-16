package filesystem

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/sahilm/fuzzy"
)

// SearchMatch represents a single search result.
type SearchMatch struct {
	Path        string
	Name        string
	MatchedLine string
	LineNum     int
	Score       int // Used for sorting fuzzy results
}

// NameSearch recursively searches files by name up to depth limit.
func NameSearch(root, query string, maxDepth int) []SearchMatch {
	if query == "" {
		return nil
	}
	
	var allPaths []string
	
	// Fast pre-walk to collect paths
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		
		// calculate depth
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		
		allPaths = append(allPaths, path)
		return nil
	})

	matches := fuzzy.Find(query, allPaths)
	
	var results []SearchMatch
	for _, m := range matches {
		results = append(results, SearchMatch{
			Path:  m.Str,
			Name:  filepath.Base(m.Str),
			Score: m.Score,
		})
	}
	
	return results
}

// ContentSearch recursively searches file contents up to depth limit.
// Returns a slice of matches (one match per line).
func ContentSearch(root, query string, maxDepth int) []SearchMatch {
	if query == "" {
		return nil
	}

	queryLower := strings.ToLower(query)
	var results []SearchMatch

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		if depth > maxDepth {
			return nil
		}

		// Quick check to skip large files (e.g. > 10MB)
		info, err := d.Info()
		if err != nil || info.Size() > 10*1024*1024 {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 1
		for scanner.Scan() {
			line := scanner.Text()
			// Simple case-insensitive substring search
			if strings.Contains(strings.ToLower(line), queryLower) {
				results = append(results, SearchMatch{
					Path:        path,
					Name:        filepath.Base(path),
					MatchedLine: strings.TrimSpace(line),
					LineNum:     lineNum,
				})
			}
			lineNum++
		}
		return nil
	})

	return results
}
