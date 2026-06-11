package library

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"playlist-manager/internal/appctx"
	"playlist-manager/internal/db"
	"playlist-manager/internal/store"
)

type Channel struct {
	ID          int
	ArtistID    string
	ChannelName string
}

func (l *Library) GetChannels() ([]Channel, error) {
	rows, err := l.db.Query("SELECT id, artist_id, channel_name FROM channels")
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
	var artistID string

	err := l.db.QueryRow("SELECT artist_id FROM channels WHERE channel_name = ?", channelName).Scan(&artistID)

	if err == sql.ErrNoRows {
		return "", nil
	}

	return artistID, l.wrapErr(err, "channel mapping", channelName)
}

func (l *Library) AddChannel(artistID, channelName string) error {
	artistID = strings.TrimSpace(artistID)
	channelName = strings.TrimSpace(channelName)

	if artistID == "" || channelName == "" {
		return fmt.Errorf("artist ID and channel name cannot be empty")
	}

	query := `INSERT INTO channels (artist_id, channel_name) VALUES (?, ?)`

	_, err := l.db.Exec(query, artistID, channelName)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("channel %q is already linked to an artist", channelName)
		}
		return fmt.Errorf("failed to link channel %q: %w", channelName, err)
	}

	return nil
}

func (l *Library) RemoveChannel(artistID, channelName string) error {
	res, err := l.db.Exec("DELETE FROM channels WHERE artist_id = ? AND channel_name = ?", artistID, channelName)
	if err != nil {
		return l.wrapErr(err, "channel link", channelName)
	}

	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("no link found between artist %q and channel %q", artistID, channelName)
	}

	return nil
}

type SQLiteChannelLinker struct {
	q      *store.Queries
	rootID FolderID
}

func NewSQLiteChannelLinker(q *store.Queries, rootID FolderID) *SQLiteChannelLinker {
	return &SQLiteChannelLinker{
		q:      q,
		rootID: rootID,
	}
}

func (l *SQLiteChannelLinker) LookupArtistByChannel(ctx context.Context, channel string) (ArtistID, error) {
	artistID, err := l.db(ctx).GetArtistIDByChannel(ctx, db.ToNullString(channel))
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return ArtistID(artistID), nil
}

func (l *SQLiteChannelLinker) LinkChannelToArtist(ctx context.Context, artistID ArtistID, channel string) error {
	return l.db(ctx).CreateChannel(ctx, store.CreateChannelParams{
		ArtistID:    string(artistID),
		ChannelName: db.ToNullString(channel),
	})
}

func (l *SQLiteChannelLinker) ArtistFolderID(ctx context.Context, id ArtistID) (FolderID, error) {
	row, err := l.db(ctx).GetArtist(ctx, string(id))
	if err != nil {
		return "", err
	}
	return FolderID(db.FromNullString(row.FolderID)).FromDB(l.rootID), nil
}

func (l *SQLiteChannelLinker) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return l.q.WithTx(tx)
	}
	return l.q
}

func (l *SQLiteChannelLinker) AllChannels(ctx context.Context) ([]Channel, error) {
	rows, err := l.db(ctx).GetChannels(ctx)
	if err != nil {
		return nil, err
	}

	channels := make([]Channel, 0, len(rows))
	for _, row := range rows {
		channels = append(channels, Channel{
			ID:          int(row.ID),
			ArtistID:    row.ArtistID,
			ChannelName: db.FromNullString(row.ChannelName),
		})
	}
	return channels, nil
}

func (l *SQLiteChannelLinker) UnlinkChannel(ctx context.Context, artistID ArtistID, channel string) error {
	return l.db(ctx).DeleteChannel(ctx, store.DeleteChannelParams{
		ArtistID:    string(artistID),
		ChannelName: db.ToNullString(channel),
	})
}
