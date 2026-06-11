package library

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"playlist-manager/internal/appctx"
	"playlist-manager/internal/db"
	"playlist-manager/internal/store"
	"playlist-manager/internal/utils"

	"github.com/google/uuid"
)

type Track struct {
	ID           string
	Title        string
	Album        string
	ArtistID     string
	DurationSec  int
	RelativePath string
	SourceID     string
}

type (
	TrackID   string
	TrackPath string
)

type NTrack struct {
	ID           TrackID
	Title        string
	Album        string
	ArtistID     ArtistID
	RelativePath TrackPath
	SourceID     string
}

func scanTrack(s scannable) (Track, error) {
	var t Track
	var aID, album, source sql.NullString

	err := s.Scan(&t.ID, &t.Title, &album, &aID, &t.DurationSec, &t.RelativePath, &source)
	if err != nil {
		return t, err
	}

	t.ArtistID = fromNull(aID, "")
	t.Album = fromNull(album, "")
	t.SourceID = fromNull(source, "")

	return t, nil
}

func (l *Library) GetAllTracks() ([]Track, error) {
	query := `SELECT id, title, album, artist_id, duration_sec, relative_path, source_id FROM tracks`
	return l.queryTracks(query)
}

func (l *Library) GetTracksByArtist(artistID string) ([]Track, error) {
	query := `SELECT id, title, album, artist_id, duration_sec, relative_path, source_id
              FROM tracks WHERE artist_id = ?`
	return l.queryTracks(query, artistID)
}

func (l *Library) GetTracksByFolder(folderID string, recursive bool) ([]Track, error) {
	folderPath, err := l.ResolveFolderPath(folderID)
	if err != nil {
		return nil, err
	}

	likeTarget := ""
	if folderPath != "" {
		likeTarget = folderPath + "/%"
	} else {
		likeTarget = "%"
	}

	query := `SELECT id, title, album, artist_id, duration_sec, relative_path, source_id 
	          FROM tracks WHERE relative_path LIKE ?`

	tracks, err := l.queryTracks(query, likeTarget)
	if err != nil {
		return nil, err
	}

	if recursive || folderPath == "" {
		return tracks, nil
	}

	var exactMatches []Track
	for _, t := range tracks {
		if filepath.ToSlash(filepath.Dir(t.RelativePath)) == folderPath {
			exactMatches = append(exactMatches, t)
		}
	}

	return exactMatches, nil
}

func (l *Library) queryTracks(query string, args ...any) ([]Track, error) {
	rows, err := l.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("track query failed: %w", err)
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}

	return tracks, nil
}

var ytRegex = regexp.MustCompile(`(?:youtube\.com\/(?:[^\/]+\/.+\/|(?:v|e(?:mbed)?)\/|.*[?&]v=)|youtu\.be\/)([^"&?\/\s]{11})`)

func (l *Library) ExtractSourceID(url string) string {
	matches := ytRegex.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (l *Library) TrackExistsBySourceID(sourceID string) (bool, error) {
	if sourceID == "" {
		return false, nil
	}

	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM tracks WHERE source_id = ?)"

	err := l.db.QueryRow(query, sourceID).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

func (l *Library) AddTrack(t Track) error {
	query := `
		INSERT INTO tracks (id, title, album, artist_id, duration_sec, relative_path, source_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := l.db.Exec(
		query,
		t.ID,
		t.Title,
		toNull(t.Album),
		toNull(t.ArtistID),
		t.DurationSec,
		t.RelativePath,
		toNull(t.SourceID),
	)
	if err != nil {
		return fmt.Errorf("failed to add track %q: %w", t.Title, err)
	}

	return nil
}

func (l *Library) RemoveTrack(id string) error {
	var relativePath string

	err := l.db.QueryRow("SELECT relative_path FROM tracks WHERE id = ?", id).Scan(&relativePath)
	if err != nil {
		return l.wrapErr(err, "track", id)
	}

	absPath := filepath.Join(l.Paths.Library, relativePath)
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete physical file for track %q: %w", id, err)
	}

	_, err = l.db.Exec("DELETE FROM tracks WHERE id = ?", id)

	return l.wrapErr(err, "track", id)
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
	Create(ctx context.Context, t NTrack) error
	ByID(ctx context.Context, id TrackID) (NTrack, error)
	ByArtistID(ctx context.Context, artistID ArtistID) ([]NTrack, error)
	ByPathPattern(ctx context.Context, pattern string) ([]NTrack, error)
	ExistsBySourceID(ctx context.Context, sourceID string) (bool, error)
	Update(ctx context.Context, id TrackID, t NTrack) error
	Delete(ctx context.Context, id TrackID) error
	UpdatePathsByPrefix(ctx context.Context, oldPrefix, newPrefix string) error
	UpdatePathsBatch(ctx context.Context, moves []TrackPathMove) error
}

type MetadataReader interface {
	Read(ctx context.Context, path string) (TrackMetadata, error)
}

type ChannelLinker interface {
	LookupArtistByChannel(ctx context.Context, channel string) (ArtistID, error)
	LinkChannelToArtist(ctx context.Context, artistID ArtistID, channel string) error
	UnlinkChannel(ctx context.Context, artistID ArtistID, channel string) error
	ArtistFolderID(ctx context.Context, id ArtistID) (FolderID, error)
	AllChannels(ctx context.Context) ([]Channel, error)
}

type FolderPathResolver interface {
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

type NLinkHandler func(channelName string, title string) (artistID string, err error)

type TrackService struct {
	repo           TrackRepository
	folderResolver FolderPathResolver
	linker         ChannelLinker
	metaReader     MetadataReader
	diskMover      PhysicalTrackMover
	transactor     Transactor
	config         StagingConfig
}

func NewTrackService(
	r TrackRepository,
	fr FolderPathResolver,
	cl ChannelLinker,
	mr MetadataReader,
	dm PhysicalTrackMover,
	t Transactor,
	cfg StagingConfig,
) *TrackService {
	return &TrackService{
		repo:           r,
		folderResolver: fr,
		linker:         cl,
		metaReader:     mr,
		diskMover:      dm,
		transactor:     t,
		config:         cfg,
	}
}

func (s *TrackService) Create(ctx context.Context, params TrackCreateParams) (NTrack, error) {
	if strings.TrimSpace(params.Title) == "" {
		return NTrack{}, fmt.Errorf("create track: title cannot be empty")
	}

	id := params.ID
	if id == "" {
		id = TrackID(uuid.New().String())
	}

	t := NTrack{
		ID:           id,
		Title:        strings.TrimSpace(params.Title),
		Album:        strings.TrimSpace(params.Album),
		ArtistID:     params.ArtistID,
		RelativePath: params.RelativePath,
		SourceID:     strings.TrimSpace(params.SourceID),
	}

	if err := s.repo.Create(ctx, t); err != nil {
		return NTrack{}, fmt.Errorf("create track %q: %w", t.Title, err)
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
		return fmt.Errorf("move tracks by artist: failed to resolve target folder: %w", err)
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
		return fmt.Errorf("move tracks execution for artist %q failed: %w", id, txErr)
	}

	return nil
}

func (s *TrackService) All(ctx context.Context) ([]NTrack, error) {
	return s.repo.ByPathPattern(ctx, "%")
}

func (s *TrackService) ExistsBySourceID(ctx context.Context, sourceID string) (bool, error) {
	return s.repo.ExistsBySourceID(ctx, sourceID)
}

func (s *TrackService) ByArtistID(ctx context.Context, artistID ArtistID) ([]NTrack, error) {
	return s.repo.ByArtistID(ctx, artistID)
}

func (s *TrackService) ByFolder(ctx context.Context, folderID FolderID, recursive bool) ([]NTrack, error) {
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

	var exactMatches []NTrack
	for _, t := range tracks {
		if path.Clean(path.Dir(string(t.RelativePath))) == path.Clean(folderPath) {
			exactMatches = append(exactMatches, t)
		}
	}

	return exactMatches, nil
}

func (s *TrackService) ProcessStaging(ctx context.Context, onUnknown NLinkHandler) error {
	stagingDir := s.config.StagingDir()
	files, err := os.ReadDir(stagingDir)
	if err != nil {
		return fmt.Errorf("could not read staging directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".opus" {
			continue
		}

		if err := s.processSingleStagingFile(ctx, file.Name(), onUnknown); err != nil {
			fmt.Printf("Error processing staging file %s: %v\n", file.Name(), err)
			continue
		}
	}

	return nil
}

func (s *TrackService) processSingleStagingFile(ctx context.Context, fileName string, onUnknown NLinkHandler) error {
	oldPathNative := filepath.Join(s.config.StagingDir(), fileName)

	m, err := s.metaReader.Read(ctx, oldPathNative)
	if err != nil {
		return fmt.Errorf("metadata extraction failed: %w", err)
	}

	channelName := strings.TrimSpace(m.Artist)
	title := strings.TrimSpace(m.Title)
	sourceID := strings.TrimSuffix(fileName, ".opus")

	artistID, err := s.linker.LookupArtistByChannel(ctx, channelName)
	if err != nil {
		return err
	}

	if artistID == "" {
		res, err := onUnknown(channelName, title)
		if err != nil {
			return err
		}

		artistID = ArtistID(res)

		if artistID == "skip" || artistID == "" {
			return nil
		}

		if err := s.linker.LinkChannelToArtist(ctx, artistID, channelName); err != nil {
			return fmt.Errorf("failed linking unknown channel to artist: %w", err)
		}
	}

	folderID, err := s.linker.ArtistFolderID(ctx, artistID)
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
			return fmt.Errorf("disk movement failed: %w", err)
		}

		return nil
	})

	return txErr
}

func (s *TrackService) Delete(ctx context.Context, id TrackID) error {
	track, err := s.repo.ByID(ctx, id)
	if err != nil {
		return fmt.Errorf("delete track: failed to find track meta: %w", err)
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

func (r *SQLiteTrackRepository) Create(ctx context.Context, t NTrack) error {
	return r.db(ctx).CreateTrack(ctx, store.CreateTrackParams{
		ID:           string(t.ID),
		Title:        t.Title,
		Album:        db.ToNullString(t.Album),
		ArtistID:     db.ToNullString(string(t.ArtistID)),
		RelativePath: string(t.RelativePath),
		SourceID:     db.ToNullString(t.SourceID),
	})
}

func (r *SQLiteTrackRepository) ByID(ctx context.Context, id TrackID) (NTrack, error) {
	row, err := r.db(ctx).GetTrack(ctx, string(id))
	if err != nil {
		return NTrack{}, err
	}

	return NTrack{
		ID:           TrackID(row.ID),
		Title:        row.Title,
		Album:        db.FromNullString(row.Album),
		ArtistID:     ArtistID(db.FromNullString(row.ArtistID)),
		RelativePath: TrackPath(row.RelativePath),
		SourceID:     db.FromNullString(row.SourceID),
	}, nil
}

func (r *SQLiteTrackRepository) ByArtistID(ctx context.Context, artistID ArtistID) ([]NTrack, error) {
	rows, err := r.db(ctx).GetTracksByArtist(ctx, db.ToNullString(string(artistID)))
	if err != nil {
		return nil, err
	}

	tracks := make([]NTrack, 0, len(rows))
	for _, row := range rows {
		tracks = append(tracks, NTrack{
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

func (r *SQLiteTrackRepository) ByPathPattern(ctx context.Context, pattern string) ([]NTrack, error) {
	rows, err := r.db(ctx).GetTracksByPath(ctx, pattern)
	if err != nil {
		return nil, err
	}

	tracks := make([]NTrack, 0, len(rows))
	for _, row := range rows {
		tracks = append(tracks, NTrack{
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

func (r *SQLiteTrackRepository) Update(ctx context.Context, id TrackID, t NTrack) error {
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
			return fmt.Errorf("failed executing batch tracking update for entry %q: %w", m.ID, err)
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
