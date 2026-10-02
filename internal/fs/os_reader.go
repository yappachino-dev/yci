package fs

import (
	"fmt"
	"os"
)

// OSDiskReader is the production DirReader, backed by os.ReadDir.
var _ DirReader = (*OSDiskReader)(nil)

// DirReader lists the names of the files directly inside a directory.
//
// It lives beside its implementation so every consumer declares the same
// contract rather than a local copy of it.
//
// Error contract: a not-exist failure satisfies errors.Is(err, fs.ErrNotExist)
// through the wrapped stdlib error; callers decide whether absence is fatal.
type DirReader interface {
	ReadDir(path string) ([]string, error)
}

// OSDiskReader is the production DirReader.
//
// It skips subdirectories, so a consumer listing a flat directory does not
// have to filter them again.
type OSDiskReader struct{}

func NewOSDiskReader() *OSDiskReader {
	return &OSDiskReader{}
}

func (r *OSDiskReader) ReadDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read directory %q: %w", path, err)
	}

	files := make([]string, 0, len(entries))

	for _, f := range entries {
		if f.IsDir() {
			continue
		}

		files = append(files, f.Name())
	}

	return files, nil
}
