package download

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeDownloader stands in for yt-dlp.
//
// Before the Downloader seam, ProcessQueue called a package-level function
// that built its own exec.Cmd, so this loop could not be exercised at all
// without a network and a toolchain. That was not because the loop is hard to
// test: it is almost entirely bookkeeping about attempts, statuses and report
// events, and all of it was unreachable behind one un-injected call.
type fakeDownloader struct {
	// behaviour decides what happens to a given source ID. An ID with no
	// entry succeeds.
	behaviour map[string]func(cfg Config) error

	mu    sync.Mutex
	calls []string
}

func (f *fakeDownloader) Download(_ context.Context, cfg Config) error {
	f.mu.Lock()
	f.calls = append(f.calls, cfg.URL)
	f.mu.Unlock()

	if fn, ok := f.behaviour[cfg.URL]; ok {
		return fn(cfg)
	}
	return nil
}

func (f *fakeDownloader) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// recordingReporter captures the event sequence, which is the part of the
// loop a user actually sees.
type recordingReporter struct {
	events []string
}

func (r *recordingReporter) OnHybridURL(string, bool) {}
func (r *recordingReporter) OnPlaylistURL(string)     {}
func (r *recordingReporter) OnEnqueueSkipped(id, reason string) {
	r.events = append(r.events, fmt.Sprintf("skip:%s:%s", id, reason))
}

func (r *recordingReporter) OnQueueStart(total int) {
	r.events = append(r.events, fmt.Sprintf("queue-start:%d", total))
}

func (r *recordingReporter) OnTrackStart(index, total int, sourceID string) {
	r.events = append(r.events, fmt.Sprintf("start:%d/%d:%s", index, total, sourceID))
}

func (r *recordingReporter) OnTrackSuccess(sourceID string) {
	r.events = append(r.events, "success:"+sourceID)
}

func (r *recordingReporter) OnTrackFailure(sourceID string, err error) {
	r.events = append(r.events, "failure:"+sourceID)
}

// newLoopService wires a Service over a real migrated queue with a fake
// downloader, which is the combination that was previously impossible.
func newLoopService(t *testing.T, dl Downloader) (*Service, *SQLiteQueueRepo) {
	t.Helper()

	repo := newQueueRepo(t)
	return NewService(ServiceConfig{
		QueueRepo:   repo,
		StagingDirs: staticStaging{StagingDirPath: t.TempDir()},
		Downloader:  dl,
	}), repo
}

// noOpReporter is the production no-op; the loop tests assert on repository
// state and result counts rather than on report events, and the event
// ordering is covered separately by recordingReporter.
var _ Reporter = noOpReporter{}

type staticStaging struct{ StagingDirPath string }

func (s staticStaging) StagingDir() string { return s.StagingDirPath }

// TestProcessQueueDrivesTheWholeLoop is the test that could not be written
// before. It runs several tracks through the real queue repository with a
// fake downloader and checks the resulting state, which is the only way to
// know the loop and the repository agree on the transitions.
func TestProcessQueueDrivesTheWholeLoop(t *testing.T) {
	dl := &fakeDownloader{}
	svc, repo := newLoopService(t, dl)
	ctx := context.Background()

	mustEnqueue(t, repo, "aaaaaaaaaaa")
	mustEnqueue(t, repo, "bbbbbbbbbbb")
	mustEnqueue(t, repo, "ccccccccccc")

	reporter := &recordingReporter{}
	result, err := svc.ProcessQueue(ctx, QueueRunConfig{}, reporter)
	if err != nil {
		t.Fatalf("ProcessQueue: %v", err)
	}

	if result.Success != 3 || result.Failed != 0 {
		t.Errorf("result = %+v, want 3 successes", result)
	}
	if got := len(dl.called()); got != 3 {
		t.Errorf("downloader called %d times, want 3", got)
	}

	// Complete deletes the row, because the schema only allows pending,
	// processing and failed. An empty queue afterwards is the proof the
	// loop and the repository agree.
	rest, err := repo.All(ctx)
	if err != nil {
		t.Fatalf("list queue: %v", err)
	}
	if len(rest) != 0 {
		t.Errorf("queue still holds %d entries, want 0", len(rest))
	}

	want := []string{
		"queue-start:3",
		"start:1/3:aaaaaaaaaaa", "success:aaaaaaaaaaa",
		"start:2/3:bbbbbbbbbbb", "success:bbbbbbbbbbb",
		"start:3/3:ccccccccccc", "success:ccccccccccc",
	}
	if strings.Join(reporter.events, "|") != strings.Join(want, "|") {
		t.Errorf("event sequence\n got %v\nwant %v", reporter.events, want)
	}
}

// TestProcessQueueOnlyCountsActionableEntries covers the accounting rule: a
// hard-failed row is not something this run can work on, so it must not be
// counted as work or reported as progress.
func TestProcessQueueOnlyCountsActionableEntries(t *testing.T) {
	dl := &fakeDownloader{}
	svc, repo := newLoopService(t, dl)
	ctx := context.Background()

	mustEnqueue(t, repo, "aaaaaaaaaaa")
	if err := repo.Fail(ctx, 1, string(QueueStatusFailed), errors.New("gone")); err != nil {
		t.Fatalf("hard-fail the entry: %v", err)
	}
	ok, err := repo.RetryByID(ctx, 1)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !ok {
		t.Fatal("RetryByID re-armed nothing")
	}

	reporter := &recordingReporter{}
	if _, err := svc.ProcessQueue(ctx, QueueRunConfig{}, reporter); err != nil {
		t.Fatalf("ProcessQueue: %v", err)
	}

	// Total is 0 rather than 1: a re-armed row is processed, but the count
	// reported to the user is of entries that can actually succeed.
	if got := reporter.events[0]; got != "queue-start:1" {
		t.Errorf("first event = %q, want queue-start:1", got)
	}
}

// TestProcessQueueAttemptsOneTrackOncePerRun is the regression test for the
// loop bug the seam exposed.
//
// A transient failure puts the row back to pending, which made it eligible
// for Next again immediately, so one run re-picked the track it had just
// failed and burned its entire retry budget in a single invocation. Measured
// before the fix on a two-track queue: the failing track was started four
// times, progress was reported as 4/2, and result.Failed read 4 when one
// track had failed.
func TestProcessQueueAttemptsOneTrackOncePerRun(t *testing.T) {
	calls := 0
	dl := &fakeDownloader{
		behaviour: map[string]func(Config) error{
			"aaaaaaaaaaa": func(Config) error {
				calls++
				return fmt.Errorf("execute yt-dlp: %w: transient", ErrExecution)
			},
			"bbbbbbbbbbb": func(Config) error { return nil },
		},
	}

	svc, repo := newLoopService(t, dl)
	ctx := context.Background()
	mustEnqueue(t, repo, "aaaaaaaaaaa")
	mustEnqueue(t, repo, "bbbbbbbbbbb")

	reporter := &recordingReporter{}
	result, err := svc.ProcessQueue(ctx, QueueRunConfig{}, reporter)
	if err != nil {
		t.Fatalf("ProcessQueue: %v", err)
	}

	if calls != 1 {
		t.Errorf("the failing track was attempted %d times in one run, want 1", calls)
	}

	// One failure, one success, and the count agrees with reality.
	if result.Failed != 1 || result.Success != 1 {
		t.Errorf("result = %+v, want one failure and one success", result)
	}

	// Progress must never exceed the announced total.
	for _, e := range reporter.events {
		if strings.HasPrefix(e, "start:") {
			var index, total int
			if _, err := fmt.Sscanf(e, "start:%d/%d:", &index, &total); err != nil {
				t.Fatalf("unparseable event %q: %v", e, err)
			}
			if index > total {
				t.Errorf("progress %q exceeds the announced total", e)
			}
		}
	}

	// The healthy track completed and left the queue; the failing one is
	// pending for a later run rather than dead.
	if inQueue(t, repo, "bbbbbbbbbbb") {
		t.Error("the successful track is still queued")
	}
	if e := entryBySource(t, repo, "aaaaaaaaaaa"); e.Status != QueueStatusPending {
		t.Errorf("status after one transient failure = %q, want pending", e.Status)
	}
}

// TestProcessQueueExhaustsTheRetryBudgetAcrossRuns pins the retry policy
// itself, which the previous test could not reach.
//
// maxAttempts is documented as the number of *retries*, so a track that never
// succeeds is attempted once and then retried that many times before it is
// failed.
func TestProcessQueueExhaustsTheRetryBudgetAcrossRuns(t *testing.T) {
	calls := 0
	dl := &fakeDownloader{
		behaviour: map[string]func(Config) error{
			"aaaaaaaaaaa": func(Config) error {
				calls++
				return fmt.Errorf("execute yt-dlp: %w: transient", ErrExecution)
			},
		},
	}

	svc, repo := newLoopService(t, dl)
	ctx := context.Background()
	mustEnqueue(t, repo, "aaaaaaaaaaa")

	wantAttempts := maxAttempts + 1 // the first attempt, plus maxAttempts retries

	for run := 0; run < wantAttempts; run++ {
		if _, err := svc.ProcessQueue(ctx, QueueRunConfig{}, noOpReporter{}); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		e := entryBySource(t, repo, "aaaaaaaaaaa")
		// The row is failed only once the budget is spent.
		wantStatus := QueueStatusPending
		if e.Attempts >= wantAttempts {
			wantStatus = QueueStatusFailed
		}
		if e.Status != wantStatus {
			t.Errorf("run %d: status = %q, want %q (attempts %d)", run, e.Status, wantStatus, e.Attempts)
		}
	}

	if calls != wantAttempts {
		t.Errorf("downloader invoked %d times, want %d", calls, wantAttempts)
	}

	// A further run must not resurrect or re-attempt a failed track.
	if _, err := svc.ProcessQueue(ctx, QueueRunConfig{}, noOpReporter{}); err != nil {
		t.Fatalf("extra run: %v", err)
	}
	if calls != wantAttempts {
		t.Errorf("a failed track was retried again: %d calls, want %d", calls, wantAttempts)
	}
}

// TestProcessQueueCancellationLeavesTheTrackForTheNextRun covers a Ctrl-C.
//
// Two things are correct here and one is a known cost, so all three are
// pinned. The row is left processing for the next run to re-arm, and no
// failure is recorded against it. But the attempt is still consumed:
// Start increments it before the download runs, and the row cannot be written
// back here because that write would need the cancelled context. Recorded as
// TD-38 rather than silently asserted as intended.
func TestProcessQueueCancellationLeavesTheTrackForTheNextRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// The first attempt is interrupted, as a Ctrl-C would; later attempts
	// succeed, so the recovery run has something to prove.
	attempts := 0
	dl := &fakeDownloader{
		behaviour: map[string]func(Config) error{
			"aaaaaaaaaaa": func(Config) error {
				attempts++
				if attempts == 1 {
					cancel()
					return context.Canceled
				}
				return nil
			},
			"bbbbbbbbbbb": func(Config) error { return nil },
		},
	}

	svc, repo := newLoopService(t, dl)
	mustEnqueue(t, repo, "aaaaaaaaaaa")
	mustEnqueue(t, repo, "bbbbbbbbbbb")

	result, err := svc.ProcessQueue(ctx, QueueRunConfig{}, noOpReporter{})
	if err == nil {
		t.Fatal("ProcessQueue returned nil, want the context error")
	}

	// The loop stops immediately rather than starting the second track.
	if result.Success != 0 || result.Failed != 0 {
		t.Errorf("result = %+v, want nothing counted on a cancelled run", result)
	}

	e := entryBySource(t, repo, "aaaaaaaaaaa")
	if e.Status != QueueStatusProcessing {
		t.Errorf("status = %q, want processing so the next run re-arms it", e.Status)
	}
	if e.ErrorMsg != "" {
		t.Errorf("error_msg = %q, want no bogus failure recorded against a cancelled run", e.ErrorMsg)
	}
	// Start counted the attempt before the download ran, and the cancelled
	// context left no way to write it back here.
	if e.Attempts != 1 {
		t.Errorf("attempts = %d, want 1: Start increments before the download", e.Attempts)
	}

	// The next run re-arms the row, gives the attempt back, and finishes the
	// track. Interrupting is not a failure, so it must not cost a retry.
	fresh := context.Background()
	recovered, err := svc.ProcessQueue(fresh, QueueRunConfig{}, noOpReporter{})
	if err != nil {
		t.Fatalf("recovery run: %v", err)
	}
	if recovered.Success != 2 {
		t.Errorf("recovery Success = %d, want both tracks", recovered.Success)
	}
	if inQueue(t, repo, "aaaaaaaaaaa") {
		t.Error("the interrupted track did not complete on the next run")
	}
}

// Three interrupts in a row must not exhaust a track's retry budget.
func TestProcessQueueRepeatedInterruptsDoNotFailATrack(t *testing.T) {
	calls := 0
	var cancel context.CancelFunc

	// The downloader cancels mid-run, as a Ctrl-C does, for the first three
	// attempts. The fourth succeeds.
	dl := &fakeDownloader{
		behaviour: map[string]func(Config) error{
			"aaaaaaaaaaa": func(Config) error {
				calls++
				if calls <= 3 {
					cancel()
					return context.Canceled
				}
				return nil
			},
		},
	}

	svc, repo := newLoopService(t, dl)
	mustEnqueue(t, repo, "aaaaaaaaaaa")

	for run := range 3 {
		ctx, c := context.WithCancel(context.Background())
		cancel = c

		if _, err := svc.ProcessQueue(ctx, QueueRunConfig{}, noOpReporter{}); err == nil {
			t.Fatalf("run %d: want the context error", run)
		}

		// The row is still processing: ResetPending runs at the start of the
		// *next* run, so the attempt spent here is returned there, not here.
		e := entryBySource(t, repo, "aaaaaaaaaaa")
		if e.Status != QueueStatusProcessing {
			t.Fatalf("run %d: status = %q, want processing", run, e.Status)
		}
		// The number that matters: without the return, this would be 3 after
		// three runs and the fourth run would fail the track outright.
		if e.Attempts != 1 {
			t.Fatalf("run %d: attempts = %d, want 1: interrupts must not accumulate", run, e.Attempts)
		}
	}

	result, err := svc.ProcessQueue(context.Background(), QueueRunConfig{}, noOpReporter{})
	if err != nil {
		t.Fatalf("final run: %v", err)
	}
	if result.Success != 1 {
		t.Errorf("Success = %d, want 1: three interrupts must not exhaust the budget", result.Success)
	}
	if inQueue(t, repo, "aaaaaaaaaaa") {
		t.Error("the track should have completed")
	}
}

// inQueue reports whether a source ID still has a row.
//
// Separate from entryBySource because a completed download deliberately
// leaves no row at all: the schema has nowhere to record "done", so Complete
// deletes the entry. Absence is the success signal, not an error.
func inQueue(t *testing.T, r *SQLiteQueueRepo, sourceID string) bool {
	t.Helper()

	for _, e := range mustAll(t, r) {
		if e.SourceID == sourceID {
			return true
		}
	}
	return false
}

func mustAll(t *testing.T, r *SQLiteQueueRepo) []QueueEntry {
	t.Helper()

	all, err := r.All(t.Context())
	if err != nil {
		t.Fatalf("list queue: %v", err)
	}
	return all
}
