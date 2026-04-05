package library

import (
	"database/sql"
	"fmt"
	"strings"
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

func (l *Library) AddChannel(artistID string, channelName string) error {
	artistID = strings.TrimSpace(artistID)
	channelName = strings.TrimSpace(channelName)

	if artistID == "" || channelName == "" {
		return fmt.Errorf("artist ID and channel name cannot be empty")
	}

	query := "INSERT INTO channels (artist_id, channel_name) VALUES (?, ?)"

	_, err := l.db.Exec(query, artistID, channelName)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("the channel %q is already linked to an artist", channelName)
		}
		return fmt.Errorf("failed to link channel %q: %w", channelName, err)
	}

	return nil
}

func (l *Library) RemoveChannel(artistID string, channelName string) error {
	res, err := l.db.Exec("DELETE FROM channels WHERE artist_id = ? AND channel_name = ?", artistID, channelName)
	if err != nil {
		return fmt.Errorf("failed to delete channel link: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("channel link %q -> %q not found", channelName, artistID)
	}
	return nil
}
