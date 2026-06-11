package library

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"playlist-manager/internal/appctx"
	"playlist-manager/internal/db"
	"playlist-manager/internal/store"
	"playlist-manager/internal/utils"
)

type Artist struct {
	ID       string
	FolderID string
	Artist   string
}

type (
	ArtistID   string
	ArtistName string
)

func (n ArtistName) ToSlugID() ArtistID {
	return ArtistID(utils.NameToSlug(string(n)))
}

func (n ArtistName) Validate() error {
	if strings.TrimSpace(string(n)) == "" {
		return fmt.Errorf("artist name cannot be empty")
	}
	return nil
}

type NArtist struct {
	ID       ArtistID
	Name     ArtistName
	FolderID FolderID
}

func scanArtist(s scannable) (Artist, error) {
	var a Artist
	var folderID sql.NullString

	err := s.Scan(&a.ID, &a.Artist, &folderID)
	a.FolderID = fromNull(folderID, RootID)

	return a, err
}

func (l *Library) GetArtist(id string) (Artist, error) {
	query := `SELECT id, display_name, folder_id FROM artists WHERE id = ?`

	a, err := scanArtist(l.db.QueryRow(query, id))

	return a, l.wrapErr(err, "artist", id)
}

func (l *Library) GetArtists() ([]Artist, error) {
	query := `SELECT id, display_name, folder_id FROM artists`

	rows, err := l.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query artists: %w", err)
	}
	defer rows.Close()

	var artists []Artist
	for rows.Next() {
		a, err := scanArtist(rows)
		if err != nil {
			return nil, err
		}
		artists = append(artists, a)
	}

	return artists, nil
}

func (l *Library) AddArtist(a Artist) error {
	query := `INSERT INTO artists (id, display_name, folder_id) VALUES (?, ?, ?)`

	_, err := l.db.Exec(
		query,
		strings.TrimSpace(a.ID),
		strings.TrimSpace(a.Artist),
		toNull(a.FolderID),
	)
	if err != nil {
		return fmt.Errorf("failed to add artist %q: %w", a.ID, err)
	}

	return nil
}

func (l *Library) RemoveArtist(id string) error {
	res, err := l.db.Exec("DELETE FROM artists WHERE id = ?", id)
	if err != nil {
		return l.wrapErr(err, "artist", id)
	}

	if rows, _ := res.RowsAffected(); rows == 0 {
		return l.wrapErr(sql.ErrNoRows, "artist", id)
	}

	return nil
}

func (l *Library) UpdateArtist(oldID string, updated Artist) error {
	oldArtist, err := l.GetArtist(oldID)
	if err != nil {
		return err
	}

	tx, err := l.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if oldArtist.FolderID != updated.FolderID {
		newRel, err := l.ResolveFolderPath(updated.FolderID)
		if err != nil {
			return fmt.Errorf("failed to resolve new folder path: %w", err)
		}

		rows, err := tx.Query(`SELECT id, relative_path FROM tracks WHERE artist_id = ?`, oldID)
		if err != nil {
			return fmt.Errorf("failed to query tracks for movement: %w", err)
		}

		type trackMove struct {
			ID      string
			OldPath string
			NewPath string
		}
		var moves []trackMove

		for rows.Next() {
			var tID, oldPath string
			if err := rows.Scan(&tID, &oldPath); err != nil {
				rows.Close()
				return err
			}

			filename := filepath.Base(filepath.FromSlash(oldPath))
			newPath := filename
			if newRel != "" {
				newPath = filepath.ToSlash(filepath.Join(newRel, filename))
			}
			moves = append(moves, trackMove{ID: tID, OldPath: oldPath, NewPath: newPath})
		}
		rows.Close()

		updateQuery := `UPDATE tracks SET relative_path = ? WHERE id = ?`
		for _, m := range moves {
			if _, err := tx.Exec(updateQuery, m.NewPath, m.ID); err != nil {
				return fmt.Errorf("failed to update track %q relative_path: %w", m.ID, err)
			}

			oldAbs := filepath.Join(l.Paths.Library, filepath.FromSlash(m.OldPath))
			newAbs := filepath.Join(l.Paths.Library, filepath.FromSlash(m.NewPath))

			if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
				return fmt.Errorf("failed to create destination directory: %w", err)
			}
			if err := os.Rename(oldAbs, newAbs); err != nil {
				return fmt.Errorf("failed to move track file on disk: %w", err)
			}
		}
	}

	query := `UPDATE artists SET id = ?, display_name = ?, folder_id = ? WHERE id = ?`
	if _, err := tx.Exec(query, strings.TrimSpace(updated.ID), updated.Artist, toNull(updated.FolderID), oldID); err != nil {
		return fmt.Errorf("failed to update artist in database: %w", err)
	}

	return tx.Commit()
}

type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type ArtistRepository interface {
	Create(ctx context.Context, a NArtist) error
	ByID(ctx context.Context, id ArtistID) (NArtist, error)
	All(ctx context.Context) ([]NArtist, error)
	Update(ctx context.Context, oldID ArtistID, a NArtist) error
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

func NewArtistService(r ArtistRepository, fc FolderChecker, tm ArtistTracksMover, t Transactor, root FolderID) *ArtistService {
	return &ArtistService{
		repo:          r,
		folderChecker: fc,
		trackMover:    tm,
		transactor:    t,
		rootFolder:    root,
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
			return fmt.Errorf("check folder fallback for artist %q: %w", id, err)
		}

		if exists {
			folderID = FolderID(id)
		} else {
			folderID = s.rootFolder
		}
	}

	a := NArtist{
		ID:       id,
		Name:     name,
		FolderID: folderID,
	}

	if err := s.repo.Create(ctx, a); err != nil {
		return fmt.Errorf("create artist %q: %w", a.ID, err)
	}

	return nil
}

func (s *ArtistService) ByID(ctx context.Context, id ArtistID) (NArtist, error) {
	a, err := s.repo.ByID(ctx, id)
	if err != nil {
		return NArtist{}, fmt.Errorf("get artist by id %q: %w", id, err)
	}

	return a, nil
}

func (s *ArtistService) All(ctx context.Context) ([]NArtist, error) {
	a, err := s.repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all artists: %w", err)
	}

	return a, nil
}

type ArtistUpdateParams struct {
	NewID  ArtistID
	Name   ArtistName
	Folder FolderID
}

func newIfPresent[T ~string](new, old T) T {
	if new != "" {
		return new
	} else {
		return old
	}
}

func (s *ArtistService) Update(ctx context.Context, oldID ArtistID, params ArtistUpdateParams) error {
	oldArtist, err := s.repo.ByID(ctx, oldID)
	if err != nil {
		return fmt.Errorf("update artist %q: %w", oldID, err)
	}

	newID := newIfPresent(params.NewID, oldID)
	newName := newIfPresent(params.Name, oldArtist.Name)
	newFolder := newIfPresent(params.Folder, oldArtist.FolderID)

	newArtist := NArtist{
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

func (r *SQLiteArtistRepository) Create(ctx context.Context, a NArtist) error {
	return r.db(ctx).CreateArtist(ctx, store.CreateArtistParams{
		ID:          string(a.ID),
		DisplayName: string(a.Name),
		FolderID:    db.ToNullString(a.FolderID.ToDB(r.rootID)),
	})
}

func (r *SQLiteArtistRepository) ByID(ctx context.Context, id ArtistID) (NArtist, error) {
	row, err := r.db(ctx).GetArtist(ctx, string(id))
	if err != nil {
		return NArtist{}, err
	}

	return NArtist{
		ID:       ArtistID(row.ID),
		Name:     ArtistName(row.DisplayName),
		FolderID: FolderID(db.FromNullString(row.FolderID)).FromDB(r.rootID),
	}, nil
}

func (r *SQLiteArtistRepository) All(ctx context.Context) ([]NArtist, error) {
	rows, err := r.db(ctx).GetArtists(ctx)
	if err != nil {
		return nil, err
	}

	artists := make([]NArtist, 0, len(rows))
	for _, row := range rows {
		artists = append(artists, NArtist{
			ID:       ArtistID(row.ID),
			Name:     ArtistName(row.DisplayName),
			FolderID: FolderID(db.FromNullString(row.FolderID)).FromDB(r.rootID),
		})
	}

	return artists, nil
}

func (r *SQLiteArtistRepository) Update(ctx context.Context, id ArtistID, a NArtist) error {
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
