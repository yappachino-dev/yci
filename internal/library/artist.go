package library

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Artist struct {
	ID       string
	FolderID string
	Artist   string
}

func scanArtist(s scannable) (Artist, error) {
	var a Artist
	var folderID sql.NullString

	err := s.Scan(&a.ID, &a.Artist, &folderID)
	a.FolderID = fromNull(folderID, RootID)

	return a, err
}

func (l *Library) GetArtist(id string) (Artist, error) {
	query := `SELECT id, display_name, folder_id FROM artists WHERE id = ?`

	a, err := scanArtist(l.db.QueryRow(query, id))

	return a, l.wrapErr(err, "artist", id)
}

func (l *Library) GetArtists() ([]Artist, error) {
	query := `SELECT id, display_name, folder_id FROM artists`

	rows, err := l.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query artists: %w", err)
	}
	defer rows.Close()

	var artists []Artist
	for rows.Next() {
		a, err := scanArtist(rows)
		if err != nil {
			return nil, err
		}
		artists = append(artists, a)
	}

	return artists, nil
}

func (l *Library) AddArtist(a Artist) error {
	query := `INSERT INTO artists (id, display_name, folder_id) VALUES (?, ?, ?)`

	_, err := l.db.Exec(query,
		strings.TrimSpace(a.ID),
		strings.TrimSpace(a.Artist),
		toNull(a.FolderID),
	)
	if err != nil {
		return fmt.Errorf("failed to add artist %q: %w", a.ID, err)
	}

	return nil
}

func (l *Library) RemoveArtist(id string) error {
	res, err := l.db.Exec("DELETE FROM artists WHERE id = ?", id)
	if err != nil {
		return l.wrapErr(err, "artist", id)
	}

	if rows, _ := res.RowsAffected(); rows == 0 {
		return l.wrapErr(sql.ErrNoRows, "artist", id)
	}

	return nil
}

func (l *Library) UpdateArtist(oldID string, updated Artist) error {
	oldArtist, err := l.GetArtist(oldID)
	if err != nil {
		return err
	}

	tx, err := l.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if oldArtist.FolderID != updated.FolderID {
		newRel, err := l.ResolveFolderPath(updated.FolderID)
		if err != nil {
			return fmt.Errorf("failed to resolve new folder path: %w", err)
		}

		rows, err := tx.Query(`SELECT id, relative_path FROM tracks WHERE artist_id = ?`, oldID)
		if err != nil {
			return fmt.Errorf("failed to query tracks for movement: %w", err)
		}

		type trackMove struct {
			ID      string
			OldPath string
			NewPath string
		}
		var moves []trackMove

		for rows.Next() {
			var tID, oldPath string
			if err := rows.Scan(&tID, &oldPath); err != nil {
				rows.Close()
				return err
			}

			filename := filepath.Base(filepath.FromSlash(oldPath))
			newPath := filename
			if newRel != "" {
				newPath = filepath.ToSlash(filepath.Join(newRel, filename))
			}
			moves = append(moves, trackMove{ID: tID, OldPath: oldPath, NewPath: newPath})
		}
		rows.Close()

		updateQuery := `UPDATE tracks SET relative_path = ? WHERE id = ?`
		for _, m := range moves {
			if _, err := tx.Exec(updateQuery, m.NewPath, m.ID); err != nil {
				return fmt.Errorf("failed to update track %q relative_path: %w", m.ID, err)
			}

			oldAbs := filepath.Join(l.Paths.Library, filepath.FromSlash(m.OldPath))
			newAbs := filepath.Join(l.Paths.Library, filepath.FromSlash(m.NewPath))

			if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
				return fmt.Errorf("failed to create destination directory: %w", err)
			}
			if err := os.Rename(oldAbs, newAbs); err != nil {
				return fmt.Errorf("failed to move track file on disk: %w", err)
			}
		}
	}

	query := `UPDATE artists SET id = ?, display_name = ?, folder_id = ? WHERE id = ?`
	if _, err := tx.Exec(query, strings.TrimSpace(updated.ID), updated.Artist, toNull(updated.FolderID), oldID); err != nil {
		return fmt.Errorf("failed to update artist in database: %w", err)
	}

	return tx.Commit()
}
