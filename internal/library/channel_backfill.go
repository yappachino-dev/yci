package library

import (
	"context"
	"fmt"
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

// BackfillChannels converges stored channel links with what YouTube reports.
//
// It refreshes two things: the missing identity, and a display name that has
// drifted. Skipping links that already have an ID looks like an optimisation
// and is not one, because whether the *name* is current is not knowable
// without asking. A link whose name has drifted is exactly the broken state
// this exists to repair, and it is invisible from the ID alone.
//
// So every artist is queried, and each link is reconciled against the one
// channel the query answered about. Links belonging to a different channel
// than the one queried are left alone: an artist can publish on several, and
// applying one channel's name to all of them would corrupt them.
func (s *ChannelService) BackfillChannels(ctx context.Context, params BackfillParams) ([]BackfillResult, error) {
	links, err := s.repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}

	order := make([]ArtistID, 0, len(links))
	grouped := make(map[ArtistID][]Channel, len(links))
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

	reporter.OnBackfillStart(len(order), len(links))

	results := make([]BackfillResult, 0, len(links))

	for i, artistID := range order {
		if err := ctx.Err(); err != nil {
			return results, err
		}

		// Numbered over every artist, matching the total above it.
		reporter.OnArtistStart(i+1, len(order), artistID)

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

		default:
			reporter.OnArtistResolved(artistID, id, currentName)

			for _, l := range grouped[artistID] {
				result := s.reconcileLink(ctx, l, id, currentName, len(grouped[artistID]), params.DryRun)
				results = append(results, result)
			}
		}

		if err := Pace(ctx, params.MinWait, params.MaxWait, len(order)-i); err != nil {
			return results, err
		}
	}

	return results, nil
}

// reconcileLink decides what one stored link needs, given the identity and
// current name the probe returned for that artist.
func (s *ChannelService) reconcileLink(ctx context.Context, link Channel, id ChannelID, currentName ChannelName, artistLinks int, dryRun bool) BackfillResult {
	renamed := normalizeChannelName(string(currentName)) != normalizeChannelName(string(link.Name))

	// This link belongs to a channel the probe did not ask about. An artist
	// can publish on several, and applying this channel's name to the others
	// would be wrong, so they are left exactly as they are.
	if link.ChannelID != "" && link.ChannelID != id {
		return BackfillResult{
			ArtistID: link.ArtistID, Stored: link.Name, Channel: link.ChannelID,
			Outcome: BackfillAlreadyKnown,
			Reason:  "belongs to another channel of this artist; not queried",
		}
	}

	result := BackfillResult{
		ArtistID: link.ArtistID,
		Stored:   link.Name,
		Found:    currentName,
		Channel:  id,
		Outcome:  BackfillAttached,
	}

	if renamed {
		result.Outcome = BackfillRenamed
		result.Reason = fmt.Sprintf("stored as %q, YouTube calls it %q", link.Name, currentName)
	}

	// No identity and several links: nothing to resolve against. Guessing
	// would file future downloads under the wrong artist.
	if link.ChannelID == "" && artistLinks > 1 {
		result.Outcome = BackfillUnresolved
		result.Reason = fmt.Sprintf("artist has %d channel links; attach this ID by hand", artistLinks)
		return result
	}

	// The ID already matches, so the link is trusted and only its label may
	// need correcting. This is the case that was being skipped, leaving a
	// stale name in place indefinitely.
	if link.ChannelID != "" && !renamed {
		result.Outcome = BackfillAlreadyKnown
		return result
	}

	if dryRun {
		return result
	}

	var writeErr error
	if link.ChannelID == "" {
		writeErr = s.repo.AttachID(ctx, link.ArtistID, link.Name, id)
	} else {
		writeErr = s.repo.RenameByID(ctx, id, link.ArtistID, currentName)
	}

	if writeErr != nil {
		result.Outcome = BackfillFailed
		result.Reason = writeErr.Error()
	}

	return result
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
