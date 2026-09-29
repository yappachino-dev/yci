package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/dhowden/tag"

	"github.com/yappachino-dev/yci/internal/probe"
)

// TrackMediaInfo is the probe-derived half of a track's metadata. Kept as a
// flat value so TrackCreateParams can carry it without importing probe.
type TrackMediaInfo struct {
	Duration   time.Duration
	Codec      string
	Bitrate    int64
	SampleRate int
}

// TagMetadataReader reads tag values in process, with no context.
//
// dhowden/tag is millisecond-scale in-memory parsing of a file already being
// read, so there is nothing to cancel. The cancellable part of metadata
// extraction is the probe, and that lives in FileMetadataReader.
type TagMetadataReader struct{}

func NewTagMetadataReader() *TagMetadataReader {
	return &TagMetadataReader{}
}

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

// FileMetadataReader is the production MetadataReader: tags from
// dhowden/tag, media properties from a Prober.
//
// The two are separate because they answer different questions and fail
// differently. Tags live in the file and are always available if the file is
// readable. Duration is derived from the encoded stream and needs a decoder,
// so it can be genuinely unavailable.
type FileMetadataReader struct {
	tags   *TagMetadataReader
	prober probe.Prober
}

func NewFileMetadataReader(prober probe.Prober) *FileMetadataReader {
	return &FileMetadataReader{tags: NewTagMetadataReader(), prober: prober}
}

func (r *FileMetadataReader) Read(ctx context.Context, path string) (TrackMetadata, error) {
	meta, err := r.tags.Read(path)
	if err != nil {
		return TrackMetadata{}, err
	}

	info, err := r.prober.Probe(ctx, path)
	switch {
	case err == nil:
		meta.Media = TrackMediaInfo{
			Duration:   info.Duration,
			Codec:      info.Codec,
			Bitrate:    info.Bitrate,
			SampleRate: info.SampleRate,
		}
	case isProbeUnavailable(err):
		// Degrade, never fail. An absent prober means duration is unknown,
		// which is not a reason to refuse a file the tags read cleanly.
		meta.MediaUnavailable = true
	case ctx.Err() != nil:
		// Cancellation is a real failure: the caller asked us to stop.
		return TrackMetadata{}, fmt.Errorf("probe %q: %w", path, ctx.Err())
	default:
		// A file that exists and has readable tags but will not probe is
		// odd but not fatal; record the problem and keep the import going.
		meta.MediaErr = err
	}

	return meta, nil
}

func isProbeUnavailable(err error) bool {
	return errors.Is(err, probe.ErrUnavailable)
}
