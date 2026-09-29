package library

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ReseedChange is one membership difference between a playlist and what its
// recipe currently derives.
type ReseedChange struct {
	Track Track
	// Add is true for a track the recipe now matches and the playlist lacks.
	Add bool
}

// ReseedReport describes what re-deriving a playlist would do.
//
// The point of a report rather than a silent rewrite is that a recipe is a
// claim about a library that keeps changing. Applying it is a decision, so
// the caller sees the difference first.
type ReseedReport struct {
	// Recipe is the stored token set that produced the derivation.
	Recipe []string
	// MatchMode is how the recipe's tokens combine.
	MatchMode MatchMode
	// SeedTitle is the seed track's current title, for the report header.
	SeedTitle string
	// Current counts the tracks the playlist holds right now, which is not
	// derivable from Matched: membership can also have been edited by hand.
	Current int
	// Matched counts every track the recipe matches, including ones already
	// present.
	Matched int
	// Unchanged counts matched tracks already in the playlist.
	Unchanged int
	// Changes lists additions and removals, in that order.
	Changes []ReseedChange
	// Unmatched counts tracks currently in the playlist that the recipe no
	// longer matches. These are the removals.
	Unmatched int
}

func (r ReseedReport) Added() int {
	n := 0
	for _, c := range r.Changes {
		if c.Add {
			n++
		}
	}
	return n
}

func (r ReseedReport) Removed() int {
	n := 0
	for _, c := range r.Changes {
		if !c.Add {
			n++
		}
	}
	return n
}

// Changed reports whether applying the recipe would alter membership.
func (r ReseedReport) Changed() bool {
	return len(r.Changes) > 0
}

// CreateFromSeed creates a playlist, records the recipe that produced its
// membership, and adds the matched tracks, all in one transaction.
//
// Splitting this across separate calls would allow a playlist to exist whose
// rows no recipe explains, which is exactly the state `playlist reseed` is
// meant to be able to reproduce.
func (s *PlaylistService) CreateFromSeed(
	ctx context.Context,
	name string,
	seedTrackID TrackID,
	tokens []string,
	mode MatchMode,
	trackIDs []TrackID,
) (Playlist, error) {
	if seedTrackID == "" || len(tokens) == 0 {
		return Playlist{}, fmt.Errorf("a seed recipe needs both a seed track and at least one token")
	}

	encoded, err := json.Marshal(tokens)
	if err != nil {
		return Playlist{}, fmt.Errorf("encode seed tokens: %w", err)
	}

	trimmed := strings.TrimSpace(name)

	var created Playlist
	err = s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		p, err := s.repo.Create(txCtx, trimmed)
		if err != nil {
			return fmt.Errorf("create playlist %q: %w", trimmed, err)
		}
		created = p

		if err := s.repo.SetRecipe(txCtx, p.ID, seedTrackID, string(encoded), mode); err != nil {
			return err
		}

		for _, id := range trackIDs {
			if err := s.repo.AddTrack(txCtx, p.ID, id); err != nil {
				return fmt.Errorf("add track %s to playlist %q: %w", id, trimmed, err)
			}
		}

		return nil
	})
	if err != nil {
		return Playlist{}, err
	}

	return created, nil
}

// Derive resolves a recipe against the current library, returning the
// matching tracks. It is the single definition of what a recipe means, used
// by both seeding and reseeding so the two can never disagree.
func (s *PlaylistService) Derive(ctx context.Context, tokens []string, mode MatchMode) ([]Track, error) {
	all, err := s.tracks.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tracks: %w", err)
	}

	matched := make([]Track, 0, len(all))
	for _, t := range all {
		if MatchesTokens(t.Title, tokens, mode) {
			matched = append(matched, t)
		}
	}

	return matched, nil
}

// ReseedPlan re-derives a playlist's membership and reports the difference
// without writing. dryRun is not a parameter: planning never writes.
func (s *PlaylistService) ReseedPlan(ctx context.Context, name string) (ReseedReport, error) {
	var report ReseedReport

	p, err := s.repo.ByName(ctx, strings.TrimSpace(name))
	if err != nil {
		return report, fmt.Errorf("find playlist %q: %w", name, err)
	}

	if !p.HasRecipe() {
		return report, &NoRecipeError{Playlist: p.Name}
	}

	// A missing seed is a distinct failure from a missing recipe: the token
	// set is intact but nothing can be re-derived from it, and the fix is to
	// re-seed rather than to reconstruct the recipe.
	if !p.HasSeed() {
		return report, &MissingSeedError{Playlist: p.Name}
	}

	seed, err := s.tracks.ByID(ctx, p.SeedTrackID)
	if err != nil {
		return report, &MissingSeedError{Playlist: p.Name, SeedTrackID: p.SeedTrackID}
	}

	matched, err := s.Derive(ctx, p.FragmentTokens, p.MatchMode)
	if err != nil {
		return report, err
	}

	entries, err := s.repo.Tracks(ctx, p.ID)
	if err != nil {
		return report, fmt.Errorf("read playlist %q membership: %w", name, err)
	}

	report.Current = len(entries)

	present := make(map[TrackID]bool, len(entries))
	for _, e := range entries {
		present[e.Track.ID] = true
	}

	inMatch := make(map[TrackID]bool, len(matched))
	for _, t := range matched {
		inMatch[t.ID] = true
		if present[t.ID] {
			report.Unchanged++
			continue
		}
		report.Changes = append(report.Changes, ReseedChange{Track: t, Add: true})
	}

	for _, e := range entries {
		if !inMatch[e.Track.ID] {
			report.Changes = append(report.Changes, ReseedChange{Track: e.Track, Add: false})
		}
	}

	report.Recipe = p.FragmentTokens
	report.MatchMode = p.MatchMode
	report.SeedTitle = seed.Title
	report.Matched = len(matched)
	report.Unmatched = report.Removed()

	return report, nil
}

// Reseed applies a plan: adds newly matched tracks and removes ones the
// recipe no longer matches, in one transaction.
//
// It is deliberately not automatic. Membership changing because someone
// imported a file is the failure mode this design exists to avoid, so the
// diff is always something a person asked for.
func (s *PlaylistService) Reseed(ctx context.Context, name string, report ReseedReport) error {
	if !report.Changed() {
		return nil
	}

	p, err := s.repo.ByName(ctx, strings.TrimSpace(name))
	if err != nil {
		return fmt.Errorf("find playlist %q: %w", name, err)
	}

	return s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		for _, change := range report.Changes {
			if change.Add {
				if err := s.repo.AddTrack(txCtx, p.ID, change.Track.ID); err != nil {
					return fmt.Errorf("add track %s to playlist %q: %w", change.Track.ID, name, err)
				}
				continue
			}
			if err := s.repo.RemoveTrack(txCtx, p.ID, change.Track.ID); err != nil {
				return fmt.Errorf("remove track %s from playlist %q: %w", change.Track.ID, name, err)
			}
		}
		return nil
	})
}

// NoRecipeError reports that a playlist cannot be re-derived because it was
// not seeded. Distinct from other failures so the CLI can suggest the fix
// instead of reporting a generic problem.
type NoRecipeError struct {
	Playlist string
}

func (e *NoRecipeError) Error() string {
	return fmt.Sprintf("playlist %q was not created from a seed track, so it has no recipe to re-derive", e.Playlist)
}

// MissingSeedError reports that a playlist's recipe cannot be evaluated
// because its seed track is gone. Distinct from NoRecipeError because the
// remedy differs: the recipe exists and is intact, but nothing can be
// re-derived from it until a new seed is chosen.
type MissingSeedError struct {
	Playlist    string
	SeedTrackID TrackID
}

func (e *MissingSeedError) Error() string {
	if e.SeedTrackID == "" {
		return fmt.Sprintf("playlist %q has a recipe but its seed track has been deleted", e.Playlist)
	}
	return fmt.Sprintf("playlist %q was seeded from track %s, which is no longer in the library", e.Playlist, e.SeedTrackID)
}
