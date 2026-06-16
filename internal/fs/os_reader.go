package fs

import (
	"os"
)

type OSDiskReader struct{}

func NewOSDiskReader() *OSDiskReader {
	return &OSDiskReader{}
}

func (r *OSDiskReader) ReadDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
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
