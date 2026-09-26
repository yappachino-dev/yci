package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedRaw creates a track whose stored title is the raw video title, as rows
// that predate title parsing look.
func (e *libraryEnv) seedRaw(t *testing.T, id, rawTitle string) Track {
	t.Helper()

	track, err := e.tracks.Create(t.Context(), TrackCreateParams{
		ID:           TrackID(id),
		Title:        rawTitle,
		RelativePath: TrackPath(id + ".opus"),
		RawTitle:     rawTitle,
	})
	if err != nil {
		t.Fatal(err)
	}

	return track
}

func TestRetitleAppliesHighConfidenceParsesOnly(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	noisy := env.seedRaw(t, "t1", "【MV】See the world / IRyS【Original Song】")
	clean := env.seedRaw(t, "t2", "the boy is mine")
	ambiguous := env.seedRaw(t, "t3", "Crazy:B「Honeycomb Summer」 / 戌亥とこ × 星街すいせい")

	report, err := env.tracks.Retitle(ctx, false)
	if err != nil {
		t.Fatalf("retitle: %v", err)
	}
	if report.Scanned != 3 {
		t.Errorf("scanned = %d, want 3", report.Scanned)
	}
	if report.Retitled != 1 {
		t.Errorf("retitled = %d, want 1 (only the high-confidence title)", report.Retitled)
	}
	// The bare title is already fully interpreted and is skipped entirely.
	if report.Updated != 2 {
		t.Errorf("updated = %d, want 2", report.Updated)
	}

	got, err := env.tracks.ByID(ctx, noisy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "See the world" {
		t.Errorf("title = %q, want %q", got.Title, "See the world")
	}
	if got.Performer != "IRyS" {
		t.Errorf("performer = %q, want %q", got.Performer, "IRyS")
	}
	if got.Version != "original" {
		t.Errorf("version = %q, want %q", got.Version, "original")
	}
	if got.RawTitle != "【MV】See the world / IRyS【Original Song】" {
		t.Errorf("raw title was not preserved: %q", got.RawTitle)
	}

	// A title the parser cannot improve keeps its stored form.
	untouched, err := env.tracks.ByID(ctx, clean.ID)
	if err != nil {
		t.Fatal(err)
	}
	if untouched.Title != "the boy is mine" {
		t.Errorf("bare title was rewritten to %q", untouched.Title)
	}

	// Low confidence never replaces a title, but hints are still recorded.
	low, err := env.tracks.ByID(ctx, ambiguous.ID)
	if err != nil {
		t.Fatal(err)
	}
	if low.Title != ambiguous.Title {
		t.Errorf("low-confidence title was replaced: %q -> %q", ambiguous.Title, low.Title)
	}
	if low.Performer == "" {
		t.Error("low-confidence parse recorded no performer hint")
	}
}

func TestRetitleBackfillsRowsWithoutProvenance(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	// A pre-migration row: title set, raw_title NULL.
	if _, err := env.tracks.Create(ctx, TrackCreateParams{
		ID:           "legacy",
		Title:        "月に向かって撃て / 星街すいせい(official)",
		RelativePath: "legacy.opus",
	}); err != nil {
		t.Fatal(err)
	}

	report, err := env.tracks.Retitle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Retitled != 1 || report.Updated != 1 {
		t.Errorf("report = %+v, want one retitled row", report)
	}

	got, err := env.tracks.ByID(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "月に向かって撃て" {
		t.Errorf("title = %q, want the parsed song", got.Title)
	}
	if got.RawTitle != "月に向かって撃て / 星街すいせい(official)" {
		t.Errorf("raw title = %q, want the pre-parse title preserved", got.RawTitle)
	}

	// Second run is a no-op: the pass is idempotent.
	second, err := env.tracks.Retitle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Updated != 0 {
		t.Errorf("second run updated %d rows, want 0", second.Updated)
	}
}

func TestRetitleDryRunWritesNothing(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	track := env.seedRaw(t, "t1", "【COVER】SOS (Nerissa Ravencroft)")

	report, err := env.tracks.Retitle(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 1 || len(report.Changes) != 1 {
		t.Fatalf("report = %+v, want one pending change", report)
	}
	if report.Changes[0].To != "SOS" {
		t.Errorf("reported title = %q, want %q", report.Changes[0].To, "SOS")
	}

	unchanged, err := env.tracks.ByID(ctx, track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Title != track.Title {
		t.Errorf("dry run wrote: title = %q", unchanged.Title)
	}
}

func TestImportStoresParsedTitleAndProvenance(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Music")
	artist, err := env.artists.Create(ctx, "", folder.ID, "An Artist")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.channels.Link(ctx, artist.ID, "An Artist"); err != nil {
		t.Fatal(err)
	}

	const rawTitle = "【MV】Turing Love / Gigi Murin(Cover)"
	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "dQw4w9WgXcQ.opus"), rawTitle, "An Artist")

	skip := func(channelName, title string) (StagingResult, error) {
		t.Errorf("unexpected handler call for %q/%q", channelName, title)
		return StagingResult{Action: ActionSkip}, nil
	}

	if err := env.staging.ProcessStaging(ctx, skip); err != nil {
		t.Fatalf("process staging: %v", err)
	}

	tracks, err := env.tracks.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(tracks))
	}

	imported := tracks[0]
	if imported.Title != "Turing Love" {
		t.Errorf("title = %q, want the parsed song %q", imported.Title, "Turing Love")
	}
	if imported.Performer != "Gigi Murin" {
		t.Errorf("performer = %q, want %q", imported.Performer, "Gigi Murin")
	}
	if imported.Version != "cover" {
		t.Errorf("version = %q, want %q", imported.Version, "cover")
	}
	if imported.RawTitle != rawTitle {
		t.Errorf("raw title = %q, want %q", imported.RawTitle, rawTitle)
	}

	// The filename follows the clean title, since it is derived at import.
	if !strings.HasSuffix(string(imported.RelativePath), "/Turing Love.opus") {
		t.Errorf("relative path = %q, want it named after the clean title", imported.RelativePath)
	}
}

func TestImportDoesNotOverwriteOccupiedDestination(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Music")
	artist, err := env.artists.Create(ctx, "", folder.ID, "An Artist")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.channels.Link(ctx, artist.ID, "An Artist"); err != nil {
		t.Fatal(err)
	}

	// Two different uploads that clean down to the same song title.
	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "dQw4w9WgXcQ.opus"), "【MV】Melt / A(Cover)", "An Artist")
	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "aaaaaaaaaaa.opus"), "【MV】Melt / B(Cover)", "An Artist")

	skip := func(channelName, title string) (StagingResult, error) {
		return StagingResult{Action: ActionSkip}, nil
	}

	if err := env.staging.ProcessStaging(ctx, skip); err != nil {
		t.Fatalf("process staging: %v", err)
	}

	tracks, err := env.tracks.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 {
		t.Fatalf("tracks = %d, want 2", len(tracks))
	}

	paths := map[string]bool{}
	for _, tr := range tracks {
		if paths[string(tr.RelativePath)] {
			t.Errorf("duplicate relative path: %s", tr.RelativePath)
		}
		paths[string(tr.RelativePath)] = true

		abs := filepath.Join(env.paths.LibraryDir(), filepath.FromSlash(string(tr.RelativePath)))
		if _, err := os.Stat(abs); err != nil {
			t.Errorf("imported file missing at %s: %v", tr.RelativePath, err)
		}
	}
}
