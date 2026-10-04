package library

import (
	"cmp"
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

// SourceYouTube is the only source that has ever written rows. It is a
// constant so the spelling cannot drift between the import path and the
// backfill command.
const SourceYouTube = "youtube"

type Track struct {
	ID           TrackID
	Title        string
	Album        string
	ArtistID     ArtistID
	RelativePath TrackPath
	SourceID     string
	// Source names the origin platform. Kept as data now so adding sources
	// later is a value change, not a migration.
	Source string
	// DurationSec is the stream length in whole seconds. Zero means unknown,
	// which is distinct from a genuinely zero-length file and is the state
	// every row predating duration probing is in.
	DurationSec int64
	// Media holds the remaining stream properties from the same probe pass
	// that produced DurationSec. Not persisted yet; recorded so a future
	// codec-compatibility filter does not need a second full-library read.
	Media TrackMediaInfo
	// RawTitle is the title exactly as the file's tags carried it. Empty
	// means the row predates title parsing.
	RawTitle string
	// Performer and Version are parsed hints, not curated data: artist_id
	// remains the confirmed link.
	Performer string
	Version   string
}

type TrackCreateParams struct {
	ID           TrackID
	Title        string
	Album        string
	ArtistID     ArtistID
	RelativePath TrackPath
	SourceID     string
	// Source names the origin platform. Kept as data now so adding sources
	// later is a value change, not a migration.
	Source string
	// DurationSec is the probed stream length; 0 when unknown.
	DurationSec int64
	// Media carries probe results for the same pass as DurationSec.
	Media TrackMediaInfo
	// RawTitle, Performer and Version carry title parsing results; see
	// Track for their meaning.
	RawTitle  string
	Performer string
	Version   string
}

type TrackPathMove struct {
	ID      TrackID
	OldPath TrackPath
	NewPath TrackPath
}

type TrackRepository interface {
	Create(ctx context.Context, t Track) error
	ByID(ctx context.Context, id TrackID) (Track, error)
	UpdateTitle(ctx context.Context, id TrackID, title, rawTitle, performer, version string) error
	ByArtistID(ctx context.Context, artistID ArtistID) ([]Track, error)
	ByPathPattern(ctx context.Context, pattern string) ([]Track, error)
	ExistsBySourceID(ctx context.Context, sourceID string) (bool, error)
	Delete(ctx context.Context, id TrackID) error
	UpdatePathsByPrefix(ctx context.Context, oldPrefix, newPrefix string) error
	UpdatePathsBatch(ctx context.Context, moves []TrackPathMove) error
	// MissingDuration lists tracks whose duration is still 0, which is the
	// state every row predating duration probing is in.
	MissingDuration(ctx context.Context) ([]Track, error)
	UpdateDuration(ctx context.Context, id TrackID, seconds int64) error
}

type FolderResolver interface {
	ResolvePath(ctx context.Context, id FolderID) (string, error)
}

type PhysicalTrackMover interface {
	MoveFile(ctx context.Context, oldPath, newPath string) error
	DeleteFile(ctx context.Context, path string) error
}

// PathsProvider supplies the library's root directories. Satisfied by
// fs.AppPaths.
type PathsProvider interface {
	StagingDir() string
	LibraryDir() string
}

// TrackService manages track entities and their physical files.
type TrackService struct {
	repo           TrackRepository
	folderResolver FolderResolver
	diskMover      PhysicalTrackMover
	transactor     Transactor
	paths          PathsProvider
	// journal is optional: without it a deleted track leaves no record, and
	// a watched playlist that still offers the video re-downloads it.
	journal TrackJournal
}

type TrackServiceConfig struct {
	TrackRepo      TrackRepository
	FolderResolver FolderResolver
	DiskMover      PhysicalTrackMover
	Transactor     Transactor
	Paths          PathsProvider
	Journal        TrackJournal
}

func NewTrackService(cfg TrackServiceConfig) *TrackService {
	return &TrackService{
		repo:           cfg.TrackRepo,
		folderResolver: cfg.FolderResolver,
		diskMover:      cfg.DiskMover,
		transactor:     cfg.Transactor,
		paths:          cfg.Paths,
		journal:        cfg.Journal,
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
		Source:       cmp.Or(strings.TrimSpace(params.Source), SourceYouTube),
		RawTitle:     strings.TrimSpace(params.RawTitle),
		Performer:    strings.TrimSpace(params.Performer),
		Version:      strings.TrimSpace(params.Version),
		DurationSec:  params.DurationSec,
		Media:        params.Media,
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

		libDir := s.paths.LibraryDir()
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

// ByID fetches a single track; satisfies TrackFinder.
func (s *TrackService) ByID(ctx context.Context, id TrackID) (Track, error) {
	return s.repo.ByID(ctx, id)
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

		// Journalled inside the same transaction as the delete: a recorded
		// removal for a track that survived would suppress a download the
		// user never cancelled.
		if s.journal != nil {
			if err := s.journal.Record(txCtx, id, VideoID(track.SourceID), TrackEventDeleted); err != nil {
				return err
			}
		}

		absPath := filepath.Join(s.paths.LibraryDir(), filepath.FromSlash(string(track.RelativePath)))

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

func (r *SQLiteTrackRepository) MissingDuration(ctx context.Context) ([]Track, error) {
	rows, err := r.db(ctx).ListTracksMissingDuration(ctx)
	if err != nil {
		return nil, db.MapSQLError(err, ErrTrackNotFound, ErrTrackPathExists)
	}

	tracks := make([]Track, 0, len(rows))
	for _, row := range rows {
		tracks = append(tracks, rowToTrack(row.Track))
	}
	return tracks, nil
}

func (r *SQLiteTrackRepository) UpdateDuration(ctx context.Context, id TrackID, seconds int64) error {
	return r.db(ctx).UpdateTrackDuration(ctx, store.UpdateTrackDurationParams{
		ID:          string(id),
		DurationSec: db.ToNullInt64(seconds),
	})
}

func rowToTrack(row store.Track) Track {
	return Track{
		ID:           TrackID(row.ID),
		Title:        row.Title,
		Album:        db.FromNullString(row.Album),
		ArtistID:     ArtistID(db.FromNullString(row.ArtistID)),
		RelativePath: TrackPath(row.RelativePath),
		SourceID:     db.FromNullString(row.SourceID),
		Source:       row.Source,
		RawTitle:     db.FromNullString(row.RawTitle),
		Performer:    row.Performer,
		Version:      row.Version,
		DurationSec:  db.FromNullInt64(row.DurationSec),
	}
}

func (r *SQLiteTrackRepository) Create(ctx context.Context, t Track) error {
	err := r.db(ctx).CreateTrack(ctx, store.CreateTrackParams{
		ID:           string(t.ID),
		Title:        t.Title,
		Album:        db.ToNullString(t.Album),
		ArtistID:     db.ToNullString(string(t.ArtistID)),
		RelativePath: string(t.RelativePath),
		SourceID:     db.ToNullString(t.SourceID),
		Source:       t.Source,
		RawTitle:     db.ToNullString(t.RawTitle),
		Performer:    t.Performer,
		Version:      t.Version,
		DurationSec:  db.ToNullInt64(t.DurationSec),
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

func (r *SQLiteTrackRepository) UpdateTitle(ctx context.Context, id TrackID, title, rawTitle, performer, version string) error {
	err := r.db(ctx).UpdateTrackTitle(ctx, store.UpdateTrackTitleParams{
		ID:        string(id),
		Title:     title,
		RawTitle:  db.ToNullString(rawTitle),
		Performer: performer,
		Version:   version,
	})
	if err != nil {
		return fmt.Errorf("update title of track %q: %w", id, err)
	}

	return nil
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
