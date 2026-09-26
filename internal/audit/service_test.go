package audit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yappachino-dev/yci/internal/library"
)

type fakeTrackProvider struct {
	tracks   []library.Track
	allErr   error
	byArtErr error
}

func (f *fakeTrackProvider) All(context.Context) ([]library.Track, error) {
	return f.tracks, f.allErr
}

func (f *fakeTrackProvider) ByArtistID(_ context.Context, _ library.ArtistID) ([]library.Track, error) {
	return nil, f.byArtErr
}

func (f *fakeTrackProvider) ByFolder(_ context.Context, _ library.FolderID, _ bool) ([]library.Track, error) {
	return f.tracks, nil
}

type fakeLibraryPaths struct{ dir string }

func (p fakeLibraryPaths) LibraryDir() string { return p.dir }

func newTestService(t *testing.T, provider *fakeTrackProvider) *Service {
	t.Helper()

	return NewService(provider, fakeLibraryPaths{dir: t.TempDir()})
}

func writeLibraryFile(t *testing.T, dir, relPath string) {
	t.Helper()

	abs := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestServiceMissingAll(t *testing.T) {
	dir := t.TempDir()
	writeLibraryFile(t, dir, "Artist/Album/present.opus")

	svc := newTestService(t, &fakeTrackProvider{
		tracks: []library.Track{
			{ID: "t1", Title: "Present", RelativePath: "Artist/Album/present.opus", SourceID: "presentvid1"},
			{ID: "t2", Title: "Missing", RelativePath: "Artist/Album/missing.opus", SourceID: "missingvi1"},
			// No SourceID: cannot be re-downloaded, never reported missing.
			{ID: "t3", Title: "Unsourceable", RelativePath: "Artist/Album/gone.opus"},
		},
	})
	svc.paths = fakeLibraryPaths{dir: dir}

	missing, err := svc.MissingAll(context.Background())
	if err != nil {
		t.Fatalf("MissingAll returned error: %v", err)
	}

	if len(missing) != 1 || missing[0].ID != "t2" {
		t.Errorf("missing = %+v, want only track t2", missing)
	}
}

func TestServicePropagatesStatFailure(t *testing.T) {
	dir := t.TempDir()
	writeLibraryFile(t, dir, "locked.opus")

	// Revoke read access on the library root so the stat itself fails
	// instead of reporting a clean miss.
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	svc := newTestService(t, &fakeTrackProvider{
		tracks: []library.Track{
			{ID: "t1", RelativePath: "locked.opus", SourceID: "someid12345"},
		},
	})
	svc.paths = fakeLibraryPaths{dir: dir}

	if _, err := svc.MissingAll(context.Background()); err == nil {
		t.Fatal("MissingAll expected error when stat fails with non-NotExist error")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Errorf("error = %v, want permission-denied chain", err)
	}
}

func TestServiceWrapsProviderErrors(t *testing.T) {
	wantErr := errors.New("disk on fire")
	svc := newTestService(t, &fakeTrackProvider{allErr: wantErr})

	if _, err := svc.MissingAll(context.Background()); !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want wrapped provider error", err)
	}
}

// TestServiceRejectsMissingRoot is the regression test for the failure that
// reported a whole library as missing when the root was absent, which made
// sync re-download every track after an unmounted drive or a bad
// library_dir. An unknown root must be an error, never a clean miss.
func TestServiceRejectsMissingRoot(t *testing.T) {
	svc := newTestService(t, &fakeTrackProvider{
		tracks: []library.Track{
			{ID: "t1", RelativePath: "Artist/a.opus", SourceID: "someid12345"},
		},
	})
	svc.paths = fakeLibraryPaths{dir: filepath.Join(t.TempDir(), "never-created")}

	for name, call := range map[string]func() error{
		"MissingAll": func() error {
			_, err := svc.MissingAll(context.Background())
			return err
		},
		"MissingByArtist": func() error {
			_, err := svc.MissingByArtist(context.Background(), "artist1")
			return err
		},
		"MissingByFolder": func() error {
			_, err := svc.MissingByFolder(context.Background(), "folder1", true)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			if !errors.Is(err, ErrLibraryRootUnavailable) {
				t.Errorf("error = %v, want ErrLibraryRootUnavailable", err)
			}
			if !errors.Is(err, os.ErrNotExist) {
				t.Errorf("error = %v, want the underlying cause preserved", err)
			}
			if !strings.Contains(err.Error(), "library_dir") {
				t.Errorf("error = %v, want a message naming the setting to check", err)
			}
		})
	}
}

func TestServiceRejectsRootThatIsAFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(root, []byte("oops"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := newTestService(t, &fakeTrackProvider{
		tracks: []library.Track{{ID: "t1", RelativePath: "a.opus", SourceID: "someid12345"}},
	})
	svc.paths = fakeLibraryPaths{dir: root}

	if _, err := svc.MissingAll(context.Background()); !errors.Is(err, ErrLibraryRootUnavailable) {
		t.Errorf("error = %v, want ErrLibraryRootUnavailable", err)
	}
}

// TestServiceRejectsRootBeforeReporting keeps the guard ahead of the scan:
// an absent root must not leak per-track results, since the caller would
// enqueue them.
func TestServiceRejectsRootBeforeReporting(t *testing.T) {
	svc := newTestService(t, &fakeTrackProvider{
		tracks: []library.Track{
			{ID: "t1", RelativePath: "a.opus", SourceID: "someid12345"},
			{ID: "t2", RelativePath: "b.opus", SourceID: "otherid1234"},
		},
	})
	svc.paths = fakeLibraryPaths{dir: filepath.Join(t.TempDir(), "gone")}

	missing, err := svc.MissingAll(context.Background())
	if err == nil {
		t.Fatal("MissingAll expected an error for an absent library root")
	}
	if len(missing) != 0 {
		t.Errorf("missing = %+v, want no tracks reported alongside the error", missing)
	}
}
