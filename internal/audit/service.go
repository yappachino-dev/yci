package audit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/yappachino-dev/yci/internal/library"
)

type TrackProvider interface {
	All(ctx context.Context) ([]library.Track, error)
	ByArtistID(ctx context.Context, id library.ArtistID) ([]library.Track, error)
	ByFolder(ctx context.Context, id library.FolderID, recursive bool) ([]library.Track, error)
}

// LibraryPaths supplies the library root directory. Satisfied by
// fs.AppPaths.
type LibraryPaths interface {
	LibraryDir() string
}

// ErrLibraryRootUnavailable reports that the library root cannot be read, so
// per-track presence is unknowable.
var ErrLibraryRootUnavailable = errors.New("library root unavailable")

type ServiceConfig struct {
	TrackProvider TrackProvider
	Paths         LibraryPaths
}

type Service struct {
	trackProvider TrackProvider
	paths         LibraryPaths
}

// NewService takes its two collaborators directly: at fewer than three
// dependencies a config struct adds ritual without value (style-guide §2).
func NewService(tracks TrackProvider, paths LibraryPaths) *Service {
	return &Service{
		trackProvider: tracks,
		paths:         paths,
	}
}

func (s *Service) MissingAll(ctx context.Context) ([]library.Track, error) {
	tracks, err := s.trackProvider.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all tracks: %w", err)
	}

	return s.filterMissing(tracks)
}

func (s *Service) MissingByArtist(ctx context.Context, id library.ArtistID) ([]library.Track, error) {
	tracks, err := s.trackProvider.ByArtistID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list artist tracks: %w", err)
	}

	return s.filterMissing(tracks)
}

func (s *Service) MissingByFolder(ctx context.Context, id library.FolderID, recursive bool) ([]library.Track, error) {
	tracks, err := s.trackProvider.ByFolder(ctx, id, recursive)
	if err != nil {
		return nil, fmt.Errorf("list folder tracks: %w", err)
	}

	return s.filterMissing(tracks)
}

// requireRoot rejects an absent or non-directory library root before any
// per-track stat. Without this check every track path fails with
// fs.ErrNotExist and the whole library is reported missing, which turns an
// unmounted drive or a mistyped library_dir into a full re-download.
func (s *Service) requireRoot() error {
	dir := s.paths.LibraryDir()

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%w %q: %w; check the library_dir setting", ErrLibraryRootUnavailable, dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w %q: not a directory; check the library_dir setting", ErrLibraryRootUnavailable, dir)
	}

	return nil
}

// filterMissing keeps tracks whose physical file cannot be stat'd. Tracks
// without a SourceID are skipped — they cannot be re-downloaded.
func (s *Service) filterMissing(tracks []library.Track) ([]library.Track, error) {
	if err := s.requireRoot(); err != nil {
		return nil, err
	}

	var missing []library.Track
	for _, t := range tracks {
		if t.SourceID == "" {
			continue
		}

		absPath := filepath.Join(s.paths.LibraryDir(), filepath.FromSlash(string(t.RelativePath)))
		if _, err := os.Stat(absPath); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("stat track %q: %w", t.ID, err)
			}

			missing = append(missing, t)
		}
	}

	return missing, nil
}
