package library

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dhowden/tag"
)

type Artist struct {
	ID       string
	FolderID string
	Artist   string
}

type artistMap map[string]Artist

func (l *Library) ProcessTempFolder() error {
	files, err := os.ReadDir(l.Paths.Temp)
	if err != nil {
		return err
	}

	for _, file := range files {
		f, err := os.Open(filepath.Join(l.Paths.Temp, file.Name()))
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

func (l *Library) CreateArtistEntry(id string, artist string) {
	l.artistIdx[id] = Artist{id, "", artist}
}

func (l *Library) MapToFolder(artistID string, folderID string) error {
	artist, exists := l.artistIdx[artistID]
	if !exists {
		return fmt.Errorf("artist %q does not exist", artistID)
	}

	artist.FolderID = folderID

	l.artistIdx[artistID] = artist

	return nil
}
