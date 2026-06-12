package library

import (
	"context"
	"database/sql"

	"playlist-manager/internal/appctx"
	"playlist-manager/internal/db"
	"playlist-manager/internal/store"
)

type Channel struct {
	ID          int
	ArtistID    string
	ChannelName string
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
