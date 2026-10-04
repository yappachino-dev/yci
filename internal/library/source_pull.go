package library

import (
	"context"
	"fmt"
	"time"
)

// SourceEnqueuer hands one watched playlist to the download queue and
// reports how many videos it added.
//
// A function rather than an interface because the implementation lives in
// internal/download, which does not import this package. Named for what it
// does, and passed in by the composition root, so this package stays free of
// that dependency the same way VideoChannelLookup does.
type SourceEnqueuer func(ctx context.Context, url string, channels []SourceChannel) (enqueued int, err error)

// PullParams configures one poll of every watched playlist.
type PullParams struct {
	Enqueue SourceEnqueuer
	// MinWait and MaxWait pace polls. A flat playlist listing is cheap, so
	// Pace skips the wait for a small set of sources.
	MinWait time.Duration
	MaxWait time.Duration
}

// PullResult is one watched playlist's outcome.
type PullResult struct {
	Name string
	// Enqueued counts videos the poll added to the download queue.
	Enqueued int
	// Err is the failure for this source alone. One playlist that has gone
	// private must not abandon the rest of the run.
	Err error
}

// Pull polls every watched playlist and enqueues what is new.
//
// It reports per source rather than returning on the first failure: a poll
// walks a list the user maintains, and stopping at one dead URL would make a
// single deletion look like the end of tracking.
func (s *SourceService) Pull(ctx context.Context, params PullParams) ([]PullResult, error) {
	sources, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	if params.Enqueue == nil {
		return nil, fmt.Errorf("pull watched playlists: no enqueue function configured")
	}

	results := make([]PullResult, 0, len(sources))
	for i, src := range sources {
		if err := ctx.Err(); err != nil {
			return results, err
		}

		enqueued, err := params.Enqueue(ctx, src.URL, src.Channels)
		results = append(results, PullResult{Name: src.Name, Enqueued: enqueued, Err: err})

		if err := Pace(ctx, params.MinWait, params.MaxWait, len(sources)-i); err != nil {
			return results, err
		}
	}

	return results, nil
}
