package library

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

type Artist struct {
	ID       string
	FolderID string
	Artist   string
}

func (l *Library) GetArtist(id string) (Artist, error) {
	var a Artist
	var folder sql.NullString

	query := "SELECT id, display_name, folder_id FROM artists WHERE id = ?"
	err := l.db.QueryRow(query, id).Scan(&a.ID, &a.Artist, &folder)
	if err != nil {
		return a, err
	}

	if folder.Valid {
		a.FolderID = folder.String
	} else {
		a.FolderID = RootID
	}

	return a, nil
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
			a.FolderID = RootID
		}

		artists = append(artists, a)
	}

	return artists, nil
}

func (l *Library) GetArtistPath(artistID string) (string, error) {
	var name string
	var folderID sql.NullString

	err := l.db.QueryRow("SELECT display_name, folder_id FROM artists WHERE id = ?", artistID).Scan(&name, &folderID)
	if err != nil {
		return "", fmt.Errorf("database error resolving artist %q: %w", artistID, err)
	}

	targetFolder := RootID
	if folderID.Valid {
		targetFolder = folderID.String
	}

	folderPath, err := l.GetFolderPath(targetFolder)
	if err != nil {
		return "", err
	}

	if folderPath == "" {
		return name, nil
	}
	return folderPath + "/" + name, nil
}

func (l *Library) buildArtistPath(a Artist) (string, error) {
	folderPath, err := l.GetFolderPath(a.FolderID)
	if err != nil {
		return "", fmt.Errorf("could not resolve new folder path: %w", err)
	}

	if folderPath == "" {
		return a.Artist, nil
	}
	return folderPath + "/" + a.Artist, nil
}

func (l *Library) AddArtist(a Artist) error {
	query := "INSERT INTO artists (id, display_name, folder_id) VALUES (?, ?, ?)"

	var folder any = a.FolderID
	if a.FolderID == "" || a.FolderID == RootID {
		folder = nil
	}

	if _, err := l.db.Exec(query, a.ID, a.Artist, folder); err != nil {
		return fmt.Errorf("failed to add %q artist: %w", a.ID, err)
	}

	return nil
}

func (l *Library) RemoveArtist(id string) error {
	res, err := l.db.Exec("DELETE FROM artists WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete artist %q: %w", id, err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("artist %q not found", id)
	}

	return nil
}

func (l *Library) UpdateArtist(oldID string, newArtist Artist) error {
	oldRelativePath, err := l.GetArtistPath(oldID)
	if err != nil {
		return err
	}

	newRelativePath, err := l.buildArtistPath(newArtist)
	if err != nil {
		return err
	}

	oldAbsPath := filepath.Join(l.Paths.Library, oldRelativePath)
	newAbsPath := filepath.Join(l.Paths.Library, newRelativePath)

	if oldAbsPath != newAbsPath {
		os.MkdirAll(filepath.Dir(newAbsPath), 0o755)

		if err := os.Rename(oldAbsPath, newAbsPath); err != nil {
			return fmt.Errorf("failed to move artist folder on disk: %w", err)
		}
	}

	query := `UPDATE artists SET id = ?, display_name = ?, folder_id = ? WHERE id = ?`
	if _, err := l.db.Exec(query, newArtist.ID, newArtist.Artist, newArtist.FolderID, oldID); err != nil {
		return fmt.Errorf("failed to update artist in db: %w", err)
	}

	trackQuery := `UPDATE tracks SET relative_path = REPLACE(relative_path, ?, ?) WHERE relative_path LIKE ?`

	oldPrefix := oldRelativePath + "/"
	newPrefix := newRelativePath + "/"

	if _, err := l.db.Exec(trackQuery, oldPrefix, newPrefix, oldPrefix+"%"); err != nil {
		return fmt.Errorf("failed to update track paths: %w", err)
	}

	return nil
}
