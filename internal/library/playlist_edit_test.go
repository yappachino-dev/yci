package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRenameMovesTheExportedFile(t *testing.T) {
	env, ids := seedPlaylist(t, "comet / 星街すいせい(official)")
	ctx := context.Background()

	if _, err := env.playlists.CreateFromSeed(ctx, "Old Name", ids[0], []string{"星街すいせい"}, MatchAll, ids); err != nil {
		t.Fatal(err)
	}

	oldPath, err := env.playlists.Export(ctx, "Old Name")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("expected an exported file: %v", err)
	}

	result, err := env.playlists.Rename(ctx, "Old Name", "New Name")
	if err != nil {
		t.Fatal(err)
	}

	if !result.ExportMoved {
		t.Error("the exported file was not renamed")
	}
	if result.OrphanedExport != "" {
		t.Errorf("unexpected orphan: %s", result.OrphanedExport)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("the old exported file still exists")
	}

	// The new file must be a real, readable playlist, not just a moved name.
	wantPath := filepath.Join(env.paths.LibraryDir(), ExportDir, "New Name.m3u")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected the file to have followed the rename: %v", err)
	}

	if _, err := env.playlists.ByName(ctx, "New Name"); err != nil {
		t.Errorf("playlist not found under its new name: %v", err)
	}
}

func TestRenameWithNothingExportedReportsNoOrphan(t *testing.T) {
	env, ids := seedPlaylist(t, "comet / 星街すいせい(official)")
	ctx := context.Background()

	if _, err := env.playlists.CreateFromSeed(ctx, "A", ids[0], []string{"星街すいせい"}, MatchAll, ids); err != nil {
		t.Fatal(err)
	}

	result, err := env.playlists.Rename(ctx, "A", "B")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExportMoved {
		t.Error("claimed to move an export that never existed")
	}
	if result.OrphanedExport != "" {
		t.Errorf("unexpected orphan: %s", result.OrphanedExport)
	}
}

func TestRenameCaseOnlyIsNotAConflict(t *testing.T) {
	env, ids := seedPlaylist(t, "comet / 星街すいせい(official)")
	ctx := context.Background()

	if _, err := env.playlists.CreateFromSeed(ctx, "gym", ids[0], []string{"星街すいせい"}, MatchAll, ids); err != nil {
		t.Fatal(err)
	}

	// The unique constraint is COLLATE NOCASE, so this collides with the
	// row's own name. It must not be reported as "already exists".
	result, err := env.playlists.Rename(ctx, "gym", "Gym")
	if err != nil {
		t.Fatalf("case-only rename failed: %v", err)
	}
	if result.NewName != "Gym" {
		t.Errorf("NewName = %q, want Gym", result.NewName)
	}
}

func TestRenameToAnExistingNameIsRefused(t *testing.T) {
	env, ids := seedPlaylist(t, "comet / 星街すいせい(official)")
	ctx := context.Background()

	if _, err := env.playlists.CreateFromSeed(ctx, "Taken", ids[0], []string{"星街すいせい"}, MatchAll, ids); err != nil {
		t.Fatal(err)
	}
	if _, err := env.playlists.Create(ctx, "Other"); err != nil {
		t.Fatal(err)
	}

	if _, err := env.playlists.Rename(ctx, "Other", "Taken"); !errors.Is(err, ErrPlaylistAlreadyExists) {
		t.Errorf("Rename error = %v, want ErrPlaylistAlreadyExists", err)
	}

	// The original must survive the failed rename.
	if _, err := env.playlists.ByName(ctx, "Other"); err != nil {
		t.Errorf("the source playlist was lost: %v", err)
	}
}

func TestRenameRejectsEmptyName(t *testing.T) {
	env, _ := seedPlaylist(t, "comet")
	ctx := context.Background()

	if _, err := env.playlists.Create(ctx, "Keep"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.playlists.Rename(ctx, "Keep", "   "); err == nil {
		t.Error("renamed to a blank name")
	}
}

func TestRenameMissingPlaylist(t *testing.T) {
	env, _ := seedPlaylist(t, "comet")

	if _, err := env.playlists.Rename(context.Background(), "Nope", "Other"); !errors.Is(err, ErrPlaylistNotFound) {
		t.Errorf("Rename error = %v, want ErrPlaylistNotFound", err)
	}
}

func TestSetMatchModePersistsAndDefaultsToAll(t *testing.T) {
	env, ids := seedPlaylist(t,
		"comet / 星街すいせい(official)",
		"something with Mori only",
	)
	ctx := context.Background()

	if _, err := env.playlists.CreateFromSeed(ctx, "Mixed", ids[0], []string{"Mori", "Calliope"}, MatchAll, nil); err != nil {
		t.Fatal(err)
	}

	p, err := env.playlists.ByName(ctx, "Mixed")
	if err != nil {
		t.Fatal(err)
	}
	if p.MatchMode != MatchAll {
		t.Errorf("default MatchMode = %q, want %q", p.MatchMode, MatchAll)
	}

	if err := env.playlists.SetMatchMode(ctx, "Mixed", MatchAny); err != nil {
		t.Fatal(err)
	}

	p, err = env.playlists.ByName(ctx, "Mixed")
	if err != nil {
		t.Fatal(err)
	}
	if p.MatchMode != MatchAny {
		t.Errorf("MatchMode = %q, want %q", p.MatchMode, MatchAny)
	}

	// A row written before the mode column existed reads back as AND, so an
	// old recipe does not silently change meaning.
	if _, err := env.playlists.Create(ctx, "Handmade"); err != nil {
		t.Fatal(err)
	}
	hand, err := env.playlists.ByName(ctx, "Handmade")
	if err != nil {
		t.Fatal(err)
	}
	if hand.MatchMode != MatchAll {
		t.Errorf("hand-built playlist MatchMode = %q, want %q", hand.MatchMode, MatchAll)
	}
}

func TestSetMatchModeRefusesUnseededPlaylist(t *testing.T) {
	env, _ := seedPlaylist(t, "comet")
	ctx := context.Background()

	if _, err := env.playlists.Create(ctx, "Handmade"); err != nil {
		t.Fatal(err)
	}

	err := env.playlists.SetMatchMode(ctx, "Handmade", MatchAny)
	var noRecipe *NoRecipeError
	if !errors.As(err, &noRecipe) {
		t.Errorf("SetMatchMode error = %v, want NoRecipeError", err)
	}
}

func TestMatchModeChangeIsVisibleOnlyAfterReseed(t *testing.T) {
	env, ids := seedPlaylist(t,
		"comet / 星街すいせい(official)",
		"unrelated / 星街すいせい(official)",
	)
	ctx := context.Background()

	if _, err := env.playlists.CreateFromSeed(ctx, "Wide", ids[0], []string{"星街すいせい", "Calliope"}, MatchAll, ids); err != nil {
		t.Fatal(err)
	}

	// Under AND, the track without "Calliope" is in the playlist by seed.
	entries, err := env.playlists.Entries(ctx, "Wide")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("seeded membership = %d, want 2", len(entries))
	}

	// Switching to ANY must not touch membership on its own.
	if err := env.playlists.SetMatchMode(ctx, "Wide", MatchAny); err != nil {
		t.Fatal(err)
	}

	entries, err = env.playlists.Entries(ctx, "Wide")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("SetMatchMode changed membership to %d entries; it must not write", len(entries))
	}

	// The plan shows the difference; applying it is a separate step.
	report, err := env.playlists.ReseedPlan(ctx, "Wide")
	if err != nil {
		t.Fatal(err)
	}
	if report.MatchMode != MatchAny {
		t.Errorf("report MatchMode = %q, want %q", report.MatchMode, MatchAny)
	}
}
