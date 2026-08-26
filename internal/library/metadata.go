package library

import (
	"fmt"
	"os"

	"github.com/dhowden/tag"
)

type TagMetadataReader struct{}

func NewTagMetadataReader() *TagMetadataReader {
	return &TagMetadataReader{}
}

// Read takes no context: local tag parsing is millisecond-scale blocking
// I/O, so cancellation support would add a goroutine per call without
// observable benefit. Revisit if reads ever hit network or huge files.
func (r *TagMetadataReader) Read(path string) (TrackMetadata, error) {
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
