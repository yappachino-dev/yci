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

func scanTrack(s scannable) (Track, error) {
	var t Track
	var aID, album, source sql.NullString

	err := s.Scan(&t.ID, &t.Title, &album, &aID, &t.DurationSec, &t.RelativePath, &source)
	if err != nil {
		return t, err
	}

	t.ArtistID = fromNull(aID, "")
	t.Album = fromNull(album, "")
	t.SourceID = fromNull(source, "")

	return t, nil
}

func (l *Library) GetAllTracks() ([]Track, error) {
	query := `SELECT id, title, album, artist_id, duration_sec, relative_path, source_id FROM tracks`
	return l.queryTracks(query)
}

func (l *Library) GetTracksByArtist(artistID string) ([]Track, error) {
	query := `SELECT id, title, album, artist_id, duration_sec, relative_path, source_id
              FROM tracks WHERE artist_id = ?`
	return l.queryTracks(query, artistID)
}

func (l *Library) queryTracks(query string, args ...any) ([]Track, error) {
	rows, err := l.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("track query failed: %w", err)
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}

	return tracks, nil
}

func (l *Library) AddTrack(t Track) error {
	query := `
		INSERT INTO tracks (id, title, album, artist_id, duration_sec, relative_path, source_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := l.db.Exec(query,
		t.ID,
		t.Title,
		toNull(t.Album),
		toNull(t.ArtistID),
		t.DurationSec,
		t.RelativePath,
		toNull(t.SourceID),
	)
	if err != nil {
		return fmt.Errorf("failed to add track %q: %w", t.Title, err)
	}

	return nil
}

func (l *Library) RemoveTrack(id string) error {
	var relativePath string

	err := l.db.QueryRow("SELECT relative_path FROM tracks WHERE id = ?", id).Scan(&relativePath)
	if err != nil {
		return l.wrapErr(err, "track", id)
	}

	absPath := filepath.Join(l.Paths.Library, relativePath)
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete physical file for track %q: %w", id, err)
	}

	_, err = l.db.Exec("DELETE FROM tracks WHERE id = ?", id)

	return l.wrapErr(err, "track", id)
}
