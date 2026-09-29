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

// Sentinel errors for artist lookups and writes.
var (
	// ErrNameEmpty reports a blank artist, folder or channel name. It is a
	// sentinel because validation runs at the boundary of Create and Rename,
	// and a caller that needs to distinguish "the user typed nothing" from
	// any other failure cannot do it with an anonymous fmt.Errorf.
	ErrNameEmpty = errors.New("name cannot be empty")

	ErrArtistNotFound      = errors.New("artist not found")
	ErrArtistAlreadyExists = errors.New("artist already exists")
)

type (
	ArtistID   string
	ArtistName string
)

func (n ArtistName) ToSlugID() ArtistID {
	return ArtistID(nameToSlug(string(n)))
}

func (n ArtistName) Validate() error {
	if strings.TrimSpace(string(n)) == "" {
		return fmt.Errorf("artist: %w", ErrNameEmpty)
	}
	return nil
}

type Artist struct {
	ID       ArtistID
	Name     ArtistName
	FolderID FolderID
}

type ArtistRepository interface {
	Create(ctx context.Context, a Artist) (Artist, error)
	ByID(ctx context.Context, id ArtistID) (Artist, error)
	All(ctx context.Context) ([]Artist, error)
	Update(ctx context.Context, oldID ArtistID, a Artist) (Artist, error)
	Delete(ctx context.Context, id ArtistID) error
}

type FolderChecker interface {
	Exists(ctx context.Context, id FolderID) (bool, error)
}

type ArtistTracksMover interface {
	MoveByArtist(ctx context.Context, id ArtistID, oldFolder, newFolder FolderID) error
}

// ArtistService manages artist entities and relocates their tracks when
// the artist moves folders.
type ArtistService struct {
	repo          ArtistRepository
	folderChecker FolderChecker
	trackMover    ArtistTracksMover
	transactor    Transactor
	rootFolder    FolderID
}

type ArtistServiceConfig struct {
	ArtistRepo    ArtistRepository
	FolderChecker FolderChecker
	TrackMover    ArtistTracksMover
	Transactor    Transactor
	RootFolder    FolderID
}

func NewArtistService(cfg ArtistServiceConfig) *ArtistService {
	return &ArtistService{
		repo:          cfg.ArtistRepo,
		folderChecker: cfg.FolderChecker,
		trackMover:    cfg.TrackMover,
		transactor:    cfg.Transactor,
		rootFolder:    cfg.RootFolder,
	}
}

func (s *ArtistService) Create(ctx context.Context, id ArtistID, folderID FolderID, name ArtistName) (Artist, error) {
	if err := name.Validate(); err != nil {
		return Artist{}, fmt.Errorf("create artist: %w", err)
	}

	if id == "" {
		id = name.ToSlugID()
	}

	if folderID == "" {
		exists, err := s.folderChecker.Exists(ctx, FolderID(id))
		if err != nil {
			return Artist{}, fmt.Errorf("folder fallback for artist %q: %w", id, err)
		}

		if exists {
			folderID = FolderID(id)
		} else {
			folderID = s.rootFolder
		}
	}

	draft := Artist{
		ID:       id,
		Name:     name,
		FolderID: folderID,
	}

	saved, err := s.repo.Create(ctx, draft)
	if err != nil {
		return Artist{}, fmt.Errorf("create artist %q: %w", draft.ID, err)
	}

	return saved, nil
}

func (s *ArtistService) ByID(ctx context.Context, id ArtistID) (Artist, error) {
	a, err := s.repo.ByID(ctx, id)
	if err != nil {
		return Artist{}, fmt.Errorf("artist by id %q: %w", id, err)
	}

	return a, nil
}

func (s *ArtistService) All(ctx context.Context) ([]Artist, error) {
	a, err := s.repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all artists: %w", err)
	}

	return a, nil
}

func (s *ArtistService) AllIDs(ctx context.Context) ([]ArtistID, error) {
	a, err := s.repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("all artists: %w", err)
	}

	ids := make([]ArtistID, 0, len(a))
	for _, i := range a {
		ids = append(ids, i.ID)
	}

	return ids, nil
}

type ArtistUpdateParams struct {
	NewID            ArtistID
	Name             ArtistName
	Folder           FolderID
	RegenerateSlugID bool
}

func (s *ArtistService) Update(ctx context.Context, oldID ArtistID, params ArtistUpdateParams) (Artist, error) {
	current, err := s.repo.ByID(ctx, oldID)
	if err != nil {
		return Artist{}, fmt.Errorf("update artist %q: %w", oldID, err)
	}

	newID := newIfPresent(params.NewID, oldID)
	newName := newIfPresent(params.Name, current.Name)
	newFolder := newIfPresent(params.Folder, current.FolderID)

	if params.RegenerateSlugID {
		newID = ArtistID(nameToSlug(string(newName)))
	}

	draft := Artist{
		ID:       newID,
		Name:     newName,
		FolderID: newFolder,
	}

	if current.FolderID != newFolder {
		var saved Artist
		txErr := s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			if err := s.trackMover.MoveByArtist(txCtx, oldID, current.FolderID, newFolder); err != nil {
				return err
			}

			updated, err := s.repo.Update(txCtx, oldID, draft)
			if err != nil {
				return err
			}

			saved = updated
			return nil
		})

		if txErr != nil {
			return Artist{}, fmt.Errorf("update artist %q: %w", oldID, txErr)
		}

		return saved, nil
	}

	saved, err := s.repo.Update(ctx, oldID, draft)
	if err != nil {
		return Artist{}, fmt.Errorf("update artist %q: %w", oldID, err)
	}

	return saved, nil
}

func (s *ArtistService) Delete(ctx context.Context, id ArtistID) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete artist: %w", err)
	}

	return nil
}

// SQLiteArtistRepository implements ArtistRepository over sqlc queries.
type SQLiteArtistRepository struct {
	q      *store.Queries
	rootID FolderID
}

func NewSQLiteArtistRepository(q *store.Queries, rootID FolderID) *SQLiteArtistRepository {
	return &SQLiteArtistRepository{
		q:      q,
		rootID: rootID,
	}
}

func (r *SQLiteArtistRepository) Create(ctx context.Context, a Artist) (Artist, error) {
	row, err := r.db(ctx).CreateArtist(ctx, store.CreateArtistParams{
		ID:          string(a.ID),
		DisplayName: string(a.Name),
		FolderID:    db.ToNullString(string(a.FolderID)),
	})
	if err != nil {
		return Artist{}, r.mapError(err)
	}

	return Artist{
		ID:       ArtistID(row.ID),
		Name:     ArtistName(row.DisplayName),
		FolderID: FolderID(db.FromNullString(row.FolderID)),
	}, nil
}

func (r *SQLiteArtistRepository) ByID(ctx context.Context, id ArtistID) (Artist, error) {
	row, err := r.db(ctx).GetArtist(ctx, string(id))
	if err != nil {
		return Artist{}, r.mapError(err)
	}

	return Artist{
		ID:       ArtistID(row.ID),
		Name:     ArtistName(row.DisplayName),
		FolderID: FolderID(db.FromNullString(row.FolderID)),
	}, nil
}

func (r *SQLiteArtistRepository) All(ctx context.Context) ([]Artist, error) {
	rows, err := r.db(ctx).GetArtists(ctx)
	if err != nil {
		return nil, r.mapError(err)
	}

	artists := make([]Artist, 0, len(rows))
	for _, row := range rows {
		artists = append(artists, Artist{
			ID:       ArtistID(row.ID),
			Name:     ArtistName(row.DisplayName),
			FolderID: FolderID(db.FromNullString(row.FolderID)),
		})
	}

	return artists, nil
}

func (r *SQLiteArtistRepository) FolderID(ctx context.Context, id ArtistID) (FolderID, error) {
	a, err := r.db(ctx).GetArtist(ctx, string(id))
	if err != nil {
		return "", r.mapError(err)
	}

	return FolderID(db.FromNullString(a.FolderID)), nil
}

func (r *SQLiteArtistRepository) Update(ctx context.Context, id ArtistID, a Artist) (Artist, error) {
	row, err := r.db(ctx).UpdateArtist(ctx, store.UpdateArtistParams{
		OldID:       string(id),
		NewID:       string(a.ID),
		DisplayName: string(a.Name),
		FolderID:    db.ToNullString(string(a.FolderID)),
	})
	if err != nil {
		return Artist{}, r.mapError(err)
	}

	return Artist{
		ID:       ArtistID(row.ID),
		Name:     ArtistName(row.DisplayName),
		FolderID: FolderID(db.FromNullString(row.FolderID)),
	}, nil
}

func (r *SQLiteArtistRepository) Delete(ctx context.Context, id ArtistID) error {
	rows, err := r.db(ctx).DeleteArtist(ctx, string(id))
	if err != nil {
		return r.mapError(err)
	}

	if rows == 0 {
		return ErrArtistNotFound
	}

	return nil
}

func (r *SQLiteArtistRepository) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return r.q.WithTx(tx)
	}

	return r.q
}

// mapError translates storage-level failures into artist sentinels.
func (r *SQLiteArtistRepository) mapError(err error) error {
	return db.MapSQLError(err, ErrArtistNotFound, ErrArtistAlreadyExists)
}
