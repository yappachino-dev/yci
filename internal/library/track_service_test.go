package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTrackMoveByArtist(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	folderA := env.mustCreateFolder(t, "Source")
	folderB := env.mustCreateFolder(t, "Target")

	artist, err := env.artists.Create(ctx, "", folderA.ID, "An Artist")
	if err != nil {
		t.Fatal(err)
	}

	// Register the track directly against its starting location.
	if err := env.repos.tracks.Create(ctx, Track{
		ID:           "t1",
		Title:        "Song",
		ArtistID:     artist.ID,
		RelativePath: "Source/song.opus",
		SourceID:     "src1",
	}); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	srcFile := filepath.Join(env.paths.LibraryDir(), "Source", "song.opus")
	if err := os.MkdirAll(filepath.Dir(srcFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcFile, []byte("audio:Song"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := env.artists.Update(ctx, artist.ID, ArtistUpdateParams{Folder: folderB.ID}); err != nil {
		t.Fatalf("move artist to another folder: %v", err)
	}

	moved, err := env.repos.tracks.ByID(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if moved.RelativePath != "Target/song.opus" {
		t.Errorf("relative path = %q, want %q", moved.RelativePath, "Target/song.opus")
	}

	if got := mustReadFile(t, filepath.Join(env.paths.LibraryDir(), "Target", "song.opus")); got != "audio:Song" {
		t.Errorf("moved file content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(env.paths.LibraryDir(), "Source", "song.opus")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("source file still exists: %v", err)
	}

	pending, err := env.actions.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("journal not drained: %d pending", len(pending))
	}
}

func TestTrackDeleteRemovesPhysicalFile(t *testing.T) {
	env := newLibraryEnv(t)

	env.mustPutTrack(t, "t1", "Song", "dir/song.opus")

	if err := env.tracks.Delete(t.Context(), "t1"); err != nil {
		t.Fatalf("delete track: %v", err)
	}

	if _, err := env.repos.tracks.ByID(t.Context(), "t1"); !errors.Is(err, ErrTrackNotFound) {
		t.Errorf("row still exists after delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.paths.LibraryDir(), "dir", "song.opus")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("physical file still exists: %v", err)
	}
}

func TestTrackByFolderRecursiveVsExact(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Music")

	seed := []struct{ rel string }{
		{"Music/direct.opus"},
		{"Music/nested/deep.opus"},
		{"Other/elsewhere.opus"},
	}
	for i, s := range seed {
		id := TrackID("t" + string(rune('0'+i)))
		title := filepath.Base(s.rel)
		if err := env.repos.tracks.Create(ctx, Track{
			ID:           id,
			Title:        title,
			RelativePath: TrackPath(s.rel),
		}); err != nil {
			t.Fatalf("seed track %q: %v", s.rel, err)
		}
	}

	exact, err := env.tracks.ByFolder(ctx, folder.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 1 || exact[0].RelativePath != "Music/direct.opus" {
		t.Errorf("exact matches = %+v, want only Music/direct.opus", exact)
	}

	recursive, err := env.tracks.ByFolder(ctx, folder.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(recursive) != 2 {
		t.Errorf("recursive matches = %d, want 2 (nested excluded Other/)", len(recursive))
	}
}
