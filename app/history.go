package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/JohnnyReverb9/tuiple/config"
	"github.com/JohnnyReverb9/tuiple/filesystem"
)

type OpType string

const (
	OpRename     OpType = "rename"
	OpCreateFile OpType = "create_file"
	OpCreateDir  OpType = "create_dir"
	OpCopy       OpType = "copy"
	OpMove       OpType = "move"
	OpDelete     OpType = "delete"
)

type HistoryItem struct {
	Src   string
	Dst   string
	IsDir bool
}

type HistoryEvent struct {
	Op    OpType
	Items []HistoryItem
	When  time.Time // when the action was recorded (PushHistory sets this)
}

var (
	undoStack []HistoryEvent
	redoStack []HistoryEvent

	pendingDeletesMu sync.Mutex
	pendingDeletes   = make(map[string]pendingDelete)
)

type pendingDelete struct {
	timer     *time.Timer
	expiresAt time.Time
}

func ActiveDeletesCount() int {
	pendingDeletesMu.Lock()
	defer pendingDeletesMu.Unlock()
	return len(pendingDeletes)
}

func NextDeleteRemaining() time.Duration {
	pendingDeletesMu.Lock()
	defer pendingDeletesMu.Unlock()

	if len(pendingDeletes) == 0 {
		return 0
	}

	var oldest time.Time
	first := true
	for _, p := range pendingDeletes {
		if first || p.expiresAt.Before(oldest) {
			oldest = p.expiresAt
			first = false
		}
	}

	rem := time.Until(oldest)
	if rem < 0 {
		return 0
	}
	return rem
}

// DeleteEntry removes a path using the delete mode from the settings and
// returns where it went, which is what undo needs to put it back:
//
//   - timer:     a hidden sibling, purged after the configured countdown
//   - trash:     the macOS Trash
//   - permanent: nowhere — the empty destination is how the history
//     popup knows the action can no longer be reverted
func DeleteEntry(src string, isDir bool) (string, error) {
	switch config.Get().Delete.Mode {
	case config.DeleteTrash:
		return filesystem.MoveToTrash(src)
	case config.DeletePermanent:
		if err := filesystem.Remove(src, isDir); err != nil {
			return "", err
		}
		return "", nil
	default:
		return SoftDeletePath(src)
	}
}

// SoftDeletePath implements the timer mode: the file is moved out of
// sight next to itself and purged once the countdown expires.
func SoftDeletePath(src string) (string, error) {
	dir := filepath.Dir(src)
	base := filepath.Base(src)
	trashName := fmt.Sprintf(".%s.tuiple_trash_%d", base, time.Now().UnixNano())
	trashPath := filepath.Join(dir, trashName)

	err := filesystem.Move(src, trashPath)
	if err != nil {
		return "", err
	}

	// Read the countdown once, here: a setting changed while a delete is
	// pending must not retroactively move the deadline the status bar is
	// already counting down.
	countdown := time.Duration(config.Get().Delete.TimerSeconds) * time.Second

	pendingDeletesMu.Lock()
	timer := time.AfterFunc(countdown, func() {
		os.RemoveAll(trashPath)
		pendingDeletesMu.Lock()
		delete(pendingDeletes, trashPath)
		pendingDeletesMu.Unlock()
	})
	pendingDeletes[trashPath] = pendingDelete{
		timer:     timer,
		expiresAt: time.Now().Add(countdown),
	}
	pendingDeletesMu.Unlock()

	return trashPath, nil
}

func cancelSoftDelete(trashPath string) {
	pendingDeletesMu.Lock()
	defer pendingDeletesMu.Unlock()
	if pd, ok := pendingDeletes[trashPath]; ok {
		pd.timer.Stop()
		delete(pendingDeletes, trashPath)
	}
}

func CleanupPendingDeletes() {
	pendingDeletesMu.Lock()
	defer pendingDeletesMu.Unlock()
	for trashPath, pd := range pendingDeletes {
		pd.timer.Stop()
		os.RemoveAll(trashPath)
	}
	pendingDeletes = make(map[string]pendingDelete)
}

// maxUndoStack bounds the in-memory undo stack so a very long session
// (tuiple used as a primary file manager all day) can't drift into
// pathological memory use. Excess entries are dropped from the bottom
// (oldest) so the most recent N actions are always undoable. The audit
// log keeps the long-term record.
const maxUndoStack = 500

func PushHistory(ev HistoryEvent) {
	if ev.When.IsZero() {
		ev.When = time.Now()
	}
	undoStack = append(undoStack, ev)
	if len(undoStack) > maxUndoStack {
		// Drop the oldest entries; keep the most recent maxUndoStack.
		undoStack = append(undoStack[:0], undoStack[len(undoStack)-maxUndoStack:]...)
	}
	// Clear redo stack on new action
	redoStack = nil
	AppendAudit(ev, AuditSourceUser)
}

// HistorySnapshot returns copies of the undo and redo stacks for read-only
// viewing (e.g. by the history popup). The returned slices are safe to
// hold onto — they are not aliased to the live stacks.
func HistorySnapshot() (undo, redo []HistoryEvent) {
	undo = make([]HistoryEvent, len(undoStack))
	copy(undo, undoStack)
	redo = make([]HistoryEvent, len(redoStack))
	copy(redo, redoStack)
	return undo, redo
}

func Undo() (string, error) {
	if len(undoStack) == 0 {
		return "Nothing to undo", nil
	}
	// pop
	ev := undoStack[len(undoStack)-1]
	undoStack = undoStack[:len(undoStack)-1]

	err := revertEvent(&ev)
	if err != nil {
		// A restore macOS merely refused is worth keeping: the file is
		// still in the Trash, and the same u will work after Put Back or
		// after the terminal is granted Full Disk Access. Anything else
		// — a purged countdown, a file deleted for good — can never
		// succeed, so the entry is dropped rather than blocking every
		// older action behind it.
		if errors.Is(err, filesystem.ErrRestoreBlocked) {
			undoStack = append(undoStack, ev)
		}
		return "", fmt.Errorf("undo failed: %w", err)
	}

	redoStack = append(redoStack, ev)
	// Record the reversal in the audit log so future "where did that
	// file go" lookups see the file's true final location, not just the
	// original action that was later undone. We log a fresh event with
	// the current timestamp; the original op is preserved so the reader
	// can see what was undone, and source=undo signals the direction.
	audit := ev
	audit.When = time.Now()
	AppendAudit(audit, AuditSourceUndo)
	return fmt.Sprintf("Undid %s", ev.Op), nil
}

func Redo() (string, error) {
	if len(redoStack) == 0 {
		return "Nothing to redo", nil
	}
	// pop
	ev := redoStack[len(redoStack)-1]
	redoStack = redoStack[:len(redoStack)-1]

	err := applyEvent(&ev)
	if err != nil {
		return "", fmt.Errorf("redo failed: %w", err)
	}

	undoStack = append(undoStack, ev)
	audit := ev
	audit.When = time.Now()
	AppendAudit(audit, AuditSourceRedo)
	return fmt.Sprintf("Redid %s", ev.Op), nil
}

func revertEvent(ev *HistoryEvent) error {
	switch ev.Op {
	case OpRename, OpMove:
		// To revert move/rename, move Dst back to Src.
		steps := make([]renameStep, 0, len(ev.Items))
		for _, item := range ev.Items {
			steps = append(steps, renameStep{src: item.Dst, dst: item.Src})
		}
		if err := moveAll(steps); err != nil {
			return err
		}
	case OpCreateFile, OpCreateDir, OpCopy:
		// To revert create/copy, remove Dst
		for _, item := range ev.Items {
			if err := filesystem.Remove(item.Dst, item.IsDir); err != nil {
				return err
			}
		}
	case OpDelete:
		// To revert a delete, put the file back where it came from. The
		// route depends on where the delete put it: an item in the macOS
		// Trash needs Finder's cooperation, a timer-mode sibling is a
		// plain rename, and a permanent delete has nothing to restore.
		for _, item := range ev.Items {
			if item.Dst == "" {
				return fmt.Errorf("%s was deleted permanently", filepath.Base(item.Src))
			}
			if filesystem.InTrash(item.Dst) {
				if err := filesystem.RestoreFromTrash(item.Dst, item.Src); err != nil {
					return fmt.Errorf("could not restore %s: %w", filepath.Base(item.Src), err)
				}
				continue
			}
			if err := filesystem.Move(item.Dst, item.Src); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("file already permanently deleted")
				}
				return fmt.Errorf("could not restore %s: %w", filepath.Base(item.Src), err)
			}
			cancelSoftDelete(item.Dst)
		}
	}
	return nil
}

func applyEvent(ev *HistoryEvent) error {
	switch ev.Op {
	case OpRename, OpMove:
		steps := make([]renameStep, 0, len(ev.Items))
		for _, item := range ev.Items {
			steps = append(steps, renameStep{src: item.Src, dst: item.Dst})
		}
		if err := moveAll(steps); err != nil {
			return err
		}
	case OpCreateFile:
		for _, item := range ev.Items {
			if err := filesystem.CreateFile(item.Dst); err != nil {
				return err
			}
		}
	case OpCreateDir:
		for _, item := range ev.Items {
			if err := filesystem.CreateDir(item.Dst); err != nil {
				return err
			}
		}
	case OpCopy:
		for _, item := range ev.Items {
			if item.IsDir {
				if err := filesystem.CopyDir(item.Src, item.Dst); err != nil {
					return err
				}
			} else {
				if err := filesystem.CopyFile(item.Src, item.Dst); err != nil {
					return err
				}
			}
		}
	case OpDelete:
		for i, item := range ev.Items {
			dst, err := DeleteEntry(item.Src, item.IsDir)
			if err != nil {
				return err
			}
			ev.Items[i].Dst = dst
		}
	}
	return nil
}
