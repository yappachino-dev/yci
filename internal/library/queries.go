package library

import (
	"database/sql"
	"fmt"
)

type Folder struct {
	ID          string
	DisplayName string
	ParentID    string
}

type Artist struct {
	ID       string
	FolderID string
	Artist   string
}

type Channel struct {
	ID          int
	ArtistID    string
	ChannelName string
}

type Track struct {
	ID           string
	Title        string
	Album        string
	ArtistID     string
	DurationSec  int
	RelativePath string
}

func (l *Library) GetFolders() ([]Folder, error) {
	query := "SELECT id, display_name, parent_id FROM folders"

	rows, err := l.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []Folder

	for rows.Next() {
		var f Folder
		var parent sql.NullString

		if err := rows.Scan(&f.ID, &f.DisplayName, &parent); err != nil {
			return nil, err
		}

		if parent.Valid {
			f.ParentID = parent.String
		} else {
			f.ParentID = root
		}

		folders = append(folders, f)
	}

	return folders, nil
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

func (l *Library) GetChannels() ([]Channel, error) {
	query := "SELECT id, artist_id, channel_name FROM channels"

	rows, err := l.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.ArtistID, &c.ChannelName); err != nil {
			return nil, err
		}
		channels = append(channels, c)
	}

	return channels, nil
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
