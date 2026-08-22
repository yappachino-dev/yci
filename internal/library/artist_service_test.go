package library

import (
	"testing"
)

func TestArtistCreateLinksToSameSlugFolder(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	env.mustCreateFolder(t, "Radiohead")

	a, err := env.artists.Create(ctx, "", "", "Radiohead")
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}
	if a.FolderID != FolderID("radiohead") {
		t.Errorf("folder = %q, want existing same-slug folder", a.FolderID)
	}

	// Without a matching folder the artist falls back to root.
	other, err := env.artists.Create(ctx, "", "", "Portishead")
	if err != nil {
		t.Fatalf("create second artist: %v", err)
	}
	if other.FolderID != FolderID(RootID) {
		t.Errorf("fallback folder = %q, want root", other.FolderID)
	}
}
