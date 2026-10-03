package library

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// TrackLister is the slice of TrackService the backfill needs: enough to find
// a video an artist published.
type TrackLister interface {
	ByArtistID(ctx context.Context, artistID ArtistID) ([]Track, error)
}

// VideoChannelLookup reports the channel behind a video. It is a function
// rather than an interface because the only implementation is yt-dlp, and a
// test supplies a closure.
//
// Strings rather than ChannelID and ChannelName: the implementation lives in
// internal/download, which does not import this package, and naming a type
// here would force that dependency or duplicate the type across both.
type VideoChannelLookup func(ctx context.Context, videoID string) (string, string, error)

// BackfillOutcome is what happened to one channel link.
type BackfillOutcome string

const (
	// BackfillAttached means the identity was recovered and stored.
	BackfillAttached BackfillOutcome = "attached"
	// BackfillAlreadyKnown means the link already carried an ID.
	BackfillAlreadyKnown BackfillOutcome = "already-known"
	// BackfillRenamed means the stored name had drifted and was corrected.
	BackfillRenamed BackfillOutcome = "renamed"
	// BackfillUnresolved means nothing could be determined without guessing.
	BackfillUnresolved BackfillOutcome = "unresolved"
	// BackfillFailed means the lookup itself failed.
	BackfillFailed BackfillOutcome = "failed"
)

// BackfillResult is one link's outcome, with the evidence behind it.
type BackfillResult struct {
	ArtistID ArtistID
	Stored   ChannelName
	Found    ChannelName
	Channel  ChannelID
	Outcome  BackfillOutcome
	Reason   string
}

// BackfillReporter receives progress while the backfill runs.
//
// Not optional in spirit: the run makes a network request per artist and takes
// seconds each, so without progress a large library is an unresponsive terminal
// for minutes, which is indistinguishable from a hang.
type BackfillReporter interface {
	OnBackfillStart(artists int, links int)
	OnArtistStart(current, artists int, artistID ArtistID)
	OnArtistResolved(artistID ArtistID, id ChannelID, currentName ChannelName)
	OnArtistUnresolved(artistID ArtistID, reason string)
}

// BackfillParams configures one backfill run.
type BackfillParams struct {
	// DryRun reports what would change without writing.
	DryRun bool
	// MinWait and MaxWait pace requests to the source. Each lookup is a
	// separate process, so yt-dlp's own --sleep-interval cannot apply
	// between them and the pacing has to happen here.
	MinWait time.Duration
	MaxWait time.Duration
	// Cookies is unused by this layer; the resolver carries it.
	Reporter BackfillReporter
}

// BackfillChannels recovers channel identities for links created before yci
// recorded them.
//
// Deliberately conservative: an ID is attached only when the evidence is
// unambiguous. An artist with a single channel link is unambiguous even if the
// name has drifted, because a rename is exactly what this repairs. An artist
// with several links is not, and guessing would file downloads under the wrong
// artist.
func (s *ChannelService) BackfillChannels(ctx context.Context, params BackfillParams) ([]BackfillResult, error) {
	links, err := s.repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}

	// An artist with more than one link cannot be resolved by elimination.
	linkCounts := make(map[ArtistID]int, len(links))
	for _, l := range links {
		linkCounts[l.ArtistID]++
	}

	// Grouped so the run iterates artists, which is the unit of work: one
	// lookup each. Iterating links and caching would have made pacing and
	// progress both wrong.
	order := make([]ArtistID, 0, len(linkCounts))
	grouped := make(map[ArtistID][]Channel, len(linkCounts))
	for _, l := range links {
		if _, seen := grouped[l.ArtistID]; !seen {
			order = append(order, l.ArtistID)
		}
		grouped[l.ArtistID] = append(grouped[l.ArtistID], l)
	}

	reporter := params.Reporter
	if reporter == nil {
		reporter = noOpBackfillReporter{}
	}

	// Only artists with something to learn are work; the rest need no
	// request and must not be paced for.
	var todo []ArtistID
	for _, id := range order {
		for _, l := range grouped[id] {
			if l.ChannelID == "" {
				todo = append(todo, id)
				break
			}
		}
	}

	reporter.OnBackfillStart(len(todo), len(links))

	results := make([]BackfillResult, 0, len(links))

	for i, artistID := range order {
		if err := ctx.Err(); err != nil {
			return results, err
		}

		known := len(grouped[artistID]) > 1

		// Already resolved? No request, and the link counts as known.
		for _, l := range grouped[artistID] {
			if l.ChannelID != "" {
				results = append(results, BackfillResult{
					ArtistID: l.ArtistID, Stored: l.Name, Channel: l.ChannelID,
					Outcome: BackfillAlreadyKnown,
				})
			}
		}

		if !containsArtist(todo, artistID) {
			continue
		}

		reporter.OnArtistStart(i+1, len(todo), artistID)

		id, currentName, probeErr := s.probeArtistChannel(ctx, artistID)
		switch {
		case probeErr != nil || id == "":
			reason := "no video could be resolved for this artist"
			if probeErr != nil {
				reason = probeErr.Error()
			}
			reporter.OnArtistUnresolved(artistID, reason)

			for _, l := range grouped[artistID] {
				results = append(results, BackfillResult{
					ArtistID: l.ArtistID, Stored: l.Name,
					Outcome: BackfillFailed, Reason: reason,
				})
			}

		case known:
			// More than one link and nothing to resolve against. Reporting
			// the ID found is still useful, but nothing is written.
			reporter.OnArtistResolved(artistID, id, currentName)
			for _, l := range grouped[artistID] {
				results = append(results, BackfillResult{
					ArtistID: l.ArtistID, Stored: l.Name, Found: currentName, Channel: id,
					Outcome: BackfillUnresolved,
					Reason:  fmt.Sprintf("artist has %d channel links; attach this ID by hand", len(grouped[artistID])),
				})
			}

		default:
			reporter.OnArtistResolved(artistID, id, currentName)

			for _, l := range grouped[artistID] {
				result := BackfillResult{
					ArtistID: l.ArtistID, Stored: l.Name, Found: currentName, Channel: id,
					Outcome: BackfillAttached,
				}
				if normalizeChannelName(string(currentName)) != normalizeChannelName(string(l.Name)) {
					result.Outcome = BackfillRenamed
					result.Reason = fmt.Sprintf("stored as %q, YouTube calls it %q", l.Name, currentName)
				}

				if !params.DryRun {
					if err := s.repo.AttachID(ctx, l.ArtistID, l.Name, id); err != nil {
						result.Outcome = BackfillFailed
						result.Reason = err.Error()
					}
				}

				results = append(results, result)
			}
		}

		if err := backfillWait(ctx, params, i < len(todo)-1); err != nil {
			return results, err
		}
	}

	return results, nil
}

// backfillWait paces requests and honours cancellation. Skipped after the last
// one so a finished run does not sit there sleeping.
func backfillWait(ctx context.Context, params BackfillParams, more bool) error {
	if !more || (params.MinWait <= 0 && params.MaxWait <= 0) {
		return ctx.Err()
	}

	wait := params.MinWait
	if params.MaxWait > params.MinWait {
		wait += time.Duration(rand.Int64N(int64(params.MaxWait-params.MinWait) + 1))
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

func containsArtist(list []ArtistID, id ArtistID) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// noOpBackfillReporter is the default, so the service never nil-checks.
type noOpBackfillReporter struct{}

func (noOpBackfillReporter) OnBackfillStart(int, int)                          {}
func (noOpBackfillReporter) OnArtistStart(int, int, ArtistID)                  {}
func (noOpBackfillReporter) OnArtistResolved(ArtistID, ChannelID, ChannelName) {}
func (noOpBackfillReporter) OnArtistUnresolved(ArtistID, string)               {}

// probeArtistChannel asks the source which channel an artist's video came
// from. Several are tried because the first video of an artist is not always
// representative.
func (s *ChannelService) probeArtistChannel(ctx context.Context, artistID ArtistID) (ChannelID, ChannelName, error) {
	if s.probe == nil || s.tracks == nil {
		return "", "", fmt.Errorf("channel backfill is not configured; call NewChannelService with WithBackfill")
	}

	tracks, err := s.tracks.ByArtistID(ctx, artistID)
	if err != nil {
		return "", "", fmt.Errorf("list tracks for artist %q: %w", artistID, err)
	}
	if len(tracks) == 0 {
		return "", "", fmt.Errorf("artist %q has no tracks to probe", artistID)
	}

	var lastErr error
	for i, t := range tracks {
		if i >= backfillProbeLimit {
			break
		}

		rawID, rawName, err := s.probe(ctx, string(t.SourceID))
		if err != nil {
			lastErr = err
			continue
		}
		if rawID != "" {
			return ChannelID(rawID), ChannelName(rawName), nil
		}
	}

	if lastErr != nil {
		return "", "", lastErr
	}

	return "", "", fmt.Errorf("no video of artist %q reported a channel", artistID)
}

// backfillProbeLimit bounds how many videos are tried per artist. Three is
// enough to get past an occasional deleted or private upload.
const backfillProbeLimit = 3

// normalizeChannelName compares names loosely: YouTube changes spacing and
// case freely, and a case-only change is not a rename.
func normalizeChannelName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
