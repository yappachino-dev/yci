package library

import (
	"database/sql"
	"fmt"
)

type Track struct {
	ID           string
	Title        string
	Album        string
	ArtistID     string
	DurationSec  int
	RelativePath string
}

func (l *Library) GetTracksByArtist(artistID string) ([]Track, error) {
	query := `
		SELECT id, title, album, artist_id, duration_sec, relative_path 
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

		if err := rows.Scan(&t.ID, &t.Title, &album, &aID, &t.DurationSec, &t.RelativePath); err != nil {
			return nil, err
		}

		if aID.Valid {
			t.ArtistID = aID.String
		}
		if album.Valid {
			t.Album = album.String
		}

		tracks = append(tracks, t)
	}

	return tracks, nil
}

func (l *Library) AddTrack(t Track) error {
	query := `
	INSERT INTO tracks (id, title, album, artist_id, duration_sec, relative_path) 
	VALUES (?, ?, ?, ?, ?, ?)`

	var artist any = t.ArtistID
	if t.ArtistID == "" {
		artist = nil
	}

	_, err := l.db.Exec(query, t.ID, t.Title, t.Album, artist, t.DurationSec, t.RelativePath)
	if err != nil {
		return fmt.Errorf("failed to add track %q: %w", t.Title, err)
	}

	return nil
}

func (l *Library) RemoveTrack(id string) error {
	res, err := l.db.Exec("DELETE FROM tracks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to remove track %q: %w", id, err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("track %q does not exist", id)
	}

	return nil
}
