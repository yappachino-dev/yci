package library

import (
	"database/sql"
	"fmt"
)

type Channel struct {
	ID          int
	ArtistID    string
	ChannelName string
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

func (l *Library) GetArtistByChannel(channelName string) (string, error) {
	query := "SELECT artist_id FROM channels WHERE channel_name = ?"

	var artistID string
	err := l.db.QueryRow(query, channelName).Scan(&artistID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("database error checking channel %q: %w", channelName, err)
	}

	return artistID, nil
}
