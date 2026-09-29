package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/yappachino-dev/yci/internal/appctx"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/store"
)

// Sentinel errors for channel link operations.
var (
	ErrChannelAlreadyLinked = errors.New("channel already linked to artist")
	ErrChannelNotLinked     = errors.New("channel not linked to artist")
)

type ChannelName string

func (n ChannelName) Validate() error {
	if strings.TrimSpace(string(n)) == "" {
		return fmt.Errorf("channel: %w", ErrNameEmpty)
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

// ChannelService links YouTube channel names to artists.
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
	if err := name.Validate(); err != nil {
		return fmt.Errorf("unlink channel: %w", err)
	}
	if artistID == "" {
		return fmt.Errorf("unlink channel: artist ID cannot be empty")
	}

	if err := s.repo.Delete(ctx, artistID, name); err != nil {
		return fmt.Errorf("unlink channel %q from artist %q: %w", name, artistID, err)
	}

	return nil
}

func (s *ChannelService) All(ctx context.Context) ([]Channel, error) {
	return s.repo.All(ctx)
}

// SQLiteChannelRepository implements ChannelRepository over sqlc queries.
type SQLiteChannelRepository struct {
	q *store.Queries
}

func NewSQLiteChannelRepository(q *store.Queries) *SQLiteChannelRepository {
	return &SQLiteChannelRepository{q: q}
}

func (r *SQLiteChannelRepository) Create(ctx context.Context, artistID ArtistID, name ChannelName) error {
	err := r.db(ctx).CreateChannel(ctx, store.CreateChannelParams{
		ArtistID:    string(artistID),
		ChannelName: db.ToNullString(string(name)),
	})
	if err != nil {
		return db.MapSQLError(err, nil, ErrChannelAlreadyLinked)
	}

	return nil
}

func (r *SQLiteChannelRepository) FindArtistID(ctx context.Context, name ChannelName) (ArtistID, bool, error) {
	artistID, err := r.db(ctx).GetArtistIDByChannel(ctx, db.ToNullString(string(name)))
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find artist by channel %q: %w", name, err)
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
	rows, err := r.db(ctx).DeleteChannel(ctx, store.DeleteChannelParams{
		ArtistID:    string(artistID),
		ChannelName: db.ToNullString(string(name)),
	})
	if err != nil {
		return fmt.Errorf("unlink channel %q from artist %q: %w", name, artistID, err)
	}

	if rows == 0 {
		return ErrChannelNotLinked
	}

	return nil
}

func (r *SQLiteChannelRepository) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return r.q.WithTx(tx)
	}

	return r.q
}
