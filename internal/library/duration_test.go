package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yappachino-dev/yci/internal/probe"
)

// stubProber returns a canned duration, an error, or a delay per path.
//
// Probe is called from the backfill's worker goroutines, so the recorded
// paths are guarded. Leaving that out raced under -race while passing
// without it, which is the worst way for a test to behave.
type stubProber struct {
	mu        sync.Mutex
	durations map[string]time.Duration
	errs      map[string]error
	delay     time.Duration

	// probed records every path the backfill asked about.
	probed []string
}

func (p *stubProber) Probe(ctx context.Context, path string) (probe.Info, error) {
	p.mu.Lock()
	p.probed = append(p.probed, path)
	p.mu.Unlock()

	if p.delay > 0 {
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return probe.Info{}, ctx.Err()
		}
	}

	// A real prober fails when the file is gone, so the stub has to as
	// well; otherwise a deleted file would report a duration.
	if _, err := os.Stat(path); err != nil {
		return probe.Info{}, errors.New("ffprobe: /x: No such file or directory")
	}

	if err, ok := p.errs[path]; ok {
		return probe.Info{}, err
	}

	return probe.Info{Duration: p.durations[path]}, nil
}

// durationBackfillEnv builds a library with tracks whose duration is unset,
// which is exactly the state track probe-duration exists to repair.
func durationBackfillEnv(t *testing.T, titles ...string) (*libraryEnv, *stubProber, []string) {
	t.Helper()

	env := newLibraryEnv(t)
	prober := &stubProber{durations: map[string]time.Duration{}, errs: map[string]error{}}

	paths := make([]string, 0, len(titles))
	for i, title := range titles {
		rel := filepath.ToSlash(filepath.Join("A", string(rune('a'+i))+".mp3"))
		env.mustPutTrack(t, string(rune('a'+i)), title, rel)

		abs := filepath.Join(env.paths.LibraryDir(), rel)
		prober.durations[abs] = time.Duration(100+i) * time.Second
		paths = append(paths, abs)
	}

	return env, prober, paths
}

func TestBackfillDurationRecordsProbedValues(t *testing.T) {
	env, prober, _ := durationBackfillEnv(t, "One", "Two", "Three")

	got, err := env.tracks.BackfillDuration(t.Context(), prober, env.paths.LibraryDir(), false)
	if err != nil {
		t.Fatalf("BackfillDuration: %v", err)
	}

	if got.Scanned != 3 || got.Updated != 3 || got.Failed != 0 {
		t.Errorf("result = %+v, want Scanned 3, Updated 3, Failed 0", got)
	}

	rows, err := env.repos.tracks.ByPathPattern(t.Context(), "%")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.DurationSec == 0 {
			t.Errorf("track %s still has duration 0", row.ID)
		}
	}
}

func TestBackfillDurationIsIdempotentAndOnlyTouchesZeroRows(t *testing.T) {
	env, prober, _ := durationBackfillEnv(t, "One", "Two")

	if _, err := env.tracks.BackfillDuration(t.Context(), prober, env.paths.LibraryDir(), false); err != nil {
		t.Fatal(err)
	}

	// Second pass has nothing to do, and must not re-probe or rewrite.
	before := prober.probeCount()
	got, err := env.tracks.BackfillDuration(t.Context(), prober, env.paths.LibraryDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scanned != 0 {
		t.Errorf("second pass scanned %d, want 0", got.Scanned)
	}
	if prober.probeCount() != before {
		t.Errorf("second pass probed %d extra file(s)", prober.probeCount()-before)
	}
}

func TestBackfillDurationDryRunWritesNothing(t *testing.T) {
	env, prober, _ := durationBackfillEnv(t, "One", "Two")

	got, err := env.tracks.BackfillDuration(t.Context(), prober, env.paths.LibraryDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Updated != 2 {
		t.Errorf("dry run reported Updated = %d, want 2", got.Updated)
	}

	rows, err := env.repos.tracks.ByPathPattern(t.Context(), "%")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.DurationSec != 0 {
			t.Errorf("dry run wrote duration %d to track %s", row.DurationSec, row.ID)
		}
	}
}

// TestBackfillDurationDeadlineAbortsInsteadOfCountingFailures is the
// regression test for a run stopped by a deadline being reported as one
// failure per file.
//
// The abort test keyed only on context.Canceled. A deadline produces
// context.DeadlineExceeded, which is not Canceled, so every in-flight probe
// fell through to the generic failure branch and the command claimed that
// every track was corrupt.
func TestBackfillDurationDeadlineAbortsInsteadOfCountingFailures(t *testing.T) {
	env, prober, _ := durationBackfillEnv(t, "One", "Two", "Three")
	prober.delay = 500 * time.Millisecond

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()

	got, err := env.tracks.BackfillDuration(ctx, prober, env.paths.LibraryDir(), false)
	if err == nil {
		t.Fatal("expected an error when the context deadline expires")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want it to wrap context.DeadlineExceeded", err)
	}
	if got.Failed != 0 {
		t.Errorf("Failed = %d; an expired deadline is not a per-file failure", got.Failed)
	}
}

func TestBackfillDurationCancelAborts(t *testing.T) {
	env, prober, _ := durationBackfillEnv(t, "One", "Two")
	prober.delay = 500 * time.Millisecond

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	got, err := env.tracks.BackfillDuration(ctx, prober, env.paths.LibraryDir(), false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got.Failed != 0 {
		t.Errorf("Failed = %d; a cancellation is not a per-file failure", got.Failed)
	}
}

func TestBackfillDurationReportsMissingAndUnreadableSeparately(t *testing.T) {
	env, prober, paths := durationBackfillEnv(t, "Present", "Absent")

	// Remove one file entirely; the other probes to an unreadable error.
	if err := os.Remove(paths[1]); err != nil {
		t.Fatal(err)
	}
	prober.errs[paths[0]] = errors.New("ffprobe: Invalid data found when processing input")

	got, err := env.tracks.BackfillDuration(t.Context(), prober, env.paths.LibraryDir(), false)
	if err != nil {
		t.Fatalf("BackfillDuration: %v", err)
	}
	if got.Missing != 1 {
		t.Errorf("Missing = %d, want 1", got.Missing)
	}
	if got.Failed != 1 {
		t.Errorf("Failed = %d, want 1", got.Failed)
	}
	if got.Updated != 0 {
		t.Errorf("Updated = %d, want 0", got.Updated)
	}
}

func TestBackfillDurationWithoutProberIsOneClearMessage(t *testing.T) {
	env, prober, _ := durationBackfillEnv(t, "One", "Two", "Three")
	_ = prober

	missingProber := &stubProber{
		durations: map[string]time.Duration{},
		errs:      map[string]error{"": probe.ErrUnavailable},
	}
	for _, p := range proberPaths(env) {
		missingProber.errs[p] = probe.ErrUnavailable
	}

	got, err := env.tracks.BackfillDuration(
		t.Context(),
		missingProber,
		env.paths.LibraryDir(),
		false,
	)
	if err != nil {
		t.Fatalf("BackfillDuration: %v", err)
	}
	if !got.Unavailable {
		t.Error("Unavailable = false, want true when no prober is installed")
	}
	if got.Failed != 0 {
		t.Errorf("Failed = %d; a missing ffprobe is not N per-file failures", got.Failed)
	}
}

// probeCount reports how many paths have been probed, safely.
func (p *stubProber) probeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.probed)
}

// proberPaths lists every file the environment put on disk, so a stub prober
// can be told to report the tool as missing.
func proberPaths(env *libraryEnv) []string {
	var paths []string
	_ = filepath.WalkDir(env.paths.LibraryDir(), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})

	return paths
}
