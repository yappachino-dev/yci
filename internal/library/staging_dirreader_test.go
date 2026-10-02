package library

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/fs"
	"github.com/yappachino-dev/yci/internal/probe"
)

// stagingDirReader stands in for the disk so tests can drive ProcessStaging
// without one. It exists to prove the seam is real: if ProcessStaging called
// os.ReadDir directly, this type could not be injected and these tests could
// not be written.
type stagingDirReader struct {
	names []string
	calls int
	err   error
}

func (r *stagingDirReader) ReadDir(string) ([]string, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return r.names, nil
}

func TestProcessStagingReadsThroughTheInjectedDirReader(t *testing.T) {
	env := newLibraryEnv(t)
	reader := &stagingDirReader{}

	env.staging = NewStagingService(StagingServiceConfig{
		TrackCreator:   env.tracks,
		ArtistReader:   env.repos.artists,
		FolderResolver: env.folders,
		Linker:         env.channels,
		MetaReader:     NewFileMetadataReader(probe.NewFFprobe()),
		DiskMover:      db.NewJournaledMover(db.NewFileActionRepository(env.queries), fs.NewOSDiskMover()),
		Transactor:     env.txr,
		Paths:          env.paths,
		DirReader:      reader,
	})

	// Nothing on disk, but the injected reader claims there is a file.
	reader.names = []string{"a track.opus"}

	if err := env.staging.ProcessStaging(context.Background(), nil); err != nil {
		t.Fatalf("ProcessStaging: %v", err)
	}

	if reader.calls != 1 {
		t.Errorf("ReadDir called %d times, want 1", reader.calls)
	}
}

func TestProcessStagingDefaultsToTheRealReader(t *testing.T) {
	env := newLibraryEnv(t)

	// No DirReader in the config: the service must supply its own.
	env.staging = NewStagingService(StagingServiceConfig{
		TrackCreator:   env.tracks,
		ArtistReader:   env.repos.artists,
		FolderResolver: env.folders,
		Linker:         env.channels,
		MetaReader:     NewFileMetadataReader(probe.NewFFprobe()),
		DiskMover:      db.NewJournaledMover(db.NewFileActionRepository(env.queries), fs.NewOSDiskMover()),
		Transactor:     env.txr,
		Paths:          env.paths,
	})

	if env.staging.dirReader == nil {
		t.Fatal("dirReader is nil: a nil DirReader would panic on first use")
	}

	// The real staging directory is empty, so this must succeed rather than
	// fail on a missing directory.
	if err := env.staging.ProcessStaging(context.Background(), nil); err != nil {
		t.Fatalf("ProcessStaging with the default reader: %v", err)
	}
}

// A missing staging directory has to surface, not read as "nothing to do":
// that is the difference between a quiet library and a broken download.
func TestProcessStagingReportsAMissingDirectory(t *testing.T) {
	env := newLibraryEnv(t)
	env.staging = NewStagingService(StagingServiceConfig{
		TrackCreator:   env.tracks,
		ArtistReader:   env.repos.artists,
		FolderResolver: env.folders,
		Linker:         env.channels,
		MetaReader:     NewFileMetadataReader(probe.NewFFprobe()),
		DiskMover:      db.NewJournaledMover(db.NewFileActionRepository(env.queries), fs.NewOSDiskMover()),
		Transactor:     env.txr,
		Paths:          env.paths,
		DirReader:      &stagingDirReader{err: errors.New("boom")},
	})
	if err := env.staging.ProcessStaging(context.Background(), nil); err == nil {
		t.Fatal("want an error from the directory read, got nil")
	} else if !strings.Contains(err.Error(), "read staging directory") {
		t.Errorf("error lost its context: %v", err)
	}
}
