package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newPlaylistEnv(t *testing.T) (*libraryEnv, *PlaylistService) {
	t.Helper()

	env := newLibraryEnv(t)
	svc := NewPlaylistService(PlaylistServiceConfig{
		Repo:       NewSQLitePlaylistRepository(env.queries),
		Tracks:     env.tracks,
		Paths:      env.paths,
		Transactor: env.txr,
	})

	return env, svc
}

func TestPlaylistLifecycle(t *testing.T) {
	env, svc := newPlaylistEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Music")
	artist, err := env.artists.Create(ctx, "", folder.ID, "An Artist")
	if err != nil {
		t.Fatal(err)
	}
	env.mustPutTrack(t, "t1", "Song One", "Music/Song One.opus")
	env.mustPutTrack(t, "t2", "Song Two", "Music/Song Two.opus")
	_ = artist
	const t1, t2 = "t1", "t2"

	if _, err := svc.Create(ctx, "Gym"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Case-insensitive duplicate names are rejected.
	if _, err := svc.Create(ctx, "gym"); err == nil {
		t.Error("duplicate playlist name accepted")
	}

	for _, id := range []string{t1, t2} {
		if err := svc.AddTrack(ctx, "Gym", TrackID(id)); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}

	// Duplicates rejected.
	if err := svc.AddTrack(ctx, "Gym", TrackID(t1)); err == nil {
		t.Error("duplicate track accepted into playlist")
	}

	// Unknown track rejected.
	err = svc.AddTrack(ctx, "Gym", "nope-nope-nope")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("adding unknown track: err = %v", err)
	}

	entries, err := svc.Entries(ctx, "GYM") // lookup is case-insensitive
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 2 || entries[0].Track.ID != TrackID(t1) || entries[1].Track.ID != TrackID(t2) {
		t.Errorf("entries = %+v, want ordered [t1 t2]", entries)
	}
	if entries[0].Position >= entries[1].Position {
		t.Errorf("positions not increasing: %d, %d", entries[0].Position, entries[1].Position)
	}

	if err := svc.RemoveTrack(ctx, "Gym", TrackID(t1)); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := svc.RemoveTrack(ctx, "Gym", TrackID(t1)); err == nil {
		t.Error("removing absent track succeeded")
	}

	entries, err = svc.Entries(ctx, "Gym")
	if err != nil || len(entries) != 1 || entries[0].Track.ID != TrackID(t2) {
		t.Errorf("after remove entries = %+v (err %v), want [t2]", entries, err)
	}

	if err := svc.Delete(ctx, "gym"); err != nil { // case-insensitive delete
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.ByName(ctx, "Gym"); err == nil {
		t.Error("playlist survived delete")
	}
}

func TestRenderM3U(t *testing.T) {
	entries := []PlaylistEntry{
		{Track: Track{ID: "t1", Title: "Song One", RelativePath: "Artists/A/Song One.opus"}, Duration: 201},
		{Track: Track{ID: "t2", Title: "Root Song", RelativePath: "Root Song.opus"}},
	}

	got := RenderM3U("My Mix", entries, "../")

	want := strings.Join([]string{
		"#EXTM3U",
		"#PLAYLIST:My Mix",
		"#EXTINF:201,Song One",
		"../Artists/A/Song One.opus",
		"#EXTINF:0,Root Song",
		"../Root Song.opus",
		"",
	}, "\n")

	if got != want {
		t.Errorf("RenderM3U:\n%q\nwant:\n%q", got, want)
	}
}

func TestPlaylistExportWritesRelativePaths(t *testing.T) {
	env, svc := newPlaylistEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Music")
	env.mustPutTrack(t, "t1", "Song One", "Music/Song One.opus")
	_ = folder

	if _, err := svc.Create(ctx, "My Mix"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddTrack(ctx, "My Mix", "t1"); err != nil {
		t.Fatal(err)
	}

	path, err := svc.Export(ctx, "My Mix")
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	wantPath := filepath.Join(env.paths.LibraryDir(), ExportDir, "My Mix.m3u")
	if path != wantPath {
		t.Errorf("export path = %q, want %q", path, wantPath)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}

	content := string(raw)
	if !strings.Contains(content, "../Music/Song One.opus") {
		t.Errorf("export does not contain library-relative path:\n%s", content)
	}

	// Idempotent re-export overwrites wholesale.
	if err := svc.RemoveTrack(ctx, "My Mix", "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Export(ctx, "My Mix"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "Song One") {
		t.Error("stale track survived re-export; render is not wholesale")
	}
}

// TestPlaylistAddTracksIsAtomic is the reason AddTracks exists. Adding rows
// one commit at a time would leave a partial playlist behind on failure, and
// a partial playlist is indistinguishable from a deliberate one.
func TestPlaylistAddTracksIsAtomic(t *testing.T) {
	env, svc := newPlaylistEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Music")
	artist, err := env.artists.Create(ctx, "", folder.ID, "An Artist")
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]TrackID, 0, 3)
	for i, name := range []string{"one", "two", "three"} {
		id := TrackID("t" + string(rune('1'+i)))
		if err := env.repos.tracks.Create(ctx, Track{
			ID:           id,
			Title:        name,
			ArtistID:     artist.ID,
			RelativePath: TrackPath("Music/" + name + ".opus"),
			SourceID:     "src" + name,
		}); err != nil {
			t.Fatalf("seed track %s: %v", name, err)
		}
		ids = append(ids, id)
	}

	playlist, err := svc.Create(ctx, "Atomic")
	if err != nil {
		t.Fatal(err)
	}

	// The last track does not exist, so the whole unit must roll back.
	err = svc.AddTracks(ctx, playlist.Name, []TrackID{ids[0], ids[1], "missing"})
	if err == nil {
		t.Fatal("AddTracks succeeded despite an unknown track")
	}

	entries, err := svc.Entries(ctx, playlist.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("playlist has %d entries after rollback, want 0: %v", len(entries), entries)
	}

	// The happy path still works, and order is preserved.
	if err := svc.AddTracks(ctx, playlist.Name, ids); err != nil {
		t.Fatalf("AddTracks: %v", err)
	}
	entries, err = svc.Entries(ctx, playlist.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	for i, e := range entries {
		if e.Track.ID != ids[i] {
			t.Errorf("position %d = %s, want %s", i, e.Track.ID, ids[i])
		}
	}
}

// TestPlaylistAddTracksDuplicateIsRejected covers the unique constraint: a
// matched set that contains the same track twice must not silently
// half-insert.
func TestPlaylistAddTracksDuplicateIsRejected(t *testing.T) {
	env, svc := newPlaylistEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Music")
	artist, err := env.artists.Create(ctx, "", folder.ID, "An Artist")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.repos.tracks.Create(ctx, Track{
		ID: "t1", Title: "one", ArtistID: artist.ID,
		RelativePath: TrackPath("Music/one.opus"), SourceID: "src1",
	}); err != nil {
		t.Fatal(err)
	}

	playlist, err := svc.Create(ctx, "Dupes")
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.AddTracks(ctx, playlist.Name, []TrackID{"t1", "t1"}); err == nil {
		t.Fatal("AddTracks accepted a duplicate track")
	}

	entries, err := svc.Entries(ctx, playlist.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("playlist has %d entries after rollback, want 0", len(entries))
	}
}
