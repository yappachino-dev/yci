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

type ServiceConfig struct {
	TrackProvider TrackProvider
	Paths         LibraryPaths
}

type Service struct {
	trackProvider TrackProvider
	paths         LibraryPaths
}

func NewService(cfg ServiceConfig) *Service {
	return &Service{
		trackProvider: cfg.TrackProvider,
		paths:         cfg.Paths,
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

// filterMissing keeps tracks whose physical file cannot be stat'd. Tracks
// without a SourceID are skipped — they cannot be re-downloaded.
func (s *Service) filterMissing(tracks []library.Track) ([]library.Track, error) {
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
