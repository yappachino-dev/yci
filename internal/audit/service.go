package audit

import (
	"context"
	"os"
	"path/filepath"

	"playlist-manager/internal/library"
)

type TrackProvider interface {
	All(ctx context.Context) ([]library.Track, error)
	ByArtistID(ctx context.Context, id library.ArtistID) ([]library.Track, error)
	ByFolder(ctx context.Context, id library.FolderID, recursive bool) ([]library.Track, error)
}

type LibraryConfig interface {
	LibraryDir() string
}

type Config struct {
	TrackProvider TrackProvider
	Config        LibraryConfig
}

type Service struct {
	trackProvider TrackProvider
	config        LibraryConfig
}

func NewService(cfg Config) *Service {
	return &Service{
		trackProvider: cfg.TrackProvider,
		config:        cfg.Config,
	}
}

func (s *Service) MissingAll(ctx context.Context) ([]library.Track, error) {
	tracks, err := s.trackProvider.All(ctx)
	if err != nil {
		return nil, err
	}

	return s.filterMissing(tracks)
}

func (s *Service) MissingByArtist(ctx context.Context, id library.ArtistID) ([]library.Track, error) {
	tracks, err := s.trackProvider.ByArtistID(ctx, id)
	if err != nil {
		return nil, err
	}

	return s.filterMissing(tracks)
}

func (s *Service) MissingByFolder(ctx context.Context, id library.FolderID, recursive bool) ([]library.Track, error) {
	tracks, err := s.trackProvider.ByFolder(ctx, id, recursive)
	if err != nil {
		return nil, err
	}

	return s.filterMissing(tracks)
}

func (s *Service) filterMissing(tracks []library.Track) ([]library.Track, error) {
	var missing []library.Track
	for _, t := range tracks {
		if t.SourceID == "" {
			continue
		}

		absPath := filepath.Join(s.config.LibraryDir(), filepath.FromSlash(string(t.RelativePath)))
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			missing = append(missing, t)
		}
	}

	return missing, nil
}
