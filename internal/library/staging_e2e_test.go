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
	if err := env.channels.Link(ctx, artist.ID, "An Artist"); err != nil {
		t.Fatal(err)
	}

	sourceName := "abc123.opus"
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
	if imported.SourceID != "abc123" {
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

			writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "id1.opus"), "Song", "Unknown Channel")

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
				stillThere, statErr := os.Stat(filepath.Join(env.paths.StagingDir(), "id1.opus"))
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

	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "one.opus"), "One", "Unknown Channel")
	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "two.opus"), "Two", "Unknown Channel")

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

	writeTaggedAudio(t, filepath.Join(env.paths.StagingDir(), "id1.opus"), `Weird/Title: Remix*`, "An Artist")

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
