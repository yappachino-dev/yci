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
	// ErrChannelNameAmbiguous means one name resolves to several artists.
	ErrChannelNameAmbiguous = errors.New("channel name links to several artists")
)

// VideoID is a YouTube video identifier, as it appears in a staged file name.
type VideoID string

// ChannelID is a YouTube channel identifier, the immutable UC... form.
//
// It is the identity of a link. The name is a label: a performer can rename
// their channel at any time, and treating the label as the key made a rename
// orphan every download attributed to them.
type ChannelID string

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
	// ChannelID is empty for a link made before the channel was resolved.
	ChannelID ChannelID
}

type ChannelRepository interface {
	Create(ctx context.Context, artistID ArtistID, name ChannelName) error
	FindArtistID(ctx context.Context, name ChannelName) (ArtistID, bool, error)
	All(ctx context.Context) ([]Channel, error)
	Delete(ctx context.Context, artistID ArtistID, name ChannelName) error

	// ResolveByID is the authoritative lookup, and RenameByID refreshes a
	// label that has drifted from the channel's current name.
	ResolveByID(ctx context.Context, id ChannelID) (ArtistID, bool, error)
	RenameByID(ctx context.Context, id ChannelID, artistID ArtistID, name ChannelName) error
	AttachID(ctx context.Context, artistID ArtistID, name ChannelName, id ChannelID) error

	RecordVideoChannel(ctx context.Context, videoID VideoID, id ChannelID) error
	ForgetVideoChannel(ctx context.Context, videoID VideoID) error
	VideoChannel(ctx context.Context, videoID VideoID) (ChannelID, bool, error)
}

// ChannelService links YouTube channel names to artists.
type ChannelService struct {
	repo ChannelRepository

	// tracks and probe back the identity backfill. Both are optional: without
	// them the service still links, resolves and unlinks, and the backfill
	// reports that it has no way to look anything up.
	tracks TrackLister
	probe  VideoChannelLookup
}

// ChannelOption configures the optional parts of ChannelService.
type ChannelOption func(*ChannelService)

// WithBackfill supplies what the identity backfill needs: a way to find a
// video an artist published, and a way to ask the source about it.
func WithBackfill(tracks TrackLister, probe VideoChannelLookup) ChannelOption {
	return func(s *ChannelService) {
		s.tracks = tracks
		s.probe = probe
	}
}

func NewChannelService(repo ChannelRepository, opts ...ChannelOption) *ChannelService {
	s := &ChannelService{repo: repo}
	for _, opt := range opts {
		opt(s)
	}

	return s
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

func (s *ChannelService) ResolveArtistByID(ctx context.Context, id ChannelID) (ArtistID, bool, error) {
	return s.repo.ResolveByID(ctx, id)
}

func (s *ChannelService) RefreshChannelName(ctx context.Context, id ChannelID, artistID ArtistID, name ChannelName) error {
	return s.repo.RenameByID(ctx, id, artistID, name)
}

func (s *ChannelService) AttachChannelID(ctx context.Context, artistID ArtistID, name ChannelName, id ChannelID) error {
	return s.repo.AttachID(ctx, artistID, name, id)
}

// RecordVideoChannel satisfies download.ChannelRecorder. It is deliberately
// shaped with plain strings so the download package does not have to import
// this one to record a mapping.
func (s *ChannelService) RecordVideoChannel(ctx context.Context, videoID, channelID string) error {
	return s.repo.RecordVideoChannel(ctx, VideoID(videoID), ChannelID(channelID))
}

func (s *ChannelService) VideoChannel(ctx context.Context, videoID VideoID) (ChannelID, bool, error) {
	return s.repo.VideoChannel(ctx, videoID)
}

func (s *ChannelService) ForgetVideoChannel(ctx context.Context, videoID VideoID) error {
	return s.repo.ForgetVideoChannel(ctx, videoID)
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
		ChannelID:   db.ToNullString(""),
	})
	if err != nil {
		return db.MapSQLError(err, nil, ErrChannelAlreadyLinked)
	}

	return nil
}

func (r *SQLiteChannelRepository) FindArtistID(ctx context.Context, name ChannelName) (ArtistID, bool, error) {
	ids, err := r.db(ctx).GetArtistIDByChannel(ctx, db.ToNullString(string(name)))
	if err != nil {
		return "", false, fmt.Errorf("find artist by channel %q: %w", name, err)
	}

	switch len(ids) {
	case 0:
		return "", false, nil
	case 1:
		return ArtistID(ids[0]), true, nil
	default:
		// Two artists may hold the same name, so picking the first would
		// file a download under an arbitrary artist and call it resolved.
		return "", false, fmt.Errorf("%w: %q is linked to %s",
			ErrChannelNameAmbiguous, name, strings.Join(ids, ", "))
	}
}

func (r *SQLiteChannelRepository) ResolveByID(ctx context.Context, id ChannelID) (ArtistID, bool, error) {
	artistID, err := r.db(ctx).GetArtistIDByChannelID(ctx, db.ToNullString(string(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolve channel %q: %w", id, err)
	}

	return ArtistID(artistID), true, nil
}

func (r *SQLiteChannelRepository) RenameByID(ctx context.Context, id ChannelID, artistID ArtistID, name ChannelName) error {
	rows, err := r.db(ctx).SetChannelNameByID(ctx, store.SetChannelNameByIDParams{
		ChannelID:   db.ToNullString(string(id)),
		ChannelName: db.ToNullString(string(name)),
		ArtistID:    string(artistID),
	})
	if err != nil {
		return fmt.Errorf("rename channel %q: %w", id, err)
	}
	if rows == 0 {
		return ErrChannelNotLinked
	}

	return nil
}

func (r *SQLiteChannelRepository) AttachID(ctx context.Context, artistID ArtistID, name ChannelName, id ChannelID) error {
	_, err := r.db(ctx).SetChannelID(ctx, store.SetChannelIDParams{
		ArtistID:    string(artistID),
		ChannelName: db.ToNullString(string(name)),
		ChannelID:   db.ToNullString(string(id)),
	})
	if err != nil {
		return fmt.Errorf("attach channel id to %q: %w", name, err)
	}

	return nil
}

func (r *SQLiteChannelRepository) RecordVideoChannel(ctx context.Context, videoID VideoID, id ChannelID) error {
	return r.db(ctx).SetVideoChannel(ctx, store.SetVideoChannelParams{
		VideoID:   string(videoID),
		ChannelID: string(id),
	})
}

func (r *SQLiteChannelRepository) ForgetVideoChannel(ctx context.Context, videoID VideoID) error {
	return r.db(ctx).DeleteVideoChannel(ctx, string(videoID))
}

func (r *SQLiteChannelRepository) VideoChannel(ctx context.Context, videoID VideoID) (ChannelID, bool, error) {
	id, err := r.db(ctx).GetVideoChannel(ctx, string(videoID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("look up channel for video %q: %w", videoID, err)
	}

	return ChannelID(id), true, nil
}

func (r *SQLiteChannelRepository) All(ctx context.Context) ([]Channel, error) {
	rows, err := r.db(ctx).GetChannels(ctx)
	if err != nil {
		return nil, err
	}

	channels := make([]Channel, 0, len(rows))
	for _, row := range rows {
		channels = append(channels, Channel{
			ID:        int(row.ID),
			ArtistID:  ArtistID(row.ArtistID),
			Name:      ChannelName(db.FromNullString(row.ChannelName)),
			ChannelID: ChannelID(db.FromNullString(row.ChannelID)),
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
