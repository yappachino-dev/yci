package library

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dhowden/tag"
)

type artistMap map[string]Artist

func (l *Library) ProcessTempFolder() error {
	files, err := os.ReadDir(l.Paths.Staging)
	if err != nil {
		return err
	}

	for _, file := range files {
		f, err := os.Open(filepath.Join(l.Paths.Staging, file.Name()))
		if err != nil {
			return err
		}
		defer f.Close()

		m, err := tag.ReadFrom(f)
		if err != nil {
			return fmt.Errorf("could not read metadata: %w", err)
		}

		fmt.Printf("%s\n Title: %s\n Artist: %s\n Album: %s\n", file.Name(), m.Title(), m.Artist(), m.Album())
	}

	return nil
}
