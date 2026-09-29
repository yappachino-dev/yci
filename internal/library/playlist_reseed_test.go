package library

import (
	"context"
	"errors"
	"testing"

	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/store"
)

// seedPlaylist creates tracks with the given titles and returns the env plus
// the IDs, so each test can seed a playlist and then perturb the library.
func seedPlaylist(t *testing.T, titles ...string) (*libraryEnv, []TrackID) {
	t.Helper()

	env := newLibraryEnv(t)
	ids := make([]TrackID, 0, len(titles))

	for i, title := range titles {
		id := TrackID(string(rune('a'+i)) + "-track")
		env.mustPutTrack(t, string(id), title, string(rune('a'+i))+"/track.opus")
		ids = append(ids, id)
	}

	return env, ids
}

func TestCreateFromSeedStoresRecipe(t *testing.T) {
	env, ids := seedPlaylist(t,
		"comet / 星街すいせい(official)",
		"みちづれ / 星街すいせい(official)",
		"unrelated song",
	)
	ctx := context.Background()

	if _, err := env.playlists.CreateFromSeed(ctx, "Seeded", ids[0], []string{"星街すいせい"}, ids[:2]); err != nil {
		t.Fatal(err)
	}

	p, err := env.playlists.ByName(ctx, "Seeded")
	if err != nil {
		t.Fatal(err)
	}

	if !p.HasRecipe() {
		t.Fatal("seeded playlist reports no recipe")
	}
	if p.SeedTrackID != ids[0] {
		t.Errorf("SeedTrackID = %q, want %q", p.SeedTrackID, ids[0])
	}
	if len(p.FragmentTokens) != 1 || p.FragmentTokens[0] != "星街すいせい" {
		t.Errorf("FragmentTokens = %q, want [星街すいせい]", p.FragmentTokens)
	}
}

func TestHandBuiltPlaylistHasNoRecipe(t *testing.T) {
	env, _ := seedPlaylist(t, "comet / 星街すいせい(official)")
	ctx := context.Background()

	if _, err := env.playlists.Create(ctx, "Handmade"); err != nil {
		t.Fatal(err)
	}

	p, err := env.playlists.ByName(ctx, "Handmade")
	if err != nil {
		t.Fatal(err)
	}
	if p.HasRecipe() {
		t.Error("a hand-built playlist must not report a recipe")
	}

	_, err = env.playlists.ReseedPlan(ctx, "Handmade")
	var noRecipe *NoRecipeError
	if !errors.As(err, &noRecipe) {
		t.Errorf("ReseedPlan error = %v, want NoRecipeError", err)
	}
}

func TestReseedPlanIsNoOpWhenNothingChanged(t *testing.T) {
	env, ids := seedPlaylist(t,
		"comet / 星街すいせい(official)",
		"みちづれ / 星街すいせい(official)",
		"unrelated song",
	)
	ctx := context.Background()

	if _, err := env.playlists.CreateFromSeed(ctx, "Seeded", ids[0], []string{"星街すいせい"}, ids[:2]); err != nil {
		t.Fatal(err)
	}

	report, err := env.playlists.ReseedPlan(ctx, "Seeded")
	if err != nil {
		t.Fatal(err)
	}

	if report.Changed() {
		t.Errorf("reseed reported changes on an unchanged library: %+v", report.Changes)
	}
	if report.Matched != 2 {
		t.Errorf("Matched = %d, want 2", report.Matched)
	}
	// Current must be the real membership count, not something derived from
	// Matched: the two differ whenever a track is added or removed by hand.
	if report.Current != 2 {
		t.Errorf("Current = %d, want 2", report.Current)
	}
}

func TestReseedPlanDetectsNewlyMatchingTrack(t *testing.T) {
	env, ids := seedPlaylist(t,
		"comet / 星街すいせい(official)",
		"unrelated song",
	)
	ctx := context.Background()

	if _, err := env.playlists.CreateFromSeed(ctx, "Seeded", ids[0], []string{"星街すいせい"}, []TrackID{ids[0]}); err != nil {
		t.Fatal(err)
	}

	// A new upload arrives that the recipe should now match.
	newID := TrackID("z-track")
	env.mustPutTrack(t, string(newID), "バイバイレイニー / 星街すいせい(official)", "z/track.opus")

	report, err := env.playlists.ReseedPlan(ctx, "Seeded")
	if err != nil {
		t.Fatal(err)
	}

	if !report.Changed() {
		t.Fatal("reseed reported no change after a matching track was added")
	}
	if report.Added() != 1 || report.Removed() != 0 {
		t.Errorf("added=%d removed=%d, want 1/0", report.Added(), report.Removed())
	}
	if report.Changes[0].Track.ID != newID || !report.Changes[0].Add {
		t.Errorf("expected an add of %q, got %+v", newID, report.Changes[0])
	}

	// Planning must never write.
	entries, err := env.playlists.Entries(ctx, "Seeded")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("plan wrote %d entries; it must not write at all", len(entries))
	}

	if err := env.playlists.Reseed(ctx, "Seeded", report); err != nil {
		t.Fatal(err)
	}

	entries, err = env.playlists.Entries(ctx, "Seeded")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("after reseed the playlist has %d entries, want 2", len(entries))
	}
}

func TestReseedPlanDetectsTrackThatStoppedMatching(t *testing.T) {
	env, ids := seedPlaylist(t,
		"comet / 星街すいせい(official)",
		"みちづれ / 星街すいせい(official)",
	)
	ctx := context.Background()

	if _, err := env.playlists.CreateFromSeed(ctx, "Seeded", ids[0], []string{"星街すいせい"}, ids); err != nil {
		t.Fatal(err)
	}

	// The user retitles a member so the recipe no longer claims it.
	if err := env.queries.UpdateTrackTitle(ctx, store.UpdateTrackTitleParams{
		ID:       string(ids[1]),
		Title:    "totally different",
		RawTitle: db.ToNullString("totally different"),
	}); err != nil {
		t.Fatal(err)
	}

	report, err := env.playlists.ReseedPlan(ctx, "Seeded")
	if err != nil {
		t.Fatal(err)
	}

	if report.Added() != 0 || report.Removed() != 1 {
		t.Fatalf("added=%d removed=%d, want 0/1: %+v", report.Added(), report.Removed(), report.Changes)
	}
	if report.Changes[0].Add {
		t.Error("the change should be a removal")
	}

	if err := env.playlists.Reseed(ctx, "Seeded", report); err != nil {
		t.Fatal(err)
	}

	entries, err := env.playlists.Entries(ctx, "Seeded")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("after reseed the playlist has %d entries, want 1", len(entries))
	}
	if entries[0].Track.ID != ids[0] {
		t.Errorf("wrong survivor: %s, want %s", entries[0].Track.ID, ids[0])
	}
}

func TestReseedPlanFailsClearlyWhenSeedIsGone(t *testing.T) {
	env, ids := seedPlaylist(t, "comet / 星街すいせい(official)")
	ctx := context.Background()

	if _, err := env.playlists.CreateFromSeed(ctx, "Seeded", ids[0], []string{"星街すいせい"}, ids); err != nil {
		t.Fatal(err)
	}

	if err := env.tracks.Delete(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}

	// Deleting the seed cascades to NULL on the FK, so the recipe survives
	// without a seed. That must not be reported as "never seeded".
	p, err := env.playlists.ByName(ctx, "Seeded")
	if err != nil {
		t.Fatal(err)
	}
	if !p.HasRecipe() {
		t.Error("the recipe should survive deletion of the seed track")
	}
	if p.HasSeed() {
		t.Error("seed_track_id should be NULL after the seed track is deleted")
	}

	_, err = env.playlists.ReseedPlan(ctx, "Seeded")
	var missing *MissingSeedError
	if !errors.As(err, &missing) {
		t.Fatalf("ReseedPlan error = %v, want MissingSeedError", err)
	}
	if err.Error() == "" {
		t.Error("MissingSeedError has no message")
	}
}

func TestCreateFromSeedRequiresBothSeedAndTokens(t *testing.T) {
	env, _ := seedPlaylist(t, "comet")
	ctx := context.Background()

	if _, err := env.playlists.CreateFromSeed(ctx, "X", "", []string{"a"}, nil); err == nil {
		t.Error("accepted a recipe with no seed track")
	}
	if _, err := env.playlists.CreateFromSeed(ctx, "Y", "a-track", nil, nil); err == nil {
		t.Error("accepted a recipe with no tokens")
	}
}

func TestCreateFromSeedLeavesNothingOnFailure(t *testing.T) {
	env, _ := seedPlaylist(t, "comet")
	ctx := context.Background()

	// A track that does not exist must roll the whole thing back, so a
	// failed seed cannot leave a playlist holding a recipe and no members.
	if _, err := env.playlists.CreateFromSeed(ctx, "Doomed", "missing", []string{"a"}, []TrackID{"missing"}); err == nil {
		t.Fatal("expected failure seeding with an unknown track")
	}

	all, err := env.playlists.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("a failed seed left %d playlist(s) behind", len(all))
	}
}

func TestRecipeTokensSurviveMixedScripts(t *testing.T) {
	env, _ := seedPlaylist(t, "comet")
	ctx := context.Background()

	want := []string{"Mori", "Calliope", "星街すいせい"}
	if _, err := env.playlists.CreateFromSeed(ctx, "Tokens", "a-track", want, nil); err != nil {
		t.Fatal(err)
	}

	p, err := env.playlists.ByName(ctx, "Tokens")
	if err != nil {
		t.Fatal(err)
	}

	if len(p.FragmentTokens) != len(want) {
		t.Fatalf("tokens = %q, want %q", p.FragmentTokens, want)
	}
	for i := range want {
		if p.FragmentTokens[i] != want[i] {
			t.Fatalf("tokens = %q, want %q", p.FragmentTokens, want)
		}
	}
}

func TestDeriveMatchesOnTokensNotJoinedSubstring(t *testing.T) {
	env, _ := seedPlaylist(t,
		"BRAIN - Mori Calliope (cover)",
		"【MV】Orpheus - Mori Calliope",
		"【#Requiem】Bocca della Verità feat @MoriCalliope",
		"KICK BACK / another performer",
	)
	ctx := context.Background()

	matched, err := env.playlists.Derive(ctx, []string{"Mori", "Calliope"})
	if err != nil {
		t.Fatal(err)
	}

	// The run-together spelling is only reachable because matching is a
	// token set rather than one joined substring.
	if len(matched) != 3 {
		t.Errorf("Derive matched %d tracks, want 3", len(matched))
	}
}
