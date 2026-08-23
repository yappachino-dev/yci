package download

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type fakeDirReader struct{ files []string }

func (f *fakeDirReader) ReadDir(string) ([]string, error) { return f.files, nil }

type fakeTrackChecker struct{ existing map[string]bool }

func (f *fakeTrackChecker) ExistsBySourceID(_ context.Context, id string) (bool, error) {
	return f.existing[id], nil
}

type fakeQueueRepo struct {
	present         map[string]bool
	enqueued        []string
	failEnq         error
	retried         []int
	failedIDs       map[int]bool
	hardRetried     int
	hardFailedCount int64
}

func (f *fakeQueueRepo) All(context.Context) ([]QueueEntry, error) { return nil, nil }

func (f *fakeQueueRepo) Enqueue(_ context.Context, sourceID string) (bool, error) {
	if f.failEnq != nil {
		return false, f.failEnq
	}

	f.enqueued = append(f.enqueued, sourceID)
	return !f.present[sourceID], nil
}

func (f *fakeQueueRepo) QueueLength(context.Context) (int, error) { return 0, nil }

func (f *fakeQueueRepo) ResetPending(context.Context) error { return nil }

func (f *fakeQueueRepo) Next(context.Context) (QueueEntry, bool, error) {
	return QueueEntry{}, false, nil
}

func (f *fakeQueueRepo) Start(context.Context, int) error    { return nil }
func (f *fakeQueueRepo) Complete(context.Context, int) error { return nil }
func (f *fakeQueueRepo) Fail(context.Context, int, error) error {
	return nil
}

func (f *fakeQueueRepo) RetryByID(_ context.Context, id int) (bool, error) {
	f.retried = append(f.retried, id)
	return f.failedIDs[id], nil
}

func (f *fakeQueueRepo) RetryHardFailed(context.Context) (int64, error) {
	f.hardRetried++
	return int64(f.hardFailedCount), nil
}

type fakeTransactor struct{}

func (fakeTransactor) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakeStagingConfig struct{ dir string }

func (f *fakeStagingConfig) StagingDir() string { return f.dir }

type captureReporter struct {
	skipped map[string]string
}

func newCaptureReporter() *captureReporter {
	return &captureReporter{skipped: make(map[string]string)}
}

func (r *captureReporter) OnHybridURL(string, bool) {}
func (r *captureReporter) OnPlaylistURL(string)     {}

func (r *captureReporter) OnEnqueueSkipped(id, reason string) {
	r.skipped[id] = reason
}

func newTestService(repo *fakeQueueRepo, checker *fakeTrackChecker, dirs *fakeDirReader) *Service {
	return NewService(ServiceConfig{
		QueueRepo:    repo,
		TrackChecker: checker,
		DirReader:    dirs,
		Transactor:   fakeTransactor{},
		Config:       &fakeStagingConfig{dir: "/tmp/staging"},
	})
}

const testVideoID = "dQw4w9WgXcQ"

func watchURL(id string) string { return "https://www.youtube.com/watch?v=" + id }

func TestServiceEnqueueCountsUnrecognizedURLsAsFailures(t *testing.T) {
	for _, u := range []string{
		"https://evil.example.com/watch?v=" + testVideoID,
		"not a url at all",
	} {
		t.Run(u, func(t *testing.T) {
			repo := &fakeQueueRepo{}
			reporter := newCaptureReporter()

			result, err := newTestService(repo, &fakeTrackChecker{}, &fakeDirReader{}).
				Enqueue(context.Background(), EnqueueParams{URLs: []string{u}, Reporter: reporter})
			if err != nil {
				t.Fatalf("Enqueue returned error: %v", err)
			}

			if result.Failed != 1 || result.Enqueued != 0 || result.Skipped != 0 {
				t.Errorf("result = %+v, want Failed=1, Enqueued=0, Skipped=0", result)
			}
			if len(repo.enqueued) != 0 {
				t.Errorf("repo enqueued %v, want nothing", repo.enqueued)
			}
			if reason := reporter.skipped[u]; reason != SkipReasonUnrecognized {
				t.Errorf("skip reason = %q, want %q", reason, SkipReasonUnrecognized)
			}
		})
	}
}

func TestServiceEnqueueBareVideoID(t *testing.T) {
	repo := &fakeQueueRepo{}

	result, err := newTestService(repo, &fakeTrackChecker{}, &fakeDirReader{}).
		Enqueue(context.Background(), EnqueueParams{URLs: []string{testVideoID}})
	if err != nil {
		t.Fatalf("Enqueue returned error: %v", err)
	}

	if result.Enqueued != 1 {
		t.Errorf("Enqueued = %d, want 1", result.Enqueued)
	}
	if !slices.Equal(repo.enqueued, []string{testVideoID}) {
		t.Errorf("repo enqueued %v, want [%s]", repo.enqueued, testVideoID)
	}
}

// The skip reasons double as a regression test for the result accounting:
// scan-phase skips must survive the enqueue transaction instead of being
// overwritten by the already-in-queue count.
func TestServiceEnqueueSkipReasons(t *testing.T) {
	repo := &fakeQueueRepo{present: map[string]bool{"inqueue": true}}
	checker := &fakeTrackChecker{existing: map[string]bool{"indb": true}}
	dirs := &fakeDirReader{files: []string{"staged.opus"}}
	reporter := newCaptureReporter()

	urls := []string{
		watchURL("staged"),
		watchURL("indb"),
		watchURL("dupvid"),
		watchURL("dupvid"),
		watchURL("inqueue"),
	}

	result, err := newTestService(repo, checker, dirs).
		Enqueue(context.Background(), EnqueueParams{URLs: urls, Reporter: reporter})
	if err != nil {
		t.Fatalf("Enqueue returned error: %v", err)
	}

	wantSkips := map[string]string{
		"staged":  SkipReasonStaged,
		"indb":    SkipReasonExists,
		"dupvid":  SkipReasonDuplicate,
		"inqueue": SkipReasonAlreadyInQueue,
	}
	for id, want := range wantSkips {
		if got := reporter.skipped[id]; got != want {
			t.Errorf("skip reason for %s = %q, want %q", id, got, want)
		}
	}

	if result.Enqueued != 1 || result.Skipped != 4 || result.Failed != 0 {
		t.Errorf("result = %+v, want Enqueued=1, Skipped=4, Failed=0", result)
	}
	// Already-queued entries still reach the repository; INSERT OR IGNORE
	// semantics make Enqueue report false for them.
	if !slices.Equal(repo.enqueued, []string{"dupvid", "inqueue"}) {
		t.Errorf("repo enqueued %v, want [dupvid inqueue]", repo.enqueued)
	}
}

func TestServiceRetryFailed(t *testing.T) {
	t.Run("without IDs resets hard-failed entries", func(t *testing.T) {
		repo := &fakeQueueRepo{hardFailedCount: 3}

		result, err := newTestService(repo, &fakeTrackChecker{}, &fakeDirReader{}).
			RetryFailed(context.Background(), nil)
		if err != nil {
			t.Fatalf("RetryFailed returned error: %v", err)
		}
		if result.Retried != 3 || repo.hardRetried != 1 {
			t.Errorf("result = %+v, hardRetried = %d, want Retried=3 via one hard reset", result, repo.hardRetried)
		}
	})

	t.Run("with IDs targets individual entries and skips unknown ones", func(t *testing.T) {
		repo := &fakeQueueRepo{failedIDs: map[int]bool{7: true}}

		result, err := newTestService(repo, &fakeTrackChecker{}, &fakeDirReader{}).
			RetryFailed(context.Background(), []int{7, 9})
		if err != nil {
			t.Fatalf("RetryFailed returned error: %v", err)
		}
		if result.Retried != 1 {
			t.Errorf("Retried = %d, want 1 (ID 7 only)", result.Retried)
		}
		if !slices.Equal(repo.retried, []int{7, 9}) {
			t.Errorf("repo retried %v, want [7 9]", repo.retried)
		}
	})
}

func TestServiceEnqueueTransactionFailureCountsAllFailed(t *testing.T) {
	repo := &fakeQueueRepo{failEnq: errors.New("disk on fire")}

	result, err := newTestService(repo, &fakeTrackChecker{}, &fakeDirReader{}).
		Enqueue(context.Background(), EnqueueParams{URLs: []string{watchURL(testVideoID)}})
	if err == nil {
		t.Fatal("Enqueue expected error from failing repository")
	}

	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
}
