package library

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/yappachino-dev/yci/internal/appctx"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/store"
)

type ChannelName string

func (n ChannelName) Validate() error {
	if strings.TrimSpace(string(n)) == "" {
		return fmt.Errorf("channel name cannot be empty")
	}
	return nil
}

type Channel struct {
	ID       int
	ArtistID ArtistID
	Name     ChannelName
}

type ChannelRepository interface {
	Create(ctx context.Context, artistID ArtistID, name ChannelName) error
	FindArtistID(ctx context.Context, name ChannelName) (ArtistID, bool, error)
	All(ctx context.Context) ([]Channel, error)
	Delete(ctx context.Context, artistID ArtistID, name ChannelName) error
}

type ChannelService struct {
	repo ChannelRepository
}

func NewChannelService(repo ChannelRepository) *ChannelService {
	return &ChannelService{repo: repo}
}

func (s *ChannelService) Link(ctx context.Context, artistID ArtistID, name ChannelName) error {
	if err := name.Validate(); err != nil {
		return fmt.Errorf("link channel: %w", err)
	}
	if artistID == "" {
		return fmt.Errorf("link channel: artist ID cannot be empty")
	}

	if err := s.repo.Create(ctx, artistID, name); err != nil {
		return fmt.Errorf("link channel %q to artist %q: %w", name, artistID, err)
	}
	return nil
}

func (s *ChannelService) ResolveArtist(ctx context.Context, name ChannelName) (ArtistID, bool, error) {
	return s.repo.FindArtistID(ctx, name)
}

func (s *ChannelService) Unlink(ctx context.Context, artistID ArtistID, name ChannelName) error {
	return s.repo.Delete(ctx, artistID, name)
}

func (s *ChannelService) All(ctx context.Context) ([]Channel, error) {
	return s.repo.All(ctx)
}

type SQLiteChannelRepository struct {
	q *store.Queries
}

func NewSQLiteChannelRepository(q *store.Queries) *SQLiteChannelRepository {
	return &SQLiteChannelRepository{q: q}
}

func (r *SQLiteChannelRepository) Create(ctx context.Context, artistID ArtistID, name ChannelName) error {
	return r.db(ctx).CreateChannel(ctx, store.CreateChannelParams{
		ArtistID:    string(artistID),
		ChannelName: db.ToNullString(string(name)),
	})
}

func (r *SQLiteChannelRepository) FindArtistID(ctx context.Context, name ChannelName) (ArtistID, bool, error) {
	artistID, err := r.db(ctx).GetArtistIDByChannel(ctx, db.ToNullString(string(name)))
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	return ArtistID(artistID), true, nil
}

func (r *SQLiteChannelRepository) All(ctx context.Context) ([]Channel, error) {
	rows, err := r.db(ctx).GetChannels(ctx)
	if err != nil {
		return nil, err
	}

	channels := make([]Channel, 0, len(rows))
	for _, row := range rows {
		channels = append(channels, Channel{
			ID:       int(row.ID),
			ArtistID: ArtistID(row.ArtistID),
			Name:     ChannelName(db.FromNullString(row.ChannelName)),
		})
	}

	return channels, nil
}

func (r *SQLiteChannelRepository) Delete(ctx context.Context, artistID ArtistID, name ChannelName) error {
	return r.db(ctx).DeleteChannel(ctx, store.DeleteChannelParams{
		ArtistID:    string(artistID),
		ChannelName: db.ToNullString(string(name)),
	})
}

func (r *SQLiteChannelRepository) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return r.q.WithTx(tx)
	}

	return r.q
}
