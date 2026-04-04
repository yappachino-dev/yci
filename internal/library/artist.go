package library

import (
	"database/sql"
	"fmt"
)

type Artist struct {
	ID       string
	FolderID string
	Artist   string
}

func (l *Library) GetArtists() ([]Artist, error) {
	query := "SELECT id, display_name, folder_id FROM artists"

	rows, err := l.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artists []Artist
	for rows.Next() {
		var a Artist
		var folder sql.NullString

		if err := rows.Scan(&a.ID, &a.Artist, &folder); err != nil {
			return nil, err
		}

		if folder.Valid {
			a.FolderID = folder.String
		} else {
			a.FolderID = root
		}

		artists = append(artists, a)
	}

	return artists, nil
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
