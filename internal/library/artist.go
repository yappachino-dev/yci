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

type (
	ArtistID   string
	ArtistName string
)

func (n ArtistName) ToSlugID() ArtistID {
	return ArtistID(nameToSlug(string(n)))
}

func (n ArtistName) Validate() error {
	if strings.TrimSpace(string(n)) == "" {
		return fmt.Errorf("artist name cannot be empty")
	}
	return nil
}

type Artist struct {
	ID       ArtistID
	Name     ArtistName
	FolderID FolderID
}

type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type ArtistRepository interface {
	Create(ctx context.Context, a Artist) error
	ByID(ctx context.Context, id ArtistID) (Artist, error)
	All(ctx context.Context) ([]Artist, error)
	Update(ctx context.Context, oldID ArtistID, a Artist) error
	Delete(ctx context.Context, id ArtistID) error
}

type FolderChecker interface {
	Exists(ctx context.Context, id FolderID) (bool, error)
}

type ArtistTracksMover interface {
	MoveByArtist(ctx context.Context, id ArtistID, oldFolder, newFolder FolderID) error
}

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

func (s *ArtistService) Create(ctx context.Context, id ArtistID, folderID FolderID, name ArtistName) error {
	if err := name.Validate(); err != nil {
		return fmt.Errorf("create artist: %w", err)
	}

	if id == "" {
		id = name.ToSlugID()
	}

	if folderID == "" {
		exists, err := s.folderChecker.Exists(ctx, FolderID(id))
		if err != nil {
			return fmt.Errorf("folder fallback for artist %q: %w", id, err)
		}

		if exists {
			folderID = FolderID(id)
		} else {
			folderID = s.rootFolder
		}
	}

	a := Artist{
		ID:       id,
		Name:     name,
		FolderID: folderID,
	}

	if err := s.repo.Create(ctx, a); err != nil {
		return fmt.Errorf("create artist %q: %w", a.ID, err)
	}

	return nil
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

type ArtistUpdateParams struct {
	NewID  ArtistID
	Name   ArtistName
	Folder FolderID
}

func (s *ArtistService) Update(ctx context.Context, oldID ArtistID, params ArtistUpdateParams) error {
	oldArtist, err := s.repo.ByID(ctx, oldID)
	if err != nil {
		return fmt.Errorf("update artist %q: %w", oldID, err)
	}

	newID := newIfPresent(params.NewID, oldID)
	newName := newIfPresent(params.Name, oldArtist.Name)
	newFolder := newIfPresent(params.Folder, oldArtist.FolderID)

	newArtist := Artist{
		ID:       newID,
		Name:     newName,
		FolderID: newFolder,
	}

	if oldArtist.FolderID != newFolder {
		txErr := s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			if err := s.trackMover.MoveByArtist(txCtx, oldID, oldArtist.FolderID, newFolder); err != nil {
				return err
			}

			if err := s.repo.Update(txCtx, oldID, newArtist); err != nil {
				return err
			}

			return nil
		})

		if txErr != nil {
			return fmt.Errorf("update artist %q: %w", oldID, txErr)
		}

		return nil
	}

	if err := s.repo.Update(ctx, oldID, newArtist); err != nil {
		return fmt.Errorf("update artist %q: %w", oldID, err)
	}

	return nil
}

func (s *ArtistService) Delete(ctx context.Context, id ArtistID) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete artist: %w", err)
	}

	return nil
}

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

func (r *SQLiteArtistRepository) Create(ctx context.Context, a Artist) error {
	return r.db(ctx).CreateArtist(ctx, store.CreateArtistParams{
		ID:          string(a.ID),
		DisplayName: string(a.Name),
		FolderID:    db.ToNullString(a.FolderID.ToDB(r.rootID)),
	})
}

func (r *SQLiteArtistRepository) ByID(ctx context.Context, id ArtistID) (Artist, error) {
	row, err := r.db(ctx).GetArtist(ctx, string(id))
	if err != nil {
		return Artist{}, err
	}

	return Artist{
		ID:       ArtistID(row.ID),
		Name:     ArtistName(row.DisplayName),
		FolderID: FolderID(db.FromNullString(row.FolderID)).FromDB(r.rootID),
	}, nil
}

func (r *SQLiteArtistRepository) All(ctx context.Context) ([]Artist, error) {
	rows, err := r.db(ctx).GetArtists(ctx)
	if err != nil {
		return nil, err
	}

	artists := make([]Artist, 0, len(rows))
	for _, row := range rows {
		artists = append(artists, Artist{
			ID:       ArtistID(row.ID),
			Name:     ArtistName(row.DisplayName),
			FolderID: FolderID(db.FromNullString(row.FolderID)).FromDB(r.rootID),
		})
	}

	return artists, nil
}

func (r *SQLiteArtistRepository) FolderID(ctx context.Context, id ArtistID) (FolderID, error) {
	a, err := r.db(ctx).GetArtist(ctx, string(id))
	if err != nil {
		return "", err
	}

	return FolderID(db.FromNullString(a.FolderID)).FromDB(r.rootID), nil
}

func (r *SQLiteArtistRepository) Update(ctx context.Context, id ArtistID, a Artist) error {
	return r.db(ctx).UpdateArtist(ctx, store.UpdateArtistParams{
		OldID:       string(id),
		NewID:       string(a.ID),
		DisplayName: string(a.Name),
		FolderID:    db.ToNullString(a.FolderID.ToDB(r.rootID)),
	})
}

func (r *SQLiteArtistRepository) Delete(ctx context.Context, id ArtistID) error {
	return r.db(ctx).DeleteArtist(ctx, string(id))
}

func (r *SQLiteArtistRepository) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return r.q.WithTx(tx)
	}

	return r.q
}
