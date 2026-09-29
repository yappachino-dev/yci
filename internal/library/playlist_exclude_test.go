package library

import (
	"context"
	"errors"
	"testing"
)

// TestDeriveAppliesExclusions is the property that makes an exclusion mean
// something: a deselected track is not in the derived set.
func TestDeriveAppliesExclusions(t *testing.T) {
	env, _ := seedPlaylist(t,
		"comet / 星街すいせい(official)",
		"unrelated / 星街すいせい(official)",
		"KICK BACK / another performer",
	)
	ctx := context.Background()

	all, err := env.playlists.Derive(ctx, []string{"星街すいせい"}, MatchAll, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("without exclusions, derive matched %d, want 2", len(all))
	}

	dropped := all[1]
	filtered, err := env.playlists.Derive(ctx, []string{"星街すいせい"}, MatchAll, []TrackID{dropped.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].ID == dropped.ID {
		t.Errorf("excluded track %s survived derivation: %v", dropped.ID, filtered)
	}

	// Excluding every match is legal; it is the caller's job to notice.
	none, err := env.playlists.Derive(ctx, []string{"星街すいせい"}, MatchAll,
		[]TrackID{all[0].ID, dropped.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("excluding everything still derived %d track(s)", len(none))
	}
}

// TestExclusionIsRecordedAndReseedHonoursIt is the full round trip: create
// with an exclusion, confirm it is persisted, and confirm a reseed neither
// re-adds the excluded track nor reports it as a change.
func TestExclusionIsRecordedAndReseedHonoursIt(t *testing.T) {
	ctx := context.Background()

	env, ids := seedPlaylist(t,
		"comet / 星街すいせい(official)",
		"unrelated / 星街すいせい(official)",
		"another / 星街すいせい(collab)",
	)

	tokens := []string{"星街すいせい"}
	excluded := ids[2]

	matched, err := env.playlists.Derive(ctx, tokens, MatchAll, []TrackID{excluded})
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 2 {
		t.Fatalf("derive matched %d, want 2", len(matched))
	}

	kept := []TrackID{matched[0].ID, matched[1].ID}
	if _, err := env.playlists.CreateFromSeed(ctx, "Selective", ids[0], tokens, MatchAll, []TrackID{excluded}, kept); err != nil {
		t.Fatal(err)
	}

	p, err := env.playlists.ByName(ctx, "Selective")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.FragmentExcludes) != 1 || p.FragmentExcludes[0] != excluded {
		t.Errorf("stored exclusions = %v, want [%s]", p.FragmentExcludes, excluded)
	}
	if !p.Excludes(excluded) {
		t.Error("Excludes() did not report the stored exclusion")
	}

	entries, err := env.playlists.Entries(ctx, "Selective")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("playlist holds %d, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Track.ID == excluded {
			t.Fatal("the excluded track is in the playlist")
		}
	}

	report, err := env.playlists.ReseedPlan(ctx, "Selective")
	if err != nil {
		t.Fatal(err)
	}
	if report.Changed() {
		t.Errorf("reseed reported changes despite the exclusion: %+v", report.Changes)
	}
	if len(report.Excludes) != 1 || report.Excludes[0] != excluded {
		t.Errorf("report Excludes = %v, want [%s]", report.Excludes, excluded)
	}
}

// TestSetExcludesMergesRatherThanReplaces guards `playlist edit --exclude`:
// naming one track must not silently clear the exclusions a previous wizard
// session recorded.
func TestSetExcludesMergesRatherThanReplaces(t *testing.T) {
	ctx := context.Background()

	env, ids := seedPlaylist(t,
		"comet / 星街すいせい(official)",
		"a / 星街すいせい(b)",
		"b / 星街すいせい(c)",
		"c / 星街すいせい(d)",
	)

	if _, err := env.playlists.CreateFromSeed(ctx, "P", ids[0],
		[]string{"星街すいせい"}, MatchAll, []TrackID{ids[1]}, []TrackID{ids[0]}); err != nil {
		t.Fatal(err)
	}

	// Adding a second exclusion.
	if err := env.playlists.SetExcludes(ctx, "P", []TrackID{ids[1], ids[2]}); err != nil {
		t.Fatal(err)
	}

	p, err := env.playlists.ByName(ctx, "P")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.FragmentExcludes) != 2 {
		t.Errorf("exclusions = %v, want both ids", p.FragmentExcludes)
	}

	// Dropping one back out.
	if err := env.playlists.SetExcludes(ctx, "P", []TrackID{ids[1]}); err != nil {
		t.Fatal(err)
	}

	p, err = env.playlists.ByName(ctx, "P")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.FragmentExcludes) != 1 || p.FragmentExcludes[0] != ids[1] {
		t.Errorf("exclusions = %v, want only %s", p.FragmentExcludes, ids[1])
	}

	// Removing the last one re-admits the track on re-derivation.
	matched, err := env.playlists.Derive(ctx, []string{"星街すいせい"}, MatchAll, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 4 {
		t.Errorf("with no exclusions, derive matched %d, want all 4", len(matched))
	}
}

func TestSetExcludesRefusesUnseededPlaylist(t *testing.T) {
	env, _ := seedPlaylist(t, "comet")
	ctx := context.Background()

	if _, err := env.playlists.Create(ctx, "Handmade"); err != nil {
		t.Fatal(err)
	}

	err := env.playlists.SetExcludes(ctx, "Handmade", []TrackID{"x"})
	var noRecipe *NoRecipeError
	if !errors.As(err, &noRecipe) {
		t.Errorf("SetExcludes error = %v, want NoRecipeError", err)
	}
}
