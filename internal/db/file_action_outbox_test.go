package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/yappachino-dev/yci/internal/appctx"
	"github.com/yappachino-dev/yci/internal/fs"
	"github.com/yappachino-dev/yci/internal/store"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbConn, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	const schema = `
		CREATE TABLE fs_journal (
			id INTEGER PRIMARY KEY,
			op TEXT NOT NULL CHECK (op IN ('move_file', 'move_dir', 'delete_file')),
			src TEXT NOT NULL,
			dst TEXT,
			status TEXT NOT NULL CHECK (status IN ('pending', 'done', 'failed')) DEFAULT 'pending',
			error TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`

	if _, err := dbConn.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return dbConn
}

type testEnv struct {
	db       *sql.DB
	repo     *FileActionRepository
	executor *FileActionExecutor
	mover    *JournaledMover
	disk     *fs.OSDiskMover
	tx       *SQLiteTransactor
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	dbConn := newTestDB(t)
	queries := store.New(dbConn)
	repo := NewFileActionRepository(queries)
	disk := fs.NewOSDiskMover()
	executor := NewFileActionExecutor(repo, disk)

	return &testEnv{
		db:       dbConn,
		repo:     repo,
		executor: executor,
		mover:    NewJournaledMover(repo, disk),
		disk:     disk,
		tx:       NewSQLiteTransactor(dbConn),
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}

	return string(data)
}

func TestExecutorMovesPendingEntries(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	root := t.TempDir()
	src := filepath.Join(root, "a.opus")
	dst := filepath.Join(root, "sub", "b.opus")

	mustWrite(t, src, "audio")
	mustWrite(t, filepath.Join(root, "placeholder"), "")

	if err := env.repo.Record(ctx, []FileActionEntry{{Op: OpMoveFile, Src: src, Dst: dst}}); err != nil {
		t.Fatalf("record: %v", err)
	}

	if err := env.executor.ExecutePending(ctx); err != nil {
		t.Fatalf("execute pending: %v", err)
	}

	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("source still exists: %v", err)
	}
	if got := mustRead(t, dst); got != "audio" {
		t.Errorf("destination content = %q, want %q", got, "audio")
	}

	pending, err := env.repo.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending entries = %d, want 0", len(pending))
	}
}

func TestExecutorIsIdempotentOnReplay(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	root := t.TempDir()
	src := filepath.Join(root, "a.opus")
	dst := filepath.Join(root, "b.opus")

	// Simulate a crash between rename and marking done:
	// the physical move already happened, the entry is still pending.
	mustWrite(t, dst, "audio")

	if err := env.repo.Record(ctx, []FileActionEntry{{Op: OpMoveFile, Src: src, Dst: dst}}); err != nil {
		t.Fatalf("record: %v", err)
	}

	if err := env.executor.ExecutePending(ctx); err != nil {
		t.Fatalf("replay should treat completed move as success: %v", err)
	}
}

func TestExecutorFailsHardOnConflicts(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	root := t.TempDir()
	src := filepath.Join(root, "a.opus")
	dst := filepath.Join(root, "b.opus")

	mustWrite(t, src, "one")
	mustWrite(t, dst, "two")

	if err := env.repo.Record(ctx, []FileActionEntry{{Op: OpMoveFile, Src: src, Dst: dst}}); err != nil {
		t.Fatalf("record: %v", err)
	}

	if err := env.executor.ExecutePending(ctx); err == nil {
		t.Error("expected conflict error when both source and destination exist")
	}

	failed, err := env.repo.Failed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 {
		t.Fatalf("failed entries = %d, want 1", len(failed))
	}

	// Reset and remove the conflict so repair converges.
	if err := os.Remove(dst); err != nil {
		t.Fatal(err)
	}
	if err := env.executor.ResetFailed(ctx); err != nil {
		t.Fatal(err)
	}
	if err := env.executor.ExecutePending(ctx); err != nil {
		t.Fatalf("retry after reset failed: %v", err)
	}
	if got := mustRead(t, dst); got != "one" {
		t.Errorf("destination content = %q, want %q", got, "one")
	}
}

func TestExecutorDeletesAreIdempotent(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	root := t.TempDir()
	path := filepath.Join(root, "a.opus")

	mustWrite(t, path, "audio")

	if err := env.repo.Record(ctx, []FileActionEntry{{Op: OpDeleteFile, Src: path}}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := env.executor.ExecutePending(ctx); err != nil {
		t.Fatalf("execute delete: %v", err)
	}

	// Re-recording a delete of an already-removed file replays cleanly.
	if err := env.repo.Record(ctx, []FileActionEntry{{Op: OpDeleteFile, Src: path}}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := env.executor.ExecutePending(ctx); err != nil {
		t.Fatalf("delete replay of missing file should succeed: %v", err)
	}
}

func TestJournalCommitsWithTransactionAndFlushesAfterCommit(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.tx.SetPostCommit(env.executor.ExecutePending)

	root := t.TempDir()
	src := filepath.Join(root, "staging.opus")
	dst := filepath.Join(root, "library", "track.opus")

	mustWrite(t, src, "audio")

	err := env.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if _, ok := appctx.TxFromContext[*sql.Tx](txCtx); !ok {
			t.Error("expected transaction in context")
		}

		return env.mover.MoveFile(txCtx, src, dst)
	})
	if err != nil {
		t.Fatalf("transactional move: %v", err)
	}

	// Physical move must have happened via the post-commit flush...
	if got := mustRead(t, dst); got != "audio" {
		t.Errorf("destination content = %q, want %q", got, "audio")
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("source still exists: %v", err)
	}

	// ...and nothing may remain pending.
	pending, err := env.repo.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending entries after commit flush = %d, want 0", len(pending))
	}
}

func TestRollbackDiscardsRecordedActions(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	root := t.TempDir()
	src := filepath.Join(root, "a.opus")
	dst := filepath.Join(root, "b.opus")

	mustWrite(t, src, "audio")

	boom := errors.New("metadata failure")
	err := env.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := env.mover.MoveFile(txCtx, src, dst); err != nil {
			return err
		}

		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}

	pending, err := env.repo.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("journal survived rollback: %d pending entries", len(pending))
	}

	// No physical mutation may have occurred either.
	if _, statErr := os.Stat(src); statErr != nil {
		t.Errorf("source vanished despite rollback: %v", statErr)
	}
	if _, statErr := os.Stat(dst); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("destination created despite rollback: %v", statErr)
	}
}

func TestDirectoryMoveViaJournal(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	root := t.TempDir()
	oldDir := filepath.Join(root, "old-artist")
	newDir := filepath.Join(root, "nested", "new-artist")

	mustWrite(t, filepath.Join(oldDir, "track.opus"), "audio")

	if err := env.repo.Record(ctx, []FileActionEntry{{Op: OpMoveDir, Src: oldDir, Dst: newDir}}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := env.executor.ExecutePending(ctx); err != nil {
		t.Fatalf("execute dir move: %v", err)
	}

	if got := mustRead(t, filepath.Join(newDir, "track.opus")); got != "audio" {
		t.Errorf("moved track content = %q, want %q", got, "audio")
	}
}

func TestExecutionStopsAtFirstFailureKeepingRestPending(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	root := t.TempDir()
	missing := filepath.Join(root, "missing.opus")
	okSrc := filepath.Join(root, "ok.opus")
	okDst := filepath.Join(root, "ok-dst.opus")

	mustWrite(t, okSrc, "audio")

	// First entry references a nonexistent source, second is fine.
	entries := []FileActionEntry{
		{Op: OpMoveFile, Src: missing, Dst: okDst},
		{Op: OpMoveFile, Src: okSrc, Dst: okDst},
	}

	if err := env.repo.Record(ctx, entries); err != nil {
		t.Fatalf("record: %v", err)
	}

	if err := env.executor.ExecutePending(ctx); err == nil {
		t.Fatal("expected failure on missing source")
	}

	failed, err := env.repo.Failed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 {
		t.Errorf("failed entries = %d, want 1", len(failed))
	}

	pending, err := env.repo.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Errorf("pending entries = %d, want 1", len(pending))
	}
}
