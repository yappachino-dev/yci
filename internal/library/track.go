package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/yappachino-dev/yci/internal/appctx"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/store"

	"github.com/google/uuid"
)

type (
	TrackID   string
	TrackPath string
)

// Sentinel errors for track lookups and writes.
var (
	ErrTrackNotFound   = errors.New("track not found")
	ErrTrackPathExists = errors.New("track path already exists")
)

type Track struct {
	ID           TrackID
	Title        string
	Album        string
	ArtistID     ArtistID
	FolderID     FolderID
	RelativePath TrackPath
	SourceID     string
}

type TrackCreateParams struct {
	ID           TrackID
	Title        string
	Album        string
	ArtistID     ArtistID
	FolderID     FolderID
	RelativePath TrackPath
	SourceID     string
}

type TrackPathMove struct {
	ID      TrackID
	OldPath TrackPath
	NewPath TrackPath
}

type TrackRepository interface {
	Create(ctx context.Context, t Track) error
	ByID(ctx context.Context, id TrackID) (Track, error)
	ByArtistID(ctx context.Context, artistID ArtistID) ([]Track, error)
	ByPathPattern(ctx context.Context, pattern string) ([]Track, error)
	ExistsBySourceID(ctx context.Context, sourceID string) (bool, error)
	Delete(ctx context.Context, id TrackID) error
	UpdatePathsByPrefix(ctx context.Context, oldPrefix, newPrefix string) error
	UpdatePathsBatch(ctx context.Context, moves []TrackPathMove) error
}

type FolderResolver interface {
	ResolvePath(ctx context.Context, id FolderID) (string, error)
}

type PhysicalTrackMover interface {
	MoveFile(ctx context.Context, oldPath, newPath string) error
	DeleteFile(ctx context.Context, path string) error
}

type StagingConfig interface {
	StagingDir() string
	LibraryDir() string
}

// TrackService manages track entities and their physical files.
type TrackService struct {
	repo           TrackRepository
	folderResolver FolderResolver
	diskMover      PhysicalTrackMover
	transactor     Transactor
	config         StagingConfig
}

type TrackServiceConfig struct {
	TrackRepo      TrackRepository
	FolderResolver FolderResolver
	DiskMover      PhysicalTrackMover
	Transactor     Transactor
	Config         StagingConfig
}

func NewTrackService(cfg TrackServiceConfig) *TrackService {
	return &TrackService{
		repo:           cfg.TrackRepo,
		folderResolver: cfg.FolderResolver,
		diskMover:      cfg.DiskMover,
		transactor:     cfg.Transactor,
		config:         cfg.Config,
	}
}

func (s *TrackService) Create(ctx context.Context, params TrackCreateParams) (Track, error) {
	if strings.TrimSpace(params.Title) == "" {
		return Track{}, fmt.Errorf("create track: title cannot be empty")
	}

	id := params.ID
	if id == "" {
		id = TrackID(uuid.New().String())
	}

	t := Track{
		ID:           id,
		Title:        strings.TrimSpace(params.Title),
		Album:        strings.TrimSpace(params.Album),
		ArtistID:     params.ArtistID,
		RelativePath: params.RelativePath,
		SourceID:     strings.TrimSpace(params.SourceID),
	}

	if err := s.repo.Create(ctx, t); err != nil {
		return Track{}, fmt.Errorf("create track %q: %w", t.Title, err)
	}

	return t, nil
}

func (s *TrackService) MoveByArtist(ctx context.Context, id ArtistID, oldFolder, newFolder FolderID) error {
	txErr := s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		// The read lives inside the transaction so the whole move is one
		// consistent unit; nested calls join via the transactor.
		tracks, err := s.repo.ByArtistID(txCtx, id)
		if err != nil {
			return fmt.Errorf("move tracks by artist: %w", err)
		}

		if len(tracks) == 0 {
			return nil
		}

		newRelFolder, err := s.folderResolver.ResolvePath(txCtx, newFolder)
		if err != nil {
			return fmt.Errorf("resolve artist target folder path: %w", err)
		}

		var moves []TrackPathMove

		for _, track := range tracks {
			filename := path.Base(string(track.RelativePath))
			var newRelPath string
			if newRelFolder == "" {
				newRelPath = filename
			} else {
				newRelPath = path.Join(newRelFolder, filename)
			}

			moves = append(moves, TrackPathMove{
				ID:      track.ID,
				OldPath: track.RelativePath,
				NewPath: TrackPath(newRelPath),
			})
		}

		if err := s.repo.UpdatePathsBatch(txCtx, moves); err != nil {
			return err
		}

		libDir := s.config.LibraryDir()
		for _, m := range moves {
			oldAbs := filepath.Join(libDir, filepath.FromSlash(string(m.OldPath)))
			newAbs := filepath.Join(libDir, filepath.FromSlash(string(m.NewPath)))

			if err := s.diskMover.MoveFile(txCtx, oldAbs, newAbs); err != nil {
				return fmt.Errorf("failed to move file on disk from %q to %q: %w", oldAbs, newAbs, err)
			}
		}

		return nil
	})

	if txErr != nil {
		return fmt.Errorf("move tracks for artist %q: %w", id, txErr)
	}

	return nil
}

func (s *TrackService) All(ctx context.Context) ([]Track, error) {
	return s.repo.ByPathPattern(ctx, "%")
}

func (s *TrackService) ExistsBySourceID(ctx context.Context, sourceID string) (bool, error) {
	return s.repo.ExistsBySourceID(ctx, sourceID)
}

func (s *TrackService) ByArtistID(ctx context.Context, artistID ArtistID) ([]Track, error) {
	return s.repo.ByArtistID(ctx, artistID)
}

func (s *TrackService) ByFolder(ctx context.Context, folderID FolderID, recursive bool) ([]Track, error) {
	folderPath, err := s.folderResolver.ResolvePath(ctx, folderID)
	if err != nil {
		return nil, err
	}

	pattern := "%"
	if folderPath != "" {
		pattern = folderPath + "/%"
	}

	tracks, err := s.repo.ByPathPattern(ctx, pattern)
	if err != nil {
		return nil, err
	}

	if recursive || folderPath == "" {
		return tracks, nil
	}

	var exactMatches []Track
	for _, t := range tracks {
		if path.Clean(path.Dir(string(t.RelativePath))) == path.Clean(folderPath) {
			exactMatches = append(exactMatches, t)
		}
	}

	return exactMatches, nil
}

func (s *TrackService) Delete(ctx context.Context, id TrackID) error {
	track, err := s.repo.ByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find track %q: %w", id, err)
	}

	txErr := s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.Delete(txCtx, id); err != nil {
			return err
		}

		absPath := filepath.Join(s.config.LibraryDir(), filepath.FromSlash(string(track.RelativePath)))

		if err := s.diskMover.DeleteFile(txCtx, absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to drop file on physical drive: %w", err)
		}

		return nil
	})

	if txErr != nil {
		return fmt.Errorf("delete track: %w", txErr)
	}

	return nil
}

// SQLiteTrackRepository implements TrackRepository over sqlc queries.
type SQLiteTrackRepository struct {
	q *store.Queries
}

func NewSQLiteTrackRepository(q *store.Queries) *SQLiteTrackRepository {
	return &SQLiteTrackRepository{q: q}
}

func rowToTrack(row store.Track) Track {
	return Track{
		ID:           TrackID(row.ID),
		Title:        row.Title,
		Album:        db.FromNullString(row.Album),
		ArtistID:     ArtistID(db.FromNullString(row.ArtistID)),
		FolderID:     FolderID(db.FromNullString(row.FolderID)),
		RelativePath: TrackPath(row.RelativePath),
		SourceID:     db.FromNullString(row.SourceID),
	}
}

func (r *SQLiteTrackRepository) Create(ctx context.Context, t Track) error {
	err := r.db(ctx).CreateTrack(ctx, store.CreateTrackParams{
		ID:           string(t.ID),
		Title:        t.Title,
		Album:        db.ToNullString(t.Album),
		ArtistID:     db.ToNullString(string(t.ArtistID)),
		FolderID:     db.ToNullString(string(t.FolderID)),
		RelativePath: string(t.RelativePath),
		SourceID:     db.ToNullString(t.SourceID),
	})
	if err != nil {
		return db.MapSQLError(err, ErrTrackNotFound, ErrTrackPathExists)
	}

	return nil
}

func (r *SQLiteTrackRepository) ByID(ctx context.Context, id TrackID) (Track, error) {
	row, err := r.db(ctx).GetTrack(ctx, string(id))
	if err != nil {
		return Track{}, r.mapError(err)
	}

	return rowToTrack(row.Track), nil
}

func (r *SQLiteTrackRepository) ByArtistID(ctx context.Context, artistID ArtistID) ([]Track, error) {
	rows, err := r.db(ctx).GetTracksByArtist(ctx, db.ToNullString(string(artistID)))
	if err != nil {
		return nil, err
	}

	tracks := make([]Track, 0, len(rows))
	for _, row := range rows {
		tracks = append(tracks, rowToTrack(row.Track))
	}

	return tracks, nil
}

func (r *SQLiteTrackRepository) ByPathPattern(ctx context.Context, pattern string) ([]Track, error) {
	rows, err := r.db(ctx).GetTracksByPath(ctx, pattern)
	if err != nil {
		return nil, err
	}

	tracks := make([]Track, 0, len(rows))
	for _, row := range rows {
		tracks = append(tracks, rowToTrack(row.Track))
	}

	return tracks, nil
}

func (r *SQLiteTrackRepository) ExistsBySourceID(ctx context.Context, sourceID string) (bool, error) {
	return r.db(ctx).CheckTracksBySourceID(ctx, db.ToNullString(sourceID))
}

func (r *SQLiteTrackRepository) Delete(ctx context.Context, id TrackID) error {
	rows, err := r.db(ctx).DeleteTrack(ctx, string(id))
	if err != nil {
		return r.mapError(err)
	}

	if rows == 0 {
		return ErrTrackNotFound
	}

	return nil
}

func (r *SQLiteTrackRepository) UpdatePathsByPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	likePattern := oldPrefix + "%"

	return r.db(ctx).UpdateTrackPathsByPrefix(ctx, store.UpdateTrackPathsByPrefixParams{
		OldPrefix:   oldPrefix,
		NewPrefix:   newPrefix,
		MatchPrefix: likePattern,
	})
}

func (r *SQLiteTrackRepository) UpdatePathsBatch(ctx context.Context, moves []TrackPathMove) error {
	for _, m := range moves {
		err := r.db(ctx).UpdateTrackPath(ctx, store.UpdateTrackPathParams{
			ID:           string(m.ID),
			RelativePath: string(m.NewPath),
		})
		if err != nil {
			return fmt.Errorf("update track path %q: %w", m.ID, err)
		}
	}

	return nil
}

func (r *SQLiteTrackRepository) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}

// mapError translates storage-level failures into track sentinels.
func (r *SQLiteTrackRepository) mapError(err error) error {
	return db.MapSQLError(err, ErrTrackNotFound, ErrTrackPathExists)
}
