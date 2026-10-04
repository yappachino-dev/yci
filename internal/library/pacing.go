package library

import (
	"context"
	"math/rand/v2"
	"time"
)

// noPaceThreshold is the request count below which pacing is skipped.
//
// A flat playlist listing costs one request and a few kilobytes, so a handful
// of them finishes faster than the wait between the first two would have taken.
// The threshold is a constant rather than a config key because it is a
// judgement about when politeness stops paying, not something a user tunes.
const noPaceThreshold = 5

// Pace drops the wait when a run is small enough not to need it, and honours
// cancellation either way.
//
// remaining is the count of requests still to run, including this one, so a
// run that winds down stops waiting rather than sleeping once more on the way
// out. That also makes an explicit "is this the last one" flag redundant: the
// final request always has one remaining.
func Pace(ctx context.Context, min, max time.Duration, remaining int) error {
	if remaining < noPaceThreshold || (min <= 0 && max <= 0) {
		return ctx.Err()
	}

	wait := min
	if max > min {
		wait += time.Duration(rand.Int64N(int64(max-min) + 1))
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
