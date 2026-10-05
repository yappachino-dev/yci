package download

import (
	"context"
	"errors"
	"testing"
	"time"
)

// longInterval makes only the first drain able to do the work, so a test that
// passes proves the drain happened on start rather than on a later tick.
const longInterval = time.Hour

// flakyRepo fails its first n queue passes and behaves normally after. A
// per-track download failure is reported as a count, not as an error from
// ProcessQueue, so this is the only way to reach the path where a whole pass
// fails.
type flakyRepo struct {
	*SQLiteQueueRepo
	failures int
}

func (f *flakyRepo) ResetPending(ctx context.Context, maxAttempts int) error {
	if f.failures > 0 {
		f.failures--
		return errors.New("database is locked")
	}

	return f.SQLiteQueueRepo.ResetPending(ctx, maxAttempts)
}

// firstCycle returns a channel closed after the worker's initial drain.
func firstCycle(t *testing.T, worker *Worker) chan DrainResult {
	t.Helper()

	cycles := make(chan DrainResult, 8)

	worker.onCycle = func(r DrainResult) { cycles <- r }

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go worker.Run(ctx, noOpReporter{})

	return cycles
}

func nextCycle(t *testing.T, cycles chan DrainResult) DrainResult {
	t.Helper()

	select {
	case r := <-cycles:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("the worker produced no drain")
		return DrainResult{}
	}
}

// TestWorkerDrainsOnStart: a worker that waited an interval before its first
// pass would leave a POST sitting pending for a full interval after every
// restart, which is the whole reason the worker exists.
func TestWorkerDrainsOnStart(t *testing.T) {
	svc, repo := newLoopService(t, &fakeDownloader{})
	mustEnqueue(t, repo, "aaaaaaaaaaa")
	mustEnqueue(t, repo, "bbbbbbbbbbb")

	worker := NewWorker(svc, WorkerConfig{Interval: longInterval})
	cycles := firstCycle(t, worker)

	got := nextCycle(t, cycles)
	if got.Success != 2 {
		t.Fatalf("first drain downloaded %d, want 2", got.Success)
	}

	entries, err := repo.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("%d rows left after the drain, want 0", len(entries))
	}
}

// A worker must stop when its context is cancelled, or every `serve` shutdown
// leaks a drain goroutine that keeps calling yt-dlp.
func TestWorkerStopsOnCancel(t *testing.T) {
	svc, _ := newLoopService(t, &fakeDownloader{})

	cycles := make(chan struct{}, 8)
	worker := NewWorker(svc, WorkerConfig{
		Interval: 10 * time.Millisecond,
		OnCycle:  func(DrainResult) { cycles <- struct{}{} },
	})

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})

	go func() {
		worker.Run(ctx, noOpReporter{})
		close(stopped)
	}()

	select {
	case <-cycles:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker never drained")
	}

	cancel()

	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker did not stop when its context was cancelled")
	}

	// Nothing may arrive after the pass that observes the cancellation.
	select {
	case <-cycles:
		t.Error("a drain ran after the worker was cancelled")
	case <-time.After(100 * time.Millisecond):
	}
}

// A per-track failure must not end the loop: a transient yt-dlp problem
// would otherwise silently stop every download until a restart.
func TestWorkerSurvivesAFailingCycle(t *testing.T) {
	const broken = "broken00001"

	dl := &fakeDownloader{behaviour: map[string]func(Config) error{
		broken: func(Config) error { return errors.New("yt-dlp exploded") },
	}}

	svc, repo := newLoopService(t, dl)
	mustEnqueue(t, repo, broken)

	worker := NewWorker(svc, WorkerConfig{Interval: 10 * time.Millisecond})
	cycles := firstCycle(t, worker)

	for i := range 3 {
		got := nextCycle(t, cycles)
		if got.Err != nil {
			t.Fatalf("pass %d reported a whole-cycle error: %v", i, got.Err)
		}
		if got.Failed != 1 {
			t.Errorf("pass %d failed = %d, want 1", i, got.Failed)
		}
	}

	// It must still be trying: the row is back to pending, not abandoned.
	entries, err := repo.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Error("the failing row was dropped rather than retried")
	}
}

// A failed pass must not end the loop. A per-track failure is only counted;
// this covers a pass that fails outright, such as the database being locked
// when the worker wakes up.
func TestWorkerSurvivesAFailedCycle(t *testing.T) {
	_, sqliteRepo := newLoopService(t, &fakeDownloader{})
	repo := &flakyRepo{SQLiteQueueRepo: sqliteRepo, failures: 1}

	svc := NewService(ServiceConfig{
		QueueRepo:   repo,
		StagingDirs: staticStaging{StagingDirPath: t.TempDir()},
		Downloader:  &fakeDownloader{},
	})
	mustEnqueue(t, sqliteRepo, "aaaaaaaaaaa")

	worker := NewWorker(svc, WorkerConfig{Interval: 10 * time.Millisecond})
	cycles := firstCycle(t, worker)

	if got := nextCycle(t, cycles); got.Err == nil {
		t.Fatal("the injected failure did not surface as a cycle error")
	}

	// The next pass must both happen and do the work that was waiting.
	got := nextCycle(t, cycles)
	if got.Err != nil {
		t.Fatalf("the worker stopped after one failed pass: %v", got.Err)
	}
	if got.Success != 1 {
		t.Errorf("the worker did not drain after recovering: %+v", got)
	}

}
