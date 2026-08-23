package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestFolderDeleteEnforcesEmptiness pins the deletion semantics: the
// non-empty invariant lives in the service, not the CLI, and --force
// cascades sub-folders while artists survive with cleared folder refs.
func TestFolderDeleteEnforcesEmptiness(t *testing.T) {
	t.Run("empty folder deletes without force", func(t *testing.T) {
		env := newLibraryEnv(t)
		f := env.mustCreateFolder(t, "Lonely")

		if err := env.folders.Delete(context.Background(), f.ID, DeleteOptions{}); err != nil {
			t.Fatalf("Delete returned error: %v", err)
		}
	})

	t.Run("folder with artist refuses without force", func(t *testing.T) {
		env := newLibraryEnv(t)
		parent := env.mustCreateFolder(t, "Parent")
		if _, err := env.artists.Create(context.Background(), "", parent.ID, "Artist"); err != nil {
			t.Fatalf("create artist: %v", err)
		}

		err := env.folders.Delete(context.Background(), parent.ID, DeleteOptions{})
		if !errors.Is(err, ErrFolderNotEmpty) {
			t.Fatalf("error = %v, want ErrFolderNotEmpty", err)
		}

		// Folder must still exist.
		if _, err := env.folders.ByID(context.Background(), parent.ID); err != nil {
			t.Fatalf("folder was deleted despite refusal: %v", err)
		}
	})

	t.Run("folder with sub-folder refuses without force", func(t *testing.T) {
		env := newLibraryEnv(t)
		parent := env.mustCreateFolder(t, "Parent")
		if _, err := env.folders.Create(context.Background(), "", parent.ID, "Child"); err != nil {
			t.Fatalf("create child folder: %v", err)
		}

		if err := env.folders.Delete(context.Background(), parent.ID, DeleteOptions{}); !errors.Is(err, ErrFolderNotEmpty) {
			t.Fatalf("error = %v, want ErrFolderNotEmpty", err)
		}
	})

	t.Run("force delete cascades folders and clears artist references", func(t *testing.T) {
		env := newLibraryEnv(t)
		ctx := context.Background()

		parent := env.mustCreateFolder(t, "Parent")
		child, err := env.folders.Create(ctx, "", parent.ID, "Child")
		if err != nil {
			t.Fatalf("create child folder: %v", err)
		}
		artist, err := env.artists.Create(ctx, "", parent.ID, "Artist")
		if err != nil {
			t.Fatalf("create artist: %v", err)
		}

		trackRel := "Parent/Artist/song.mp3"
		env.mustPutTrack(t, "track1", "Song", trackRel)

		if err := env.folders.Delete(ctx, parent.ID, DeleteOptions{Force: true}); err != nil {
			t.Fatalf("forced Delete returned error: %v", err)
		}

		if _, err := env.folders.ByID(ctx, child.ID); !errors.Is(err, ErrFolderNotFound) {
			t.Errorf("child folder error = %v, want ErrFolderNotFound (cascaded)", err)
		}

		got, err := env.artists.ByID(ctx, artist.ID)
		if err != nil {
			t.Fatalf("artist row should survive the cascade: %v", err)
		}
		if got.FolderID != "" {
			t.Errorf("artist folder ref = %q, want empty (FK ON DELETE SET NULL)", got.FolderID)
		}

		// The physical tree is intentionally left on disk; audit sees files
		// whose declared paths still resolve.
		if _, err := os.Stat(filepath.Join(env.paths.LibraryDir(), filepath.FromSlash(trackRel))); err != nil {
			t.Errorf("physical file missing after force delete: %v", err)
		}
	})
}
