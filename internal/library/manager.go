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

func (l *Library) AddArtist(id string, name string, folderID string) error {
	query := "INSERT INTO artists (id, display_name, folder_id) VALUES (?, ?, ?)"

	var folder any = folderID
	if folderID == "" || folderID == "root" {
		folder = nil
	}

	if _, err := l.db.Exec(query, id, name, folder); err != nil {
		return fmt.Errorf("failed to add %q artist: %w", id, err)
	}

	return nil
}

func (l *Library) RemoveArtist(id string) error {
	res, err := l.db.Exec("DELETE FROM artists WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to remove artist %q: %w", id, err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("artist %q does not exist", id)
	}

	return nil
}

func (l *Library) MapToFolder(artistID string, folderID string) error {
	var folder any = folderID
	if folderID == "" || folderID == "root" {
		folder = nil
	} else {
		var exists bool

		err := l.db.QueryRow("SELECT EXISTS(SELECT 1 FROM folders WHERE id = ?)", folderID).Scan(&exists)
		if err != nil || !exists {
			return fmt.Errorf("target folder %q does not exist", folderID)
		}
	}

	res, err := l.db.Exec("UPDATE artists SET folder_id = ? WHERE id = ?", folder, artistID)
	if err != nil {
		return fmt.Errorf("failed to map artist %q to folder %q: %w", artistID, folderID, err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("artist %q does not exist", artistID)
	}

	return nil
}
