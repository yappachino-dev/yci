package library

import (
	"context"
	"errors"
	"testing"
)

// TestCreateInFolderMintsLeafAndLinks checks the whole point of the call:
// one invocation leaves a folder named after the artist, containing it.
func TestCreateInFolderMintsLeafAndLinks(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := context.Background()

	parent, err := env.folders.Create(ctx, "", "", FolderName("hololive"))
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	a, err := env.artists.CreateInFolder(ctx, parent.ID, ArtistName("Cecilia Immergreen"))
	if err != nil {
		t.Fatalf("CreateInFolder: %v", err)
	}

	if a.ID != "cecilia-immergreen" {
		t.Errorf("artist ID = %q, want the name slug", a.ID)
	}

	if a.FolderID != "cecilia-immergreen" {
		t.Errorf("artist folder = %q, want a folder named after the artist", a.FolderID)
	}

	leaf, err := env.folders.ByID(ctx, a.FolderID)
	if err != nil {
		t.Fatalf("leaf folder was not created: %v", err)
	}

	if leaf.ParentID != parent.ID {
		t.Errorf("leaf parent = %q, want %q", leaf.ParentID, parent.ID)
	}

	if leaf.Name != FolderName("Cecilia Immergreen") {
		t.Errorf("leaf name = %q, want the artist name", leaf.Name)
	}
}

// TestCreateInFolderIsAtomic is the regression guard. Without the
// transaction a failing artist insert leaves the leaf folder behind, which is
// exactly the debris the two-command flow was meant to avoid.
func TestCreateInFolderIsAtomic(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := context.Background()

	// Occupy the artist slug so the artist insert fails on the unique ID,
	// while the leaf folder id is still free.
	if _, err := env.artists.Create(ctx, "taken", "", ArtistName("Taken")); err != nil {
		t.Fatalf("seed artist: %v", err)
	}

	if _, err := env.folders.Create(ctx, "", "", FolderName("hololive")); err != nil {
		t.Fatalf("create parent: %v", err)
	}

	_, err := env.artists.CreateInFolder(ctx, "hololive", ArtistName("Taken"))
	if err == nil {
		t.Fatal("expected the duplicate artist slug to fail")
	}

	// The whole point: no orphan leaf folder survived the failure.
	exists, err := env.repos.folders.Exists(ctx, "taken")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}

	if exists {
		t.Error("leaf folder survived a failed artist create; the transaction is not atomic")
	}
}

// TestCreateInFolderRefusesExistingLeaf: --parent means "mint this folder".
// Reusing an existing one would silently ignore the parent.
func TestCreateInFolderRefusesExistingLeaf(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := context.Background()

	if _, err := env.folders.Create(ctx, "hololive", "", FolderName("hololive")); err != nil {
		t.Fatalf("create parent: %v", err)
	}

	if _, err := env.folders.Create(ctx, "existing", "hololive", FolderName("Existing")); err != nil {
		t.Fatalf("seed leaf: %v", err)
	}

	_, err := env.artists.CreateInFolder(ctx, "hololive", ArtistName("Existing"))
	if !errors.Is(err, ErrFolderAlreadyExists) {
		t.Fatalf("got %v, want ErrFolderAlreadyExists", err)
	}
}

func TestCreateInFolderRejectsMissingParent(t *testing.T) {
	env := newLibraryEnv(t)

	_, err := env.artists.CreateInFolder(context.Background(), "no-such-folder", ArtistName("Nobody"))
	if !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("got %v, want ErrFolderNotFound", err)
	}
}

// TestLeafSlugMatchesArtistSlug pins the assumption CreateInFolder rests on:
// the artist ID and the leaf folder ID are both nameToSlug of the same name.
func TestLeafSlugMatchesArtistSlug(t *testing.T) {
	for _, name := range []string{"Cecilia Immergreen", "Re:Union", "米卡", "A/B & C"} {
		got := ArtistName(name).ToSlugID()
		want := FolderName(name).ToSlugID()

		if string(got) != string(want) {
			t.Errorf("%q: artist slug %q != folder slug %q", name, got, want)
		}
	}
}
