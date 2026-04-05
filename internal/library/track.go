package library

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

type Track struct {
	ID           string
	Title        string
	Album        string
	ArtistID     string
	DurationSec  int
	RelativePath string
	SourceID     string
}

func (l *Library) GetAllTracks() ([]Track, error) {
	query := `SELECT id, title, album, artist_id, duration_sec, relative_path, source_id FROM tracks`

	rows, err := l.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all tracks: %w", err)
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		var t Track
		var aID sql.NullString
		var album sql.NullString
		var source sql.NullString

		if err := rows.Scan(&t.ID, &t.Title, &album, &aID, &t.DurationSec, &t.RelativePath, &source); err != nil {
			return nil, err
		}

		if aID.Valid {
			t.ArtistID = aID.String
		}
		if album.Valid {
			t.Album = album.String
		}
		if source.Valid {
			t.SourceID = source.String
		}

		tracks = append(tracks, t)
	}

	return tracks, nil
}

func (l *Library) GetTracksByArtist(artistID string) ([]Track, error) {
	query := `
		SELECT id, title, album, artist_id, duration_sec, relative_path, source_id 
		FROM tracks 
		WHERE artist_id = ?`

	rows, err := l.db.Query(query, artistID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tracks for artist %q: %w", artistID, err)
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		var t Track
		var aID sql.NullString
		var album sql.NullString
		var source sql.NullString

		if err := rows.Scan(&t.ID, &t.Title, &album, &aID, &t.DurationSec, &t.RelativePath, &source); err != nil {
			return nil, err
		}

		if aID.Valid {
			t.ArtistID = aID.String
		}
		if album.Valid {
			t.Album = album.String
		}
		if source.Valid {
			t.SourceID = source.String
		}

		tracks = append(tracks, t)
	}

	return tracks, nil
}

func (l *Library) AddTrack(t Track) error {
	query := `
		INSERT INTO tracks (id, title, album, artist_id, duration_sec, relative_path, source_id) 
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	var artist any = t.ArtistID
	if t.ArtistID == "" {
		artist = nil
	}

	_, err := l.db.Exec(query, t.ID, t.Title, t.Album, artist, t.DurationSec, t.RelativePath, t.SourceID)
	if err != nil {
		return fmt.Errorf("failed to add track %q: %w", t.Title, err)
	}

	return nil
}

func (l *Library) RemoveTrack(id string) error {
	var relativePath string

	err := l.db.QueryRow("SELECT relative_path FROM tracks WHERE id = ?", id).Scan(&relativePath)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("track %q not found in database", id)
		}
		return fmt.Errorf("database error: %w", err)
	}

	absolutePath := filepath.Join(l.Paths.Library, relativePath)

	err = os.Remove(absolutePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete physical file %q: %w", absolutePath, err)
	}

	_, err = l.db.Exec("DELETE FROM tracks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete track from database: %w", err)
	}

	return nil
}
