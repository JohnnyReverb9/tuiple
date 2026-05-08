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

// textExtensions is a whitelist of extensions treated as plain text.
var textExtensions = map[string]struct{}{
	// code
	".go": {}, ".py": {}, ".js": {}, ".ts": {}, ".jsx": {}, ".tsx": {},
	".c": {}, ".h": {}, ".cpp": {}, ".cc": {}, ".cxx": {}, ".hpp": {},
	".java": {}, ".kt": {}, ".kts": {}, ".scala": {}, ".groovy": {},
	".rs": {}, ".rb": {}, ".php": {}, ".swift": {}, ".cs": {},
	".lua": {}, ".pl": {}, ".pm": {}, ".r": {}, ".jl": {},
	".sh": {}, ".bash": {}, ".zsh": {}, ".fish": {}, ".ps1": {},
	".dart": {}, ".ex": {}, ".exs": {}, ".erl": {}, ".hrl": {},
	".hs": {}, ".lhs": {}, ".ml": {}, ".mli": {}, ".fs": {}, ".fsi": {},
	".clj": {}, ".cljs": {}, ".cljc": {}, ".elm": {}, ".purs": {},
	".nim": {}, ".cr": {}, ".zig": {}, ".v": {}, ".d": {},
	// web / markup
	".html": {}, ".htm": {}, ".xml": {}, ".xhtml": {}, ".svg": {},
	".css": {}, ".scss": {}, ".sass": {}, ".less": {}, ".styl": {},
	// data / config
	".json": {}, ".jsonc": {}, ".json5": {},
	".yaml": {}, ".yml": {},
	".toml": {}, ".ini": {}, ".cfg": {}, ".conf": {}, ".config": {},
	".env": {}, ".properties": {}, ".plist": {},
	".csv": {}, ".tsv": {},
	// docs
	".md": {}, ".mdx": {}, ".markdown": {}, ".rst": {}, ".txt": {},
	".tex": {}, ".latex": {}, ".org": {}, ".adoc": {}, ".pod": {},
	// templates
	".tmpl": {}, ".tpl": {}, ".j2": {}, ".jinja": {}, ".jinja2": {},
	".erb": {}, ".haml": {}, ".slim": {}, ".pug": {}, ".njk": {},
	".hbs": {}, ".mustache": {},
	// misc text
	".log": {}, ".sql": {}, ".graphql": {}, ".gql": {},
	".proto": {}, ".thrift": {}, ".avsc": {},
	".tf": {}, ".tfvars": {}, ".hcl": {},
	".diff": {}, ".patch": {},
	".dockerfile": {}, ".containerfile": {},
	".vim": {}, ".vimrc": {},
	".editorconfig": {}, ".gitignore": {}, ".gitattributes": {},
}

// isTextFile reports whether path should be treated as plain text.
// Files with no extension fall back to a null-byte binary check.
func isTextFile(path string, data []byte) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		// No extension: accept only if no null bytes found.
		return !isBinary(data)
	}
	_, ok := textExtensions[ext]
	return ok
}

// isBinary reports whether data contains null bytes (reliable binary indicator).
func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// NameSearch recursively searches files by name up to depth limit.
func NameSearch(root, query string, maxDepth int) []SearchMatch {
	if query == "" {
		return nil
	}

	var allPaths []string

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator)) + 1

		if depth > maxDepth {
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
		if err != nil {
			return nil
		}

		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
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

		info, err := d.Info()
		if err != nil || info.Size() > 10*1024*1024 {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		// Read header for binary/text detection.
		header := make([]byte, 8192)
		n, _ := file.Read(header)
		if !isTextFile(path, header[:n]) {
			return nil
		}
		if _, err := file.Seek(0, 0); err != nil {
			return nil
		}

		scanner := bufio.NewScanner(file)
		lineNum := 1
		for scanner.Scan() {
			line := scanner.Text()
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
