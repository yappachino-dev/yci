package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFolderServiceCreateDefaults(t *testing.T) {
	env := newLibraryEnv(t)

	f, err := env.folders.Create(t.Context(), "", "", "My Folder")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	if f.ID != "my-folder" {
		t.Errorf("auto slug ID = %q, want %q", f.ID, "my-folder")
	}
	if f.ParentID != FolderID(RootID) {
		t.Errorf("default parent = %q, want root", f.ParentID)
	}

	stored, err := env.folders.ByID(t.Context(), f.ID)
	if err != nil {
		t.Fatalf("reload folder: %v", err)
	}
	if stored.Name != "My Folder" {
		t.Errorf("stored name = %q, want %q", stored.Name, "My Folder")
	}
}

func TestFolderResolvePath(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	parent := env.mustCreateFolder(t, "Music")
	child := Folder{ID: "metal", Name: "Metal", ParentID: parent.ID}
	if _, err := env.folders.Create(ctx, child.ID, child.ParentID, child.Name); err != nil {
		t.Fatalf("create nested folder: %v", err)
	}

	got, err := env.folders.ResolvePath(ctx, "metal")
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	if got != "Music/Metal" {
		t.Errorf("path = %q, want %q", got, "Music/Metal")
	}

	rootPath, err := env.folders.ResolvePath(ctx, FolderID(RootID))
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	if rootPath != "" {
		t.Errorf("root path = %q, want empty", rootPath)
	}
}

func TestFolderUpdateRenameRewritesTracksAndMovesDirectory(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Old Name")
	env.mustPutTrack(t, "t1", "Song", "Old Name/song.opus")

	updated, err := env.folders.Update(ctx, folder.ID, FolderUpdateParams{Name: "New Name"})
	if err != nil {
		t.Fatalf("update folder: %v", err)
	}
	if updated.ID != "old-name" || updated.Name != "New Name" {
		t.Errorf("updated = {%s %s}, want {old-name New Name}", updated.ID, updated.Name)
	}

	tracks, err := env.tracks.ByFolder(ctx, folder.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].RelativePath != "New Name/song.opus" {
		t.Fatalf("track paths not rewritten: %+v", tracks)
	}

	movedTo := filepath.Join(env.paths.LibraryDir(), "New Name", "song.opus")
	if got := mustReadFile(t, movedTo); got != "audio:Song" {
		t.Errorf("moved file content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(env.paths.LibraryDir(), "Old Name")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old directory still exists: %v", err)
	}

	pending, err := env.actions.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("journal not drained after post-commit flush: %d pending", len(pending))
	}
}

func TestFolderUpdateRejectsCyclicParent(t *testing.T) {
	env := newLibraryEnv(t)

	// Display names deliberately differ from slug IDs — this is the case
	// that used to slip past the cycle check.
	parent := env.mustCreateFolder(t, "Parent Folder")
	child := Folder{ID: "child-folder", Name: "Child Folder", ParentID: parent.ID}
	if _, err := env.folders.Create(t.Context(), child.ID, child.ParentID, child.Name); err != nil {
		t.Fatal(err)
	}

	_, err := env.folders.Update(t.Context(), parent.ID, FolderUpdateParams{Parent: child.ID})
	if !errors.Is(err, ErrCyclicParent) {
		t.Fatalf("err = %v, want ErrCyclicParent (cycle via display-name/ID mismatch)", err)
	}
}

func TestFolderUpdateRejectsMissingParent(t *testing.T) {
	env := newLibraryEnv(t)

	folder := env.mustCreateFolder(t, "Solo")

	_, err := env.folders.Update(t.Context(), folder.ID, FolderUpdateParams{Parent: "ghost"})
	if !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("err = %v, want ErrParentNotFound", err)
	}
}

func TestFolderNamesSanitizedInSlugsAndPaths(t *testing.T) {
	env := newLibraryEnv(t)
	ctx := t.Context()

	folder := env.mustCreateFolder(t, "Weird/Name")

	if folder.ID != "weird_name" {
		t.Errorf("slug ID = %q, want %q (no path separators)", folder.ID, "weird_name")
	}

	got, err := env.folders.ResolvePath(ctx, folder.ID)
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	if got != "Weird_Name" {
		t.Errorf("path = %q, want %q (illegal characters replaced)", got, "Weird_Name")
	}

	predicted, err := env.folders.PredictPath(ctx, folder.ID, "Sub:Dir")
	if err != nil {
		t.Fatalf("predict path: %v", err)
	}
	if predicted != "Weird_Name/Sub_Dir" {
		t.Errorf("predicted = %q, want %q", predicted, "Weird_Name/Sub_Dir")
	}
}

func TestFolderNameOfOnlyIllegalCharactersSanitized(t *testing.T) {
	env := newLibraryEnv(t)

	f, err := env.folders.Create(t.Context(), "", "", "///")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	got, err := env.folders.ResolvePath(t.Context(), f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != "___" {
		t.Errorf("path = %q, want %q (illegal characters replaced)", got, "___")
	}
}
