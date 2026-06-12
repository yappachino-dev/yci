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

	"playlist-manager/internal/appctx"
	"playlist-manager/internal/db"
	"playlist-manager/internal/store"
	"playlist-manager/internal/utils"

	"github.com/google/uuid"
)

var ErrProcessAborted = errors.New("processing aborted by user")

type (
	TrackID       string
	TrackPath     string
	StagingAction int
)

const (
	ActionImport StagingAction = iota
	ActionSkip
	ActionAbort
)

type StagingResult struct {
	Action StagingAction
	Artist ArtistID
}

type Track struct {
	ID           TrackID
	Title        string
	Album        string
	ArtistID     ArtistID
	RelativePath TrackPath
	SourceID     string
}

type TrackCreateParams struct {
	ID           TrackID
	Title        string
	Album        string
	ArtistID     ArtistID
	RelativePath TrackPath
	SourceID     string
}

type TrackUpdateParams struct {
	ID           TrackID
	Title        string
	Album        string
	ArtistID     ArtistID
	RelativePath TrackPath
	SourceID     string
}

type TrackPathMove struct {
	ID      TrackID
	OldPath TrackPath
	NewPath TrackPath
}

type TrackMetadata struct {
	Title  string
	Album  string
	Artist string
}

type TrackRepository interface {
	Create(ctx context.Context, t Track) error
	ByID(ctx context.Context, id TrackID) (Track, error)
	ByArtistID(ctx context.Context, artistID ArtistID) ([]Track, error)
	ByPathPattern(ctx context.Context, pattern string) ([]Track, error)
	ExistsBySourceID(ctx context.Context, sourceID string) (bool, error)
	Update(ctx context.Context, id TrackID, t Track) error
	Delete(ctx context.Context, id TrackID) error
	UpdatePathsByPrefix(ctx context.Context, oldPrefix, newPrefix string) error
	UpdatePathsBatch(ctx context.Context, moves []TrackPathMove) error
}

type MetadataReader interface {
	Read(ctx context.Context, path string) (TrackMetadata, error)
}

type ArtistReader interface {
	FolderID(ctx context.Context, id ArtistID) (FolderID, error)
}

type ChannelLinker interface {
	Link(ctx context.Context, artistID ArtistID, name ChannelName) error
	ResolveArtist(ctx context.Context, name ChannelName) (ArtistID, bool, error)
	All(ctx context.Context) ([]Channel, error)
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

type LinkHandler func(channelName string, title string) (StagingResult, error)

type TrackService struct {
	repo           TrackRepository
	artistReader   ArtistReader
	folderResolver FolderResolver
	linker         ChannelLinker
	metaReader     MetadataReader
	diskMover      PhysicalTrackMover
	transactor     Transactor
	config         StagingConfig
}

func NewTrackService(
	r TrackRepository,
	ar ArtistReader,
	fr FolderResolver,
	cl ChannelLinker,
	mr MetadataReader,
	dm PhysicalTrackMover,
	t Transactor,
	cfg StagingConfig,
) *TrackService {
	return &TrackService{
		repo:           r,
		artistReader:   ar,
		folderResolver: fr,
		linker:         cl,
		metaReader:     mr,
		diskMover:      dm,
		transactor:     t,
		config:         cfg,
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

func (s *TrackService) UpdatePathByPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	if err := s.repo.UpdatePathsByPrefix(ctx, oldPrefix, newPrefix); err != nil {
		return fmt.Errorf("bulk update track paths from %q to %q: %w", oldPrefix, newPrefix, err)
	}

	return nil
}

func (s *TrackService) MoveByArtist(ctx context.Context, id ArtistID, oldFolder, newFolder FolderID) error {
	tracks, err := s.repo.ByArtistID(ctx, id)
	if err != nil {
		return fmt.Errorf("move tracks by artist: %w", err)
	}

	if len(tracks) == 0 {
		return nil
	}

	newRelFolder, err := s.folderResolver.ResolvePath(ctx, newFolder)
	if err != nil {
		return fmt.Errorf("resolve artist target folder path: %w", err)
	}

	txErr := s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
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

func (s *TrackService) ProcessStaging(ctx context.Context, onUnknown LinkHandler) error {
	stagingDir := s.config.StagingDir()

	files, err := os.ReadDir(stagingDir)
	if err != nil {
		return fmt.Errorf("read staging directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".opus" {
			continue
		}

		if err := s.processSingleStagingFile(ctx, file.Name(), onUnknown); err != nil {
			if errors.Is(err, ErrProcessAborted) {
				return err
			}

			fmt.Printf("Error processing staging file %s: %v\n", file.Name(), err)
			continue
		}
	}

	return nil
}

func (s *TrackService) processSingleStagingFile(ctx context.Context, fileName string, onUnknown LinkHandler) error {
	oldPathNative := filepath.Join(s.config.StagingDir(), fileName)

	m, err := s.metaReader.Read(ctx, oldPathNative)
	if err != nil {
		return fmt.Errorf("extract track metadata: %w", err)
	}

	channelName := ChannelName(strings.TrimSpace(m.Artist))
	title := strings.TrimSpace(m.Title)
	sourceID := strings.TrimSuffix(fileName, ".opus")

	artistID, ok, err := s.linker.ResolveArtist(ctx, channelName)
	if err != nil {
		return err
	}

	if !ok {
		res, err := onUnknown(string(channelName), title)
		if err != nil {
			return err
		}

		switch res.Action {
		case ActionSkip:
			return nil
		case ActionAbort:
			return ErrProcessAborted
		case ActionImport:
			artistID = res.Artist
		}

		if err := s.linker.Link(ctx, artistID, channelName); err != nil {
			return fmt.Errorf("failed linking unknown channel to artist: %w", err)
		}
	}

	folderID, err := s.artistReader.FolderID(ctx, artistID)
	if err != nil {
		return fmt.Errorf("failed tracking artist folder target: %w", err)
	}

	folderPath, err := s.folderResolver.ResolvePath(ctx, folderID)
	if err != nil {
		return fmt.Errorf("failed resolving destination path: %w", err)
	}

	safeTitle := utils.SanitizeFilename(title)
	newFilename := safeTitle + ".opus"

	var relPath string
	if folderPath == "" {
		relPath = newFilename
	} else {
		relPath = path.Join(folderPath, newFilename)
	}

	absPathNative := filepath.Join(s.config.LibraryDir(), filepath.FromSlash(relPath))

	txErr := s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		params := TrackCreateParams{
			ID:           TrackID(uuid.New().String()),
			Title:        title,
			Album:        m.Album,
			ArtistID:     artistID,
			RelativePath: TrackPath(relPath),
			SourceID:     sourceID,
		}

		if _, err := s.Create(txCtx, params); err != nil {
			return err
		}

		if err := s.diskMover.MoveFile(txCtx, oldPathNative, absPathNative); err != nil {
			return fmt.Errorf("move staging file to library: %w", err)
		}

		return nil
	})

	return txErr
}

func (s *TrackService) Delete(ctx context.Context, id TrackID) error {
	track, err := s.repo.ByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find track metadata for deletion: %w", err)
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

type SQLiteTrackRepository struct {
	q *store.Queries
}

func NewSQLiteTrackRepository(q *store.Queries) *SQLiteTrackRepository {
	return &SQLiteTrackRepository{q: q}
}

func (r *SQLiteTrackRepository) Create(ctx context.Context, t Track) error {
	return r.db(ctx).CreateTrack(ctx, store.CreateTrackParams{
		ID:           string(t.ID),
		Title:        t.Title,
		Album:        db.ToNullString(t.Album),
		ArtistID:     db.ToNullString(string(t.ArtistID)),
		RelativePath: string(t.RelativePath),
		SourceID:     db.ToNullString(t.SourceID),
	})
}

func (r *SQLiteTrackRepository) ByID(ctx context.Context, id TrackID) (Track, error) {
	row, err := r.db(ctx).GetTrack(ctx, string(id))
	if err != nil {
		return Track{}, err
	}

	return Track{
		ID:           TrackID(row.ID),
		Title:        row.Title,
		Album:        db.FromNullString(row.Album),
		ArtistID:     ArtistID(db.FromNullString(row.ArtistID)),
		RelativePath: TrackPath(row.RelativePath),
		SourceID:     db.FromNullString(row.SourceID),
	}, nil
}

func (r *SQLiteTrackRepository) ByArtistID(ctx context.Context, artistID ArtistID) ([]Track, error) {
	rows, err := r.db(ctx).GetTracksByArtist(ctx, db.ToNullString(string(artistID)))
	if err != nil {
		return nil, err
	}

	tracks := make([]Track, 0, len(rows))
	for _, row := range rows {
		tracks = append(tracks, Track{
			ID:           TrackID(row.ID),
			Title:        row.Title,
			Album:        db.FromNullString(row.Album),
			ArtistID:     ArtistID(db.FromNullString(row.ArtistID)),
			RelativePath: TrackPath(row.RelativePath),
			SourceID:     db.FromNullString(row.SourceID),
		})
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
		tracks = append(tracks, Track{
			ID:           TrackID(row.ID),
			Title:        row.Title,
			Album:        db.FromNullString(row.Album),
			ArtistID:     ArtistID(db.FromNullString(row.ArtistID)),
			RelativePath: TrackPath(row.RelativePath),
			SourceID:     db.FromNullString(row.SourceID),
		})
	}
	return tracks, nil
}

func (r *SQLiteTrackRepository) ExistsBySourceID(ctx context.Context, sourceID string) (bool, error) {
	return r.db(ctx).CheckTracksBySourceID(ctx, db.ToNullString(sourceID))
}

func (r *SQLiteTrackRepository) Update(ctx context.Context, id TrackID, t Track) error {
	return r.db(ctx).UpdateTrack(ctx, store.UpdateTrackParams{
		OldID:        string(id),
		NewID:        string(t.ID),
		Title:        t.Title,
		Album:        db.ToNullString(t.Album),
		ArtistID:     db.ToNullString(string(t.ArtistID)),
		RelativePath: string(t.RelativePath),
		SourceID:     db.ToNullString(t.SourceID),
	})
}

func (r *SQLiteTrackRepository) Delete(ctx context.Context, id TrackID) error {
	return r.db(ctx).DeleteTrack(ctx, string(id))
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
			return fmt.Errorf("execute batch tracking update for entry %q: %w", m.ID, err)
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
