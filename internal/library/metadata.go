package library

import (
	"context"
	"fmt"
	"os"

	"github.com/dhowden/tag"
)

type TagMetadataReader struct{}

func NewTagMetadataReader() *TagMetadataReader {
	return &TagMetadataReader{}
}

func (r *TagMetadataReader) Read(ctx context.Context, path string) (TrackMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return TrackMetadata{}, fmt.Errorf("read tags from %q: %w", path, err)
	}

	return TrackMetadata{
		Title:  m.Title(),
		Album:  m.Album(),
		Artist: m.Artist(),
	}, nil
}
