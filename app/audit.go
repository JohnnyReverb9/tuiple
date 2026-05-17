package app

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"tuiple/filesystem"
)

// AuditSource describes how a logged history event came to be: was it
// the user's original intent (`user`), or did it happen because the
// user pressed undo (`undo`) or redo (`redo`)?  This lets the audit
// viewer reflect what actually hit disk, not just what was intended.
type AuditSource string

const (
	AuditSourceUser AuditSource = "user"
	AuditSourceUndo AuditSource = "undo"
	AuditSourceRedo AuditSource = "redo"
)

// AuditEntry is the on-disk JSONL record. Field tags are kept short
// because every byte counts inside the rotation budget.
type AuditEntry struct {
	When   time.Time     `json:"time"`
	Op     OpType        `json:"op"`
	Source AuditSource   `json:"source"`
	Items  []HistoryItem `json:"items,omitempty"`
}

// Audit storage settings. Both the file location and the rotation cap
// are deliberate — see the package doc and history_popup.go for the
// design rationale.
const (
	auditMaxBytes int64 = 5 * 1024 * 1024 // 5 MB hard cap; trimmed in halves
)

var auditMu sync.Mutex // serialises file writes (the main goroutine is sequential, but defense in depth)

// auditPath returns ~/.config/tuiple/audit.jsonl, alongside the existing
// bookmarks.json so both tuiple state files live in one place.
func auditPath() string {
	return filepath.Join(filesystem.HomeDir(), ".config", "tuiple", "audit.jsonl")
}

// AppendAudit writes one JSONL record describing ev to the audit log.
// Errors are intentionally swallowed: the audit log is a convenience,
// not a correctness mechanism, so a failed write must never block or
// surface as a user-visible error in the middle of a file operation.
func AppendAudit(ev HistoryEvent, source AuditSource) {
	auditMu.Lock()
	defer auditMu.Unlock()

	path := auditPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}

	rec := AuditEntry{
		When:   ev.When,
		Op:     ev.Op,
		Source: source,
		Items:  ev.Items,
	}
	if rec.When.IsZero() {
		rec.When = time.Now()
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
	_ = f.Sync()

	// Cheap rotation check: stat the file we just wrote to, trim if
	// we've crossed the budget.
	if info, err := f.Stat(); err == nil && info.Size() > auditMaxBytes {
		_ = rotateAuditLocked(path)
	}
}

// rotateAuditLocked drops the older half of the audit log when it grows
// past auditMaxBytes.  Must be called with auditMu held.
//
// Implementation: load every line, keep the second half, write back to
// a temp file, atomically rename.  Anything we fail to parse is dropped
// to avoid carrying corrupt records forward.
func rotateAuditLocked(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	var lines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		b := sc.Bytes()
		if len(b) == 0 {
			continue
		}
		// Copy because scanner reuses its buffer.
		cp := make([]byte, len(b))
		copy(cp, b)
		lines = append(lines, cp)
	}
	f.Close()

	if len(lines) <= 1 {
		return nil
	}
	keep := lines[len(lines)/2:]

	tmp := path + ".rot"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(out)
	for _, ln := range keep {
		w.Write(ln)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	return os.Rename(tmp, path)
}

// ReadAudit returns every record in the audit file, newest first. The
// caller doesn't have to worry about file existence: a missing log
// simply yields an empty slice.
func ReadAudit() ([]AuditEntry, error) {
	auditMu.Lock()
	defer auditMu.Unlock()

	f, err := os.Open(auditPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []AuditEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // skip corrupt rows silently
		}
		entries = append(entries, e)
	}

	// Newest first so the popup can scroll without sorting per draw.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].When.After(entries[j].When)
	})
	return entries, nil
}
