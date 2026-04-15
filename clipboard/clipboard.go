// Package clipboard manages internal copy/cut operations.
package clipboard

import "tuiple/filesystem"

// OpType defines whether the files are copied or cut.
type OpType int

const (
	OpNone OpType = iota
	OpCopy
	OpCut
)

// Item holds a single clipboard entry.
type Item struct {
	Entry filesystem.FileEntry
}

var (
	clipboardItems []Item
	currentOp      OpType
)

// Set stores a list of files and the operation type.
func Set(items []Item, op OpType) {
	clipboardItems = items
	currentOp = op
}

// Get returns the current items and op type.
func Get() ([]Item, OpType) {
	return clipboardItems, currentOp
}

// Clear empties the clipboard.
func Clear() {
	clipboardItems = nil
	currentOp = OpNone
}
