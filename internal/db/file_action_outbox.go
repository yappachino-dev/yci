package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yappachino-dev/yci/internal/appctx"
	"github.com/yappachino-dev/yci/internal/store"
)

// FileActionOp enumerates the physical filesystem operations that can be
// journaled. Every operation must be safe to replay (idempotent) because the
// journal is re-executed after crashes.
type FileActionOp string

const (
	OpMoveFile   FileActionOp = "move_file"
	OpMoveDir    FileActionOp = "move_dir"
	OpDeleteFile FileActionOp = "delete_file"
)

func (op FileActionOp) valid() bool {
	switch op {
	case OpMoveFile, OpMoveDir, OpDeleteFile:
		return true
	default:
		return false
	}
}

// FileActionEntry describes one planned physical filesystem operation. Paths
// are absolute native paths so a journal recorded before a configuration
// change still points at the physical locations involved at record time.
type FileActionEntry struct {
	Op  FileActionOp
	Src string
	Dst string // ignored for OpDeleteFile
}

// DiskMover is the low-level physical primitive used to execute journal
// entries. It is implemented by fs.OSDiskMover.
type DiskMover interface {
	MoveFile(ctx context.Context, oldPath, newPath string) error
	MoveDirectory(ctx context.Context, oldRootPath, newRootPath string) error
	DeleteFile(ctx context.Context, path string) error
}

// FileActionRepository records and tracks planned filesystem operations in
// the fs_journal table. Recording happens inside the caller's transaction (if
// any) so that the journal commits atomically together with metadata changes.
type FileActionRepository struct {
	q *store.Queries
}

func NewFileActionRepository(q *store.Queries) *FileActionRepository {
	return &FileActionRepository{q: q}
}

// Record persists journal entries within the caller's transaction when one is
// active, otherwise directly on the database.
func (r *FileActionRepository) Record(ctx context.Context, entries []FileActionEntry) error {
	for _, entry := range entries {
		if !entry.Op.valid() {
			return fmt.Errorf("record file action: unknown op %q", entry.Op)
		}

		if entry.Src == "" {
			return fmt.Errorf("record file action %q: src cannot be empty", entry.Op)
		}

		params := store.RecordFileActionParams{
			Op:  string(entry.Op),
			Src: entry.Src,
			Dst: ToNullString(entry.Dst),
		}

		if _, err := r.db(ctx).RecordFileAction(ctx, params); err != nil {
			return fmt.Errorf("record file action %q %q -> %q: %w", entry.Op, entry.Src, entry.Dst, err)
		}
	}

	return nil
}

// Pending returns all unexecuted journal entries in insertion order.
// Insertion order is execution order: operations are causal.
func (r *FileActionRepository) Pending(ctx context.Context) ([]store.GetPendingFileActionsRow, error) {
	rows, err := r.db(ctx).GetPendingFileActions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending file actions: %w", err)
	}

	return rows, nil
}

// Failed returns journal entries that failed during execution.
func (r *FileActionRepository) Failed(ctx context.Context) ([]store.GetFailedFileActionsRow, error) {
	rows, err := r.db(ctx).GetFailedFileActions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list failed file actions: %w", err)
	}

	return rows, nil
}

func (r *FileActionRepository) markDone(ctx context.Context, id int64) error {
	if err := r.db(ctx).CompleteFileAction(ctx, id); err != nil {
		return fmt.Errorf("complete file action %d: %w", id, err)
	}

	return nil
}

func (r *FileActionRepository) markFailed(ctx context.Context, id int64, cause error) error {
	err := r.db(ctx).FailFileAction(ctx, store.FailFileActionParams{
		Error: sql.NullString{String: cause.Error(), Valid: true},
		ID:    id,
	})
	if err != nil {
		return fmt.Errorf("fail file action %d: %w", id, err)
	}

	return nil
}

// ResetFailed requeues failed entries for another execution attempt.
func (r *FileActionRepository) ResetFailed(ctx context.Context) error {
	if err := r.db(ctx).ResetFailedFileActions(ctx); err != nil {
		return fmt.Errorf("reset failed file actions: %w", err)
	}

	return nil
}

// PurgeDone deletes successfully executed journal entries.
func (r *FileActionRepository) PurgeDone(ctx context.Context) error {
	if err := r.db(ctx).PurgeCompletedFileActions(ctx); err != nil {
		return fmt.Errorf("purge completed file actions: %w", err)
	}

	return nil
}

func (r *FileActionRepository) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return r.q.WithTx(tx)
	}

	return r.q
}

// JournaledMover implements the physical mover interfaces used by the library
// services. Inside a transaction it only records the intended operation into
// the journal; the actual disk mutation happens after the transaction commits,
// driven by FileActionExecutor. Outside a transaction it falls back to direct
// execution.
type JournaledMover struct {
	repo *FileActionRepository
	disk DiskMover
}

func NewJournaledMover(repo *FileActionRepository, disk DiskMover) *JournaledMover {
	return &JournaledMover{repo: repo, disk: disk}
}

func (m *JournaledMover) MoveFile(ctx context.Context, oldPath, newPath string) error {
	return m.recordOrExecute(ctx, FileActionEntry{Op: OpMoveFile, Src: oldPath, Dst: newPath})
}

func (m *JournaledMover) MoveDirectory(ctx context.Context, oldRootPath, newRootPath string) error {
	return m.recordOrExecute(ctx, FileActionEntry{Op: OpMoveDir, Src: oldRootPath, Dst: newRootPath})
}

func (m *JournaledMover) DeleteFile(ctx context.Context, path string) error {
	return m.recordOrExecute(ctx, FileActionEntry{Op: OpDeleteFile, Src: path})
}

func (m *JournaledMover) recordOrExecute(ctx context.Context, entry FileActionEntry) error {
	if _, inTx := appctx.TxFromContext[*sql.Tx](ctx); inTx {
		return m.repo.Record(ctx, []FileActionEntry{entry})
	}

	return ExecuteFileAction(ctx, m.disk, entry)
}

// FileActionExecutor replays journal entries against the physical filesystem.
// Execution is forward-only (redo): every operation is applied idempotently so
// that a crash between "rename done" and "mark done" converges on retry.
type FileActionExecutor struct {
	repo *FileActionRepository
	disk DiskMover
}

func NewFileActionExecutor(repo *FileActionRepository, disk DiskMover) *FileActionExecutor {
	return &FileActionExecutor{repo: repo, disk: disk}
}

// Pending lists unexecuted journal entries in execution order.
func (e *FileActionExecutor) Pending(ctx context.Context) ([]store.GetPendingFileActionsRow, error) {
	return e.repo.Pending(ctx)
}

// Failed lists journal entries that failed during a previous execution.
func (e *FileActionExecutor) Failed(ctx context.Context) ([]store.GetFailedFileActionsRow, error) {
	return e.repo.Failed(ctx)
}

// ResetFailed requeues failed journal entries for another attempt.
func (e *FileActionExecutor) ResetFailed(ctx context.Context) error {
	return e.repo.ResetFailed(ctx)
}

// PurgeDone removes successfully executed entries from the journal.
func (e *FileActionExecutor) PurgeDone(ctx context.Context) error {
	return e.repo.PurgeDone(ctx)
}

// ExecutePending applies every pending journal entry in order. On the first
// failure it marks that entry as failed and stops; remaining entries stay
// pending and are retried on the next run.
func (e *FileActionExecutor) ExecutePending(ctx context.Context) error {
	pending, err := e.repo.Pending(ctx)
	if err != nil {
		return err
	}

	for _, row := range pending {
		entry := FileActionEntry{
			Op:  FileActionOp(row.Op),
			Src: row.Src,
			Dst: row.Dst.String,
		}

		if err := ExecuteFileAction(ctx, e.disk, entry); err != nil {
			if markErr := e.repo.markFailed(ctx, row.ID, err); markErr != nil {
				return errors.Join(err, markErr)
			}

			return fmt.Errorf("execute file action #%d (%s %q -> %q): %w", row.ID, row.Op, row.Src, row.Dst.String, err)
		}

		if err := e.repo.markDone(ctx, row.ID); err != nil {
			return err
		}
	}

	return nil
}

// ExecuteFileAction applies a single operation with redo semantics:
//
//	move:      src missing + dst exists -> already done (success)
//	           both present or both missing -> hard failure
//	delete:    src missing -> already done (success)
func ExecuteFileAction(ctx context.Context, disk DiskMover, entry FileActionEntry) error {
	srcExists, err := pathExists(entry.Src)
	if err != nil {
		return fmt.Errorf("inspect source %q: %w", entry.Src, err)
	}

	var dstExists bool
	var dst string

	if entry.Op == OpMoveFile || entry.Op == OpMoveDir {
		dst = entry.Dst
		if dst == "" {
			return fmt.Errorf("move action requires a destination")
		}

		dstExists, err = pathExists(dst)
		if err != nil {
			return fmt.Errorf("inspect destination %q: %w", dst, err)
		}
	}

	switch entry.Op {
	case OpMoveFile, OpMoveDir:
		switch {
		case srcExists && dstExists:
			return fmt.Errorf("refusing move: both %q and %q exist", entry.Src, dst)

		case !srcExists && !dstExists:
			return fmt.Errorf("cannot move: neither %q nor %q exist", entry.Src, dst)

		case !srcExists && dstExists:
			return nil // already executed by an earlier attempt

		default:
			return performMove(ctx, disk, entry)
		}

	case OpDeleteFile:
		if !srcExists {
			return nil // already deleted by an earlier attempt
		}

		return disk.DeleteFile(ctx, entry.Src)

	default:
		return fmt.Errorf("unknown file action op %q", entry.Op)
	}
}

func performMove(ctx context.Context, disk DiskMover, entry FileActionEntry) error {
	if err := os.MkdirAll(filepath.Dir(entry.Dst), 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	if entry.Op == OpMoveDir {
		return disk.MoveDirectory(ctx, entry.Src, entry.Dst)
	}

	return disk.MoveFile(ctx, entry.Src, entry.Dst)
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}
