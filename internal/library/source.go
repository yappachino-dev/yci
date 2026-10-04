package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/yappachino-dev/yci/internal/appctx"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/store"
)

var (
	ErrSourceNotFound  = errors.New("watched playlist not found")
	ErrSourceExists    = errors.New("watched playlist already exists")
	ErrNoChannelsSaved = errors.New("a watched playlist needs at least one channel selected")
)

// SourceChannel is one channel of a watched playlist. The UC... ID is the
// identity a poll matches against and the name is the label shown to the user,
// because a performer can rename a channel at any time and a selection stored
// as a string would quietly stop matching.
//
// Duplicated rather than shared with internal/download's ChannelRef: neither
// package imports the other, and the CLI is the boundary that converts.
type SourceChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Source is a watched YouTube playlist: a remote list whose new videos are
// pulled into the download queue on demand. It is not a Playlist, whose
// membership is local, authoritative, and only ever changed deliberately.
type Source struct {
	Name     string
	URL      string
	Channels []SourceChannel
}

// ChannelNames returns the labels for display, in stored order.
func (s Source) ChannelNames() []string {
	names := make([]string, 0, len(s.Channels))
	for _, ch := range s.Channels {
		names = append(names, ch.Name)
	}

	return names
}

type SourceRepository interface {
	Create(ctx context.Context, name, url, channels string) error
	ByName(ctx context.Context, name string) (Source, error)
	All(ctx context.Context) ([]Source, error)
	SetChannels(ctx context.Context, name, channels string) error
	Delete(ctx context.Context, name string) error
}

// SourceService stores the playlists a user watches.
type SourceService struct {
	repo SourceRepository
}

// NewSourceService takes the repository directly: one dependency does not need
// a config struct (style-guide §2).
func NewSourceService(repo SourceRepository) *SourceService {
	return &SourceService{repo: repo}
}

func (s *SourceService) Add(ctx context.Context, name, url string, channels []SourceChannel) error {
	if len(channels) == 0 {
		return ErrNoChannelsSaved
	}

	encoded, err := encodeSourceChannels(channels)
	if err != nil {
		return fmt.Errorf("encode source channels: %w", err)
	}

	if err := s.repo.Create(ctx, name, url, encoded); err != nil {
		return fmt.Errorf("add watched playlist %q: %w", name, err)
	}

	return nil
}

func (s *SourceService) ByName(ctx context.Context, name string) (Source, error) {
	src, err := s.repo.ByName(ctx, name)
	if err != nil {
		return Source{}, fmt.Errorf("find watched playlist %q: %w", name, err)
	}

	return src, nil
}

func (s *SourceService) List(ctx context.Context) ([]Source, error) {
	sources, err := s.repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list watched playlists: %w", err)
	}

	return sources, nil
}

func (s *SourceService) SetChannels(ctx context.Context, name string, channels []SourceChannel) error {
	if len(channels) == 0 {
		return ErrNoChannelsSaved
	}

	encoded, err := encodeSourceChannels(channels)
	if err != nil {
		return fmt.Errorf("encode source channels: %w", err)
	}

	if err := s.repo.SetChannels(ctx, name, encoded); err != nil {
		return fmt.Errorf("update watched playlist %q: %w", name, err)
	}

	return nil
}

func (s *SourceService) Remove(ctx context.Context, name string) error {
	if err := s.repo.Delete(ctx, name); err != nil {
		return fmt.Errorf("remove watched playlist %q: %w", name, err)
	}

	return nil
}

// encodeSourceChannels stores the selection as JSON rather than a delimited
// string, because a YouTube channel name can contain any character a display
// name allows, including the separator.
func encodeSourceChannels(channels []SourceChannel) (string, error) {
	encoded, err := json.Marshal(channels)
	if err != nil {
		return "", fmt.Errorf("marshal channels: %w", err)
	}

	return string(encoded), nil
}

// SQLiteSourceRepository implements SourceRepository over sqlc queries.
type SQLiteSourceRepository struct {
	q *store.Queries
}

func NewSQLiteSourceRepository(q *store.Queries) *SQLiteSourceRepository {
	return &SQLiteSourceRepository{q: q}
}

func (r *SQLiteSourceRepository) Create(ctx context.Context, name, url, channels string) error {
	err := r.db(ctx).CreateSource(ctx, store.CreateSourceParams{
		Name:     name,
		Url:      url,
		Channels: channels,
	})
	if err != nil {
		return db.MapSQLError(err, nil, ErrSourceExists)
	}

	return nil
}

func (r *SQLiteSourceRepository) ByName(ctx context.Context, name string) (Source, error) {
	row, err := r.db(ctx).GetSourceByName(ctx, name)
	if err != nil {
		return Source{}, db.MapSQLError(err, ErrSourceNotFound, nil)
	}

	return decodeSourceRow(row.Name, row.Url, row.Channels)
}

func (r *SQLiteSourceRepository) All(ctx context.Context) ([]Source, error) {
	rows, err := r.db(ctx).GetSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("query sources: %w", err)
	}

	sources := make([]Source, 0, len(rows))
	for _, row := range rows {
		src, err := decodeSourceRow(row.Name, row.Url, row.Channels)
		if err != nil {
			return nil, err
		}

		sources = append(sources, src)
	}

	return sources, nil
}

func (r *SQLiteSourceRepository) SetChannels(ctx context.Context, name, channels string) error {
	affected, err := r.db(ctx).SetSourceChannels(ctx, store.SetSourceChannelsParams{
		Name:     name,
		Channels: channels,
	})
	if err != nil {
		return fmt.Errorf("update source %q: %w", name, err)
	}

	if affected == 0 {
		return ErrSourceNotFound
	}

	return nil
}

func (r *SQLiteSourceRepository) Delete(ctx context.Context, name string) error {
	affected, err := r.db(ctx).DeleteSource(ctx, name)
	if err != nil {
		return fmt.Errorf("delete source %q: %w", name, err)
	}

	if affected == 0 {
		return ErrSourceNotFound
	}

	return nil
}

// decodeSourceRow rebuilds a Source, treating unparseable stored JSON as an
// empty selection rather than a hard failure: a corrupt column must not make
// every other watched playlist unreadable.
func decodeSourceRow(name, url, channels string) (Source, error) {
	var decoded []SourceChannel
	if err := json.Unmarshal([]byte(channels), &decoded); err != nil {
		decoded = nil
	}

	return Source{Name: name, URL: url, Channels: decoded}, nil
}

func (r *SQLiteSourceRepository) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return r.q.WithTx(tx)
	}

	return r.q
}
