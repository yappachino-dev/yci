package download

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/store"
)

// newQueueRepo builds a queue repository over a real migrated database.
//
// The queue is a state machine with three live statuses and two terminal
// ones, and every transition here is a place a download can get stuck or
// start twice. None of it had been exercised, so these tests exist to pin
// the transitions rather than the types.
func newQueueRepo(t *testing.T) *SQLiteQueueRepo {
	t.Helper()

	if err := db.SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if _, err := db.Migrate(t.Context(), conn, nopWriter{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return NewSQLiteQueueRepo(store.New(conn))
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

func mustEnqueue(t *testing.T, r *SQLiteQueueRepo, sourceID string) {
	t.Helper()

	added, err := r.Enqueue(t.Context(), sourceID)
	if err != nil {
		t.Fatalf("Enqueue(%q): %v", sourceID, err)
	}
	if !added {
		t.Errorf("Enqueue(%q) reported the entry was not added", sourceID)
	}
}

func entryBySource(t *testing.T, r *SQLiteQueueRepo, sourceID string) QueueEntry {
	t.Helper()

	all, err := r.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range all {
		if e.SourceID == sourceID {
			return e
		}
	}

	t.Fatalf("no queue entry for %q", sourceID)

	return QueueEntry{}
}

// TestEnqueueIsIdempotent covers the property that stops a re-sync queueing
// the same download twice: the second attempt must report "not added" rather
// than creating a second row.
func TestEnqueueIsIdempotent(t *testing.T) {
	repo := newQueueRepo(t)
	ctx := context.Background()

	mustEnqueue(t, repo, "dQw4w9WgXcQ")

	added, err := repo.Enqueue(ctx, "dQw4w9WgXcQ")
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Error("a duplicate source ID was added to the queue a second time")
	}

	all, err := repo.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("queue holds %d entries, want 1", len(all))
	}
}

// TestQueueLifecycle walks the normal path: pending, picked up, started
// (which increments attempts), then removed on completion.
func TestQueueLifecycle(t *testing.T) {
	repo := newQueueRepo(t)
	ctx := context.Background()

	mustEnqueue(t, repo, "dQw4w9WgXcQ")

	next, ok, err := repo.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("no actionable entry for a pending queue")
	}
	if next.Status != QueueStatusPending {
		t.Errorf("picked status = %q, want pending", next.Status)
	}
	if next.Attempts != 0 {
		t.Errorf("picked attempts = %d, want 0", next.Attempts)
	}

	if err := repo.Start(ctx, next.ID); err != nil {
		t.Fatal(err)
	}

	// A processing entry is not actionable, or two runs would race for it.
	if _, ok, err := repo.Next(ctx); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Error("a processing entry was offered as actionable")
	}

	if got := entryBySource(t, repo, "dQw4w9WgXcQ").Attempts; got != 1 {
		t.Errorf("attempts after Start = %d, want 1", got)
	}

	// Completion removes the row: the schema permits only pending,
	// processing and failed, so there is no state to record "done" in.
	if err := repo.Complete(ctx, next.ID); err != nil {
		t.Fatal(err)
	}

	all, err := repo.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("queue still holds %d entries after completion: %+v", len(all), all)
	}
}

// TestFailureRecordsTheReasonAndRetries covers the state a stuck download
// actually lands in: the error text has to survive, because it is the only
// thing telling the user why.
func TestFailureRecordsTheReasonAndRetries(t *testing.T) {
	repo := newQueueRepo(t)
	ctx := context.Background()

	mustEnqueue(t, repo, "dQw4w9WgXcQ")

	next, _, err := repo.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Start(ctx, next.ID); err != nil {
		t.Fatal(err)
	}

	// A transient failure goes back to pending so it is retried.
	if err := repo.Fail(ctx, next.ID, string(QueueStatusPending), errors.New("HTTP Error 503: Service Unavailable")); err != nil {
		t.Fatal(err)
	}

	after := entryBySource(t, repo, "dQw4w9WgXcQ")
	if after.Status != QueueStatusPending {
		t.Errorf("status after a transient failure = %q, want pending", after.Status)
	}
	if after.ErrorMsg != "HTTP Error 503: Service Unavailable" {
		t.Errorf("error message = %q, want the reason to be kept", after.ErrorMsg)
	}
	if _, ok, err := repo.Next(ctx); err != nil || !ok {
		t.Errorf("a retried entry should be actionable again (ok=%v err=%v)", ok, err)
	}

	// Exhausting the attempts makes it terminal, and it stops being offered.
	// Start is what increments the counter, so the retry has to go through it
	// the way a real run does.
	if err := repo.Start(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Fail(ctx, next.ID, string(QueueStatusFailed), errors.New("HTTP Error 403: Forbidden")); err != nil {
		t.Fatal(err)
	}

	final := entryBySource(t, repo, "dQw4w9WgXcQ")
	if final.Status != QueueStatusFailed {
		t.Errorf("status after exhausting attempts = %q, want failed", final.Status)
	}
	if final.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", final.Attempts)
	}
	if _, ok, _ := repo.Next(ctx); ok {
		t.Error("a failed entry was still offered as actionable")
	}
}

// TestRetryReArmsAFailedEntry covers `download retry`.
func TestRetryReArmsAFailedEntry(t *testing.T) {
	repo := newQueueRepo(t)
	ctx := context.Background()

	mustEnqueue(t, repo, "dQw4w9WgXcQ")
	next, _, err := repo.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Start(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Fail(ctx, next.ID, string(QueueStatusFailed), errors.New("boom")); err != nil {
		t.Fatal(err)
	}

	ok, err := repo.RetryByID(ctx, next.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("RetryByID reported nothing was re-armed")
	}

	retried := entryBySource(t, repo, "dQw4w9WgXcQ")
	if retried.Status != QueueStatusPending {
		t.Errorf("status after retry = %q, want pending", retried.Status)
	}
	if retried.ErrorMsg != "" {
		t.Errorf("the stale error message survived a retry: %q", retried.ErrorMsg)
	}

	// Retrying an ID that is not in the queue is not an error, but must not
	// claim to have re-armed anything.
	ok, err = repo.RetryByID(ctx, 999999)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("RetryByID claimed to re-arm an entry that does not exist")
	}
}

// TestRetryHardFailedOnlyTouchesExhaustedEntries is the difference between
// re-arming real failures and re-arming everything.
func TestRetryHardFailedOnlyTouchesExhaustedEntries(t *testing.T) {
	repo := newQueueRepo(t)
	ctx := context.Background()

	// One that exhausted its attempts. The reset is keyed on
	// attempts >= maxAttempts, not merely on being failed, so the entry has
	// to be failed repeatedly to count as hard-failed.
	mustEnqueue(t, repo, "hardfail01")
	id := entryBySource(t, repo, "hardfail01").ID

	// Start on the same entry repeatedly rather than re-arming between
	// rounds: RetryByID resets attempts to 0, so a retry-per-round loop would
	// never accumulate to the exhaustion threshold the reset is keyed on.
	for i := 0; i < maxAttempts; i++ {
		if err := repo.Start(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := repo.Fail(ctx, id, string(QueueStatusFailed), errors.New("boom")); err != nil {
			t.Fatal(err)
		}
	}

	if got := entryBySource(t, repo, "hardfail01").Attempts; got != maxAttempts {
		t.Fatalf("attempts = %d, want %d before the bulk reset", got, maxAttempts)
	}

	// One still merely pending.
	mustEnqueue(t, repo, "stillpend1")

	n, err := repo.RetryHardFailed(ctx, maxAttempts)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("re-armed %d entries, want just the failed one", n)
	}

	rearmed := entryBySource(t, repo, "hardfail01")
	if rearmed.Status != QueueStatusPending {
		t.Errorf("the hard-failed entry is now %q, want pending", rearmed.Status)
	}
	// The counter has to reset too, or the next failure exhausts attempts
	// immediately and the retry buys nothing.
	if rearmed.Attempts != 0 {
		t.Errorf("attempts after reset = %d, want 0", rearmed.Attempts)
	}
	if rearmed.ErrorMsg != "" {
		t.Errorf("the stale error survived the reset: %q", rearmed.ErrorMsg)
	}
	if got := entryBySource(t, repo, "stillpend1").Status; got != QueueStatusPending {
		t.Errorf("the untouched entry became %q", got)
	}
}

// TestResetPendingClearsInterruptedRuns covers a download left 'processing'
// by a crash: the next run must be able to pick it up again.
func TestResetPendingClearsInterruptedRuns(t *testing.T) {
	repo := newQueueRepo(t)
	ctx := context.Background()

	mustEnqueue(t, repo, "interrupted")
	next, _, err := repo.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Start(ctx, next.ID); err != nil {
		t.Fatal(err)
	}

	if _, ok, _ := repo.Next(ctx); ok {
		t.Fatal("a processing entry should not be actionable before the reset")
	}

	if err := repo.ResetPending(ctx, maxAttempts); err != nil {
		t.Fatal(err)
	}

	if got := entryBySource(t, repo, "interrupted").Status; got != QueueStatusPending {
		t.Errorf("status after ResetPending = %q, want pending", got)
	}
	if _, ok, err := repo.Next(ctx); err != nil || !ok {
		t.Errorf("the reset entry should be actionable (ok=%v err=%v)", ok, err)
	}
}

// TestRepositoryRoutesThroughTheTransaction guards the SQLITE_BUSY fix at the
// level it matters: a queue write inside a transaction must use that
// transaction, not a second connection.
func TestRepositoryRoutesThroughTheTransaction(t *testing.T) {
	if err := db.SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}

	path := filepath.Join(t.TempDir(), "tx.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	if _, err := db.Migrate(t.Context(), conn, nopWriter{}); err != nil {
		t.Fatal(err)
	}

	transactor := db.NewSQLiteTransactor(conn)
	repo := NewSQLiteQueueRepo(store.New(conn))

	err = transactor.WithinTransaction(t.Context(), func(txCtx context.Context) error {
		_, err := repo.Enqueue(txCtx, "intx0000001")
		return err
	})
	if err != nil {
		t.Fatalf("enqueue inside a transaction: %v", err)
	}

	all, err := repo.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("queue holds %d entries after the transaction, want 1", len(all))
	}
}

// TestDependenciesAreReportedNotFatal pins the contract that a missing
// external tool is reported rather than crashing, since imports must degrade
// instead of failing.
func TestDependenciesAreReportedNotFatal(t *testing.T) {
	err := CheckDependencies()
	if err == nil {
		// yt-dlp is present in this environment; nothing to assert beyond
		// that the call is safe to make.
		return
	}

	if !errors.Is(err, ErrDependencyMissing) {
		t.Errorf("CheckDependencies error = %v, want ErrDependencyMissing", err)
	}
	if !strings.Contains(err.Error(), "yt-dlp") && !strings.Contains(err.Error(), "ffmpeg") {
		t.Errorf("the error does not name the missing tool: %v", err)
	}
}
