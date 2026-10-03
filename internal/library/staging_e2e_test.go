package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStagingImportEndToEnd covers the full import flow of a staged file:
// real tag parsing, channel linking, track registration, journaled physical
// move executed post-commit, and journal drain.
func TestStagingImportEndToEnd(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Music")
	artist, err := env.artists.Create(ctx, "", folder.ID, "An Artist")
	if err != nil {
		t.Fatal(err)
	}
	// Channel resolution is lookup-based: the link must exist up front.
	if err := env.channels.Link(ctx, artist.ID, "An Artist", ""); err != nil {
		t.Fatal(err)
	}

	sourceName := "dQw4w9WgXcQ.opus"
	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), sourceName), "My Song", "An Artist")

	handlerInvoked := false
	onUnknown := func(channelName, title string) (StagingResult, error) {
		handlerInvoked = true
		t.Errorf("unexpected unknown-channel handler call for %q/%q", channelName, title)
		return StagingResult{Action: ActionSkip}, nil
	}

	if err := env.staging.ProcessStaging(ctx, onUnknown); err != nil {
		t.Fatalf("process staging: %v", err)
	}
	if handlerInvoked {
		t.Error("handler ran although the channel was linked")
	}

	tracks, err := env.tracks.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(tracks))
	}

	imported := tracks[0]
	if imported.Title != "My Song" {
		t.Errorf("title = %q, want %q (from tags)", imported.Title, "My Song")
	}
	if imported.SourceID != "dQw4w9WgXcQ" {
		t.Errorf("source ID = %q, want %q", imported.SourceID, "abc123")
	}
	if imported.RelativePath != "Music/My Song.opus" {
		t.Errorf("relative path = %q, want %q", imported.RelativePath, "Music/My Song.opus")
	}

	movedTo := filepath.Join(env.paths.LibraryDir(), "Music", "My Song.opus")
	if _, err := os.Stat(movedTo); err != nil {
		t.Errorf("file not in library: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.paths.StagingDir(), sourceName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staged file still present: %v", err)
	}

	resolved, ok, err := env.channels.ResolveArtist(ctx, "An Artist")
	if err != nil || !ok || resolved != imported.ArtistID {
		t.Errorf("channel resolution after link = (%q, %t, %v)", resolved, ok, err)
	}

	pending, err := env.actions.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("journal not drained: %d pending", len(pending))
	}
}

func TestStagingImportUnknownChannelHandlerDecisions(t *testing.T) {
	cases := []struct {
		name        string
		result      StagingResult
		wantTracks  int
		wantChannel bool
	}{
		{
			name:       "skip leaves nothing behind",
			result:     StagingResult{Action: ActionSkip},
			wantTracks: 0,
		},
		{
			name:        "import with link binds the channel permanently",
			result:      StagingResult{Action: ActionImport, Artist: ArtistID("a1"), LinkArtist: true},
			wantTracks:  1,
			wantChannel: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			env := newLibraryEnv(t)
			ctx := t.Context()

			if _, err := env.artists.Create(ctx, "a1", "", "Fallback"); err != nil {
				t.Fatal(err)
			}

			writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "aBcDeFgHiJk.opus"), "Song", "Unknown Channel")

			onUnknown := func(channelName, title string) (StagingResult, error) {
				return tt.result, nil
			}

			if err := env.staging.ProcessStaging(ctx, onUnknown); err != nil {
				t.Fatalf("process staging: %v", err)
			}

			tracks, err := env.tracks.All(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(tracks) != tt.wantTracks {
				t.Fatalf("tracks = %d, want %d", len(tracks), tt.wantTracks)
			}

			_, linked, err := env.channels.ResolveArtist(ctx, "Unknown Channel")
			if err != nil {
				t.Fatal(err)
			}
			if linked != tt.wantChannel {
				t.Errorf("channel linked = %t, want %t", linked, tt.wantChannel)
			}

			if tt.wantTracks == 1 {
				stillThere, statErr := os.Stat(filepath.Join(env.paths.StagingDir(), "aBcDeFgHiJk.opus"))
				if statErr == nil && stillThere != nil {
					t.Error("staged file not moved into library")
				}
			}
		})
	}
}

func TestStagingImportAbortStopsLoop(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "aBcDeFgHiJk.opus"), "One", "Unknown Channel")
	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "lLkJhGfDsAp.opus"), "Two", "Unknown Channel")

	calls := 0
	onUnknown := func(channelName, title string) (StagingResult, error) {
		calls++
		return StagingResult{Action: ActionAbort}, nil
	}

	err := env.staging.ProcessStaging(ctx, onUnknown)
	if !errors.Is(err, ErrProcessAborted) {
		t.Fatalf("err = %v, want ErrProcessAborted", err)
	}
	if calls != 1 {
		t.Errorf("handler called %d times, want 1 (loop must stop)", calls)
	}

	entries, err := os.ReadDir(env.paths.StagingDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("staging dir = %d files, want 2 untouched after abort", len(entries))
	}
}

func TestSanitizeFilenameInImportedTitle(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	artist, err := env.artists.Create(ctx, "", "", "An Artist")
	if err != nil {
		t.Fatal(err)
	}

	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "aBcDeFgHiJk.opus"), `Weird/Title: Remix*`, "An Artist")

	onUnknown := func(channelName, title string) (StagingResult, error) {
		return StagingResult{Action: ActionImport, Artist: artist.ID}, nil
	}

	if err := env.staging.ProcessStaging(ctx, onUnknown); err != nil {
		t.Fatalf("process staging: %v", err)
	}

	tracks, err := env.tracks.All(ctx)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("tracks = %d (err %v), want 1", len(tracks), err)
	}
	if strings.ContainsAny(string(tracks[0].RelativePath), `/:*?|"<>`) || strings.Contains(string(tracks[0].RelativePath), "\\") {
		t.Errorf("illegal characters survived in path %q", tracks[0].RelativePath)
	}
	if _, statErr := os.Stat(filepath.Join(env.paths.LibraryDir(), filepath.FromSlash(string(tracks[0].RelativePath)))); statErr != nil {
		t.Errorf("sanitized file missing on disk: %v", statErr)
	}
}

func TestStagingImportRejectsForeignFilenames(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	artist, err := env.artists.Create(ctx, "", "", "An Artist")
	if err != nil {
		t.Fatal(err)
	}

	// A title-named file (as produced by another managed env) must not be
	// imported with the whole filename as its source ID.
	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "From The Start.opus"), "From The Start", "An Artist")
	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "dQw4w9WgXcQ.opus"), "Real Song", "An Artist")

	onUnknown := func(channelName, title string) (StagingResult, error) {
		return StagingResult{Action: ActionImport, Artist: artist.ID}, nil
	}

	if err := env.staging.ProcessStaging(ctx, onUnknown); err != nil {
		t.Fatalf("process staging: %v", err)
	}

	tracks, err := env.tracks.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("tracks = %d, want 1 (only the well-formed staged file)", len(tracks))
	}
	if tracks[0].SourceID != "dQw4w9WgXcQ" {
		t.Errorf("source ID = %q, want dQw4w9WgXcQ", tracks[0].SourceID)
	}

	// The rejected file stays in staging for the user to deal with.
	if _, statErr := os.Stat(filepath.Join(env.paths.StagingDir(), "From The Start.opus")); statErr != nil {
		t.Errorf("foreign file should remain untouched in staging: %v", statErr)
	}
}

// TestStagingImportSurvivesChannelRename is the scenario the channel ID exists
// for. A performer renames their channel, so the artist tag on the next
// download carries a name no link holds, and the tag lookup misses.
//
// Before, that meant the unknown-channel handler ran for every affected file.
// Now the ID recorded at download time resolves the artist regardless, the
// stale label is corrected in passing, and the handler stays out of it.
func TestStagingImportSurvivesChannelRename(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Music")
	artist, err := env.artists.Create(ctx, "", folder.ID, "An Artist")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.channels.Link(ctx, artist.ID, "Old Channel Name", ""); err != nil {
		t.Fatal(err)
	}
	if err := env.channels.AttachChannelID(ctx, artist.ID, "Old Channel Name", "UC8rcEBzJSleTkf_-agPM20g"); err != nil {
		t.Fatalf("attach channel id: %v", err)
	}

	// Download time recorded the ID; the file's tag carries the new name.
	const videoID = "dQw4w9WgXcQ"
	if err := env.channels.RecordVideoChannel(ctx, videoID, "UC8rcEBzJSleTkf_-agPM20g"); err != nil {
		t.Fatalf("record video channel: %v", err)
	}
	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), videoID+".opus"), "Renamed Song", "New Channel Name")

	onUnknown := func(channelName, title string) (StagingResult, error) {
		t.Errorf("unknown-channel handler ran for %q/%q; the recorded ID should have resolved it", channelName, title)
		return StagingResult{Action: ActionSkip}, nil
	}

	if err := env.staging.ProcessStaging(ctx, onUnknown); err != nil {
		t.Fatalf("process staging: %v", err)
	}

	tracks, err := env.tracks.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(tracks))
	}
	if tracks[0].ArtistID != artist.ID {
		t.Errorf("track artist = %q, want %q", tracks[0].ArtistID, artist.ID)
	}

	// The stored label follows YouTube rather than drifting forever.
	resolved, ok, err := env.channels.ResolveArtist(ctx, "New Channel Name")
	if err != nil || !ok {
		t.Fatalf("new name not resolvable after import: (%q, %t, %v)", resolved, ok, err)
	}
	if resolved != artist.ID {
		t.Errorf("new name resolves to %q, want %q", resolved, artist.ID)
	}

	// And no duplicate link was left behind.
	all, err := env.channels.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("%d channel rows after rename, want 1", len(all))
	}
	if all[0].Name != "New Channel Name" {
		t.Errorf("stored name = %q, want the refreshed label", all[0].Name)
	}
}

// TestStagingImportWithoutRecordedChannelFallsBackToName keeps the old path
// working: a file imported with no download-time record still resolves by its
// tag, so hand-placed files and pre-existing staging continue to import.
func TestStagingImportWithoutRecordedChannelFallsBackToName(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Music")
	artist, err := env.artists.Create(ctx, "", folder.ID, "An Artist")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.channels.Link(ctx, artist.ID, "An Artist", ""); err != nil {
		t.Fatal(err)
	}

	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "dQw4w9WgXcQ.opus"), "Manual Song", "An Artist")

	if err := env.staging.ProcessStaging(ctx, func(channelName, title string) (StagingResult, error) {
		t.Errorf("handler ran for %q/%q despite a matching name link", channelName, title)
		return StagingResult{Action: ActionSkip}, nil
	}); err != nil {
		t.Fatalf("process staging: %v", err)
	}

	tracks, err := env.tracks.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].ArtistID != artist.ID {
		t.Fatalf("tracks = %+v, want one track attributed to %q", tracks, artist.ID)
	}
}

// TestStagingImportLinksTheNewChannelWithItsID is the common path, not an edge
// case. Most links in a real library are created by this prompt rather than by
// a typed command, and it runs when the download has already recorded the
// channel ID.
//
// Creating the link without it is what left every real link needing a repair
// pass, so the ID is passed through to the new link.
func TestStagingImportLinksTheNewChannelWithItsID(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Music")
	artist, err := env.artists.Create(ctx, "", folder.ID, "New Artist")
	if err != nil {
		t.Fatal(err)
	}

	// No link exists yet: this is a first sighting of the channel.
	const videoID = "dQw4w9WgXcQ"
	if err := env.channels.RecordVideoChannel(ctx, videoID, "UC8rcEBzJSleTkf_-agPM20g"); err != nil {
		t.Fatalf("record video channel: %v", err)
	}
	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), videoID+".opus"), "Brand New", "New Artist Ch.")

	linked := false
	onUnknown := func(channelName, title string) (StagingResult, error) {
		linked = true
		return StagingResult{Action: ActionImport, Artist: artist.ID, LinkArtist: true}, nil
	}

	if err := env.staging.ProcessStaging(ctx, onUnknown); err != nil {
		t.Fatalf("process staging: %v", err)
	}
	if !linked {
		t.Fatal("unknown-channel handler never ran, so no link was created")
	}

	all, err := env.channels.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("channel links = %d, want 1", len(all))
	}
	if all[0].ChannelID != "UC8rcEBzJSleTkf_-agPM20g" {
		t.Errorf("new link has channel id %q, want the ID the download recorded", all[0].ChannelID)
	}
	if all[0].ArtistID != artist.ID {
		t.Errorf("new link artist = %q, want %q", all[0].ArtistID, artist.ID)
	}
}

// TestStagingImportLinkWithoutARecordStaysPartial: a link made where no
// download record exists has no ID to give, and must still be created rather
// than refused.
func TestStagingImportLinkWithoutARecordStaysPartial(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Music")
	artist, err := env.artists.Create(ctx, "", folder.ID, "Hand Artist")
	if err != nil {
		t.Fatal(err)
	}

	// No RecordVideoChannel: this file was placed in staging by hand.
	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "dQw4w9WgXcQ.opus"), "Manual", "Hand Artist Ch.")

	if err := env.staging.ProcessStaging(ctx, func(string, string) (StagingResult, error) {
		return StagingResult{Action: ActionImport, Artist: artist.ID, LinkArtist: true}, nil
	}); err != nil {
		t.Fatalf("process staging: %v", err)
	}

	all, err := env.channels.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("channel links = %d, want 1: a link with no ID must still be made", len(all))
	}
	if all[0].ChannelID != "" {
		t.Errorf("channel id = %q, want empty when no record existed", all[0].ChannelID)
	}
}
