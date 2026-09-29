// Package probe reads media properties that tag libraries do not expose.
//
// Tag readers answer "what does the file call itself". They do not answer
// "how long is it, what codec is it, what sample rate". This package owns
// that second question, behind an interface, so the mechanism is swappable:
// ffprobe today, a pure-Go reader tomorrow.
package probe

import (
	"context"
	"errors"
	"time"
)

// ErrUnavailable reports that no probe mechanism is installed. Callers treat
// it as "metadata is absent", never as "the file is broken": a missing probe
// must not be able to fail an import or mark a track missing.
var ErrUnavailable = errors.New("no media prober is available")

// Info is what a probe can tell us about a media file.
//
// Fields beyond Duration are not needed today. They are here because a
// player-compatibility question (lossless only, drop anything below 96 kHz)
// needs codec and sample rate, and retrofitting a second probe pass later
// would mean re-reading the whole library.
type Info struct {
	Duration   time.Duration
	Codec      string
	Bitrate    int64
	SampleRate int
	Channels   int
}

// Prober reports media properties for one file.
type Prober interface {
	// Probe returns ErrUnavailable-derived errors when no mechanism is
	// installed, so a caller can degrade instead of failing.
	Probe(ctx context.Context, path string) (Info, error)
}
