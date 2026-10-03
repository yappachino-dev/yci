package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yappachino-dev/yci/internal/fs"
	"github.com/yappachino-dev/yci/internal/probe"
)

// DurationBackfillResult reports what one duration pass did.
type DurationBackfillResult struct {
	// Scanned counts rows considered: every track with duration 0.
	Scanned int
	// Updated counts rows written.
	Updated int
	// Unchanged counts files that probed to zero seconds, which is almost
	// always a bad read rather than a real zero-length track.
	Unchanged int
	// Missing counts rows whose file is not on disk.
	Missing int
	// Failed counts files that could not be probed for another reason.
	Failed int
	// Unavailable reports that no prober was installed, so nothing was
	// scanned at all. This is a configuration fact, not a per-file error.
	Unavailable bool
}

// durationBackfillConcurrency bounds parallel probes.
//
// Probing is a subprocess per file, so this is bounded by process creation
// rather than by CPU. Eight is enough to hide ffprobe's startup cost on a
// library this size without making a laptop's disk seek.
const durationBackfillConcurrency = 8

// BackfillDuration probes the files of tracks whose duration is 0 and records
// what it finds.
//
// It only ever writes to rows that are already 0, so running it twice is
// harmless and it cannot overwrite a value that was correct. It is
// deliberately not part of sync or audit: those walk the whole library, and
// a subprocess per file would turn a fast scan into a slow one. Probing
// belongs here, on demand.
func (s *TrackService) BackfillDuration(
	ctx context.Context,
	prober probe.Prober,
	libraryDir string,
	dryRun bool,
) (DurationBackfillResult, error) {
	var result DurationBackfillResult

	tracks, err := s.repo.MissingDuration(ctx)
	if err != nil {
		return result, fmt.Errorf("list tracks missing duration: %w", err)
	}

	result.Scanned = len(tracks)
	if result.Scanned == 0 {
		return result, nil
	}

	// Fail before doing any work, so a missing ffprobe is reported as one
	// clear message rather than 234 identical per-file failures.
	if _, err := prober.Probe(ctx, firstExistingTrackPath(tracks, libraryDir)); err != nil && isProbeUnavailable(err) {
		result.Unavailable = true
		return result, nil
	}

	type probeResult struct {
		track   Track
		seconds int64
		skip    bool
		err     error
	}

	results := make(chan probeResult, len(tracks))
	sem := make(chan struct{}, durationBackfillConcurrency)
	var wg sync.WaitGroup

	for _, t := range tracks {
		wg.Add(1)
		go func(track Track) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- probeResult{track: track, err: ctx.Err()}
				return
			}

			seconds, skip, err := probeTrackDuration(ctx, prober, track, libraryDir)
			results <- probeResult{track: track, seconds: seconds, skip: skip, err: err}
		}(t)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		switch {
		// Both ways a context can end abort the whole pass, or a run
		// stopped by its deadline reports N corrupt files.
		case r.err != nil && isContextEnd(r.err):
			return result, r.err

		case r.err != nil && errors.Is(r.err, probe.ErrUnavailable):
			result.Unavailable = true
			continue

		case r.err != nil && r.err == errTrackFileMissing:
			result.Missing++
			continue

		case r.err != nil:
			result.Failed++

		case r.skip || r.seconds == 0:
			result.Unchanged++

		case dryRun:
			result.Updated++

		default:
			if err := s.repo.UpdateDuration(ctx, r.track.ID, r.seconds); err != nil {
				return result, fmt.Errorf("update duration for track %s: %w", r.track.ID, err)
			}
			result.Updated++
		}
	}

	return result, nil
}

var errTrackFileMissing = errors.New("track file is not on disk")

// isContextEnd reports whether an error is the context being cancelled or
// running out of time, either of which means the pass should stop rather
// than blame the file.
func isContextEnd(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func probeTrackDuration(ctx context.Context, prober probe.Prober, track Track, libraryDir string) (int64, bool, error) {
	// A zero-length row that genuinely is zero-length does not exist in
	// practice, so "already set" is signalled with skip rather than by
	// returning a sentinel duration.
	if track.DurationSec != 0 {
		return 0, true, nil
	}

	path := filepath.Join(libraryDir, filepath.FromSlash(string(track.RelativePath)))

	// Checked rather than inferred from the prober's text: ffprobe exits
	// non-zero for both an absent and an undecodable file. Resolution also
	// tolerates the other Unicode form, so byte drift is not "missing".
	resolved, statErr := fs.ResolveExisting(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return 0, false, errTrackFileMissing
		}

		return 0, false, fmt.Errorf("stat %q: %w", path, statErr)
	}

	path = resolved

	info, err := prober.Probe(ctx, path)
	if err != nil {
		// The file can still disappear between the stat and the probe.
		if isMissingFileError(err) {
			return 0, false, errTrackFileMissing
		}
		return 0, false, err
	}

	return int64(info.Duration.Seconds()), false, nil
}

// isMissingFileError reports whether a probe failed because the file is not
// on disk, as opposed to the file being unreadable. ffprobe exits non-zero
// with a diagnostic in both cases, so this matches on the message.
func isMissingFileError(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "does not exist")
}

// firstExistingTrackPath gives the availability pre-check a path that exists,
// so it distinguishes "ffprobe is missing" from "this one file is broken".
func firstExistingTrackPath(tracks []Track, libraryDir string) string {
	for _, t := range tracks {
		candidate := filepath.Join(libraryDir, filepath.FromSlash(string(t.RelativePath)))
		if resolved, err := fs.ResolveExisting(candidate); err == nil {
			return resolved
		}
	}

	if len(tracks) == 0 {
		return ""
	}
	return filepath.Join(libraryDir, filepath.FromSlash(string(tracks[0].RelativePath)))
}
