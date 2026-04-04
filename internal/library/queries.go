package library

import "database/sql"

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
