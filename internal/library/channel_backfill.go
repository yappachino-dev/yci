package library

import (
	"context"
	"fmt"
	"strings"
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

// BackfillChannels recovers channel identities for links created before yci
// recorded them.
//
// The method is deliberately conservative: it attaches an ID only when the
// evidence is unambiguous. An artist with a single channel link is
// unambiguous even if the name has drifted, because the performer renaming is
// exactly the case this exists to repair. An artist with several links is not,
// and guessing would file downloads under the wrong artist.
//
// Dry run reports what would change without writing.
func (s *ChannelService) BackfillChannels(ctx context.Context, dryRun bool) ([]BackfillResult, error) {
	links, err := s.repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}

	// An artist with more than one link cannot be resolved by elimination.
	linkCounts := make(map[ArtistID]int, len(links))
	for _, l := range links {
		linkCounts[l.ArtistID]++
	}

	results := make([]BackfillResult, 0, len(links))
	resolved := make(map[ArtistID]ChannelID)
	resolvedNames := make(map[ArtistID]ChannelName)

	for _, link := range links {
		if link.ChannelID != "" {
			results = append(results, BackfillResult{
				ArtistID: link.ArtistID,
				Stored:   link.Name,
				Channel:  link.ChannelID,
				Outcome:  BackfillAlreadyKnown,
			})
			continue
		}

		// One lookup per artist is enough: the answer is about the artist.
		if _, done := resolved[link.ArtistID]; !done {
			id, name, err := s.probeArtistChannel(ctx, link.ArtistID)
			resolved[link.ArtistID] = id

			if err != nil || id == "" {
				reason := "no video could be resolved for this artist"
				if err != nil {
					reason = err.Error()
				}

				for _, l := range linksOf(links, link.ArtistID) {
					results = append(results, BackfillResult{
						ArtistID: l.ArtistID,
						Stored:   l.Name,
						Outcome:  BackfillFailed,
						Reason:   reason,
					})
				}
				continue
			}

			resolvedNames[link.ArtistID] = name
		}

		id := resolved[link.ArtistID]
		result := BackfillResult{
			ArtistID: link.ArtistID,
			Stored:   link.Name,
			Found:    resolvedNames[link.ArtistID],
			Channel:  id,
			Outcome:  BackfillAttached,
		}

		// A drifted name is the rename this whole feature exists for, and
		// worth saying so rather than reporting as a routine attach.
		if normalizeChannelName(string(result.Found)) != normalizeChannelName(string(result.Stored)) {
			result.Outcome = BackfillRenamed
			result.Reason = fmt.Sprintf("stored as %q, YouTube calls it %q", result.Stored, result.Found)
		}

		// A single link per artist means a rename cannot be a mistake here:
		// there is nothing else it could be.
		if linkCounts[link.ArtistID] > 1 {
			result.Outcome = BackfillUnresolved
			result.Reason = fmt.Sprintf("artist has %d channel links; attach this ID by hand", linkCounts[link.ArtistID])
			results = append(results, result)
			continue
		}

		if !dryRun {
			if err := s.repo.AttachID(ctx, link.ArtistID, link.Name, id); err != nil {
				result.Outcome = BackfillFailed
				result.Reason = err.Error()
			}
		}

		results = append(results, result)
	}

	return results, nil
}

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

func linksOf(links []Channel, artistID ArtistID) []Channel {
	var out []Channel
	for _, l := range links {
		if l.ArtistID == artistID {
			out = append(out, l)
		}
	}
	return out
}

// normalizeChannelName compares names loosely: YouTube changes spacing and
// case freely, and a case-only change is not a rename.
func normalizeChannelName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
