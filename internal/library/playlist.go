package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yappachino-dev/yci/internal/appctx"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/store"
)

// Playlist domain errors.
var (
	ErrPlaylistNotFound       = errors.New("playlist not found")
	ErrPlaylistAlreadyExists  = errors.New("playlist already exists")
	ErrTrackAlreadyInPlaylist = errors.New("track already in playlist")
	ErrTrackNotInPlaylist     = errors.New("track not in playlist")
)

// Playlist is a named, ordered sequence of tracks persisted in the database.
// Exported .m3u files are regenerable artifacts of this state, never a
// second source of truth.
type Playlist struct {
	ID   int64
	Name string
	// SeedTrackID is the track the playlist was seeded from, or empty for
	// a playlist that was created empty or built by hand.
	SeedTrackID TrackID
	// FragmentTokens is the title-token set chosen at seed time. It is the
	// recipe: re-deriving membership means re-running this token set, not
	// re-evaluating a stored rule.
	FragmentTokens []string
	// MatchMode is how those tokens combine. Defaults to MatchAll for a
	// playlist written before the column existed, so an old recipe does not
	// silently change meaning.
	MatchMode MatchMode
}

// HasRecipe reports whether the playlist was seeded at all.
//
// Tokens are the signal, not the seed track: seed_track_id is a foreign key
// with ON DELETE SET NULL, so deleting the seed track blanks it while the
// tokens survive. Reading the seed as the marker would then tell a user who
// did seed a playlist that they never did, and send them looking for the
// wrong problem.
func (p Playlist) HasRecipe() bool {
	return len(p.FragmentTokens) > 0
}

// HasSeed reports whether the recipe can actually be evaluated. A playlist
// whose seed track was deleted still has a recipe but no way to apply it.
func (p Playlist) HasSeed() bool {
	return p.SeedTrackID != ""
}

// PlaylistEntry pairs a track with its playback order within one playlist.
type PlaylistEntry struct {
	Track    Track
	Duration int // seconds; 0 when unknown
	Position int64
}

// PlaylistRepository persists playlists and their membership.
type PlaylistRepository interface {
	Create(ctx context.Context, name string) (Playlist, error)
	SetRecipe(ctx context.Context, id int64, seedTrackID TrackID, tokensJSON string, mode MatchMode) error
	Rename(ctx context.Context, id int64, name string) error
	SetMode(ctx context.Context, id int64, mode MatchMode) error
	All(ctx context.Context) ([]Playlist, error)
	ByName(ctx context.Context, name string) (Playlist, error)
	Delete(ctx context.Context, id int64) error
	AddTrack(ctx context.Context, playlistID int64, trackID TrackID) error
	RemoveTrack(ctx context.Context, playlistID int64, trackID TrackID) error
	Tracks(ctx context.Context, playlistID int64) ([]PlaylistEntry, error)
}

type SQLitePlaylistRepository struct{ q *store.Queries }

func NewSQLitePlaylistRepository(q *store.Queries) *SQLitePlaylistRepository {
	return &SQLitePlaylistRepository{q: q}
}

func (r *SQLitePlaylistRepository) Create(ctx context.Context, name string) (Playlist, error) {
	row, err := r.db(ctx).CreatePlaylist(ctx, name)
	if err != nil {
		return Playlist{}, db.MapSQLError(err, nil, ErrPlaylistAlreadyExists)
	}

	return playlistFromRow(row)
}

func (r *SQLitePlaylistRepository) All(ctx context.Context) ([]Playlist, error) {
	rows, err := r.db(ctx).GetPlaylists(ctx)
	if err != nil {
		return nil, fmt.Errorf("list playlists: %w", err)
	}

	out := make([]Playlist, 0, len(rows))
	for _, row := range rows {
		p, err := playlistFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}

	return out, nil
}

// db resolves the queries to the transaction carried by ctx, falling back to
// the pool. Every other repository has this; without it a multi-row write
// deadlocks against SQLite's single writer.
func (r *SQLitePlaylistRepository) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}

func (r *SQLitePlaylistRepository) ByName(ctx context.Context, name string) (Playlist, error) {
	row, err := r.db(ctx).GetPlaylistByName(ctx, name)
	if err != nil {
		return Playlist{}, db.MapSQLError(err, ErrPlaylistNotFound, nil)
	}

	return playlistFromRow(row)
}

// SetRecipe records how the playlist was seeded. It runs inside the same
// transaction as the seeded membership, so a playlist never exists holding
// rows it cannot explain.
func (r *SQLitePlaylistRepository) SetRecipe(ctx context.Context, id int64, seedTrackID TrackID, tokensJSON string, mode MatchMode) error {
	if err := r.db(ctx).SetPlaylistRecipe(ctx, store.SetPlaylistRecipeParams{
		ID:             id,
		SeedTrackID:    db.ToNullString(string(seedTrackID)),
		FragmentTokens: tokensJSON,
		FragmentMode:   mode.String(),
	}); err != nil {
		return fmt.Errorf("set playlist recipe: %w", err)
	}
	return nil
}

// Rename changes a playlist's name. The unique constraint is
// COLLATE NOCASE, so a conflict maps to the same sentinel as creation.
func (r *SQLitePlaylistRepository) Rename(ctx context.Context, id int64, name string) error {
	if err := r.db(ctx).RenamePlaylist(ctx, store.RenamePlaylistParams{ID: id, Name: name}); err != nil {
		return db.MapSQLError(err, nil, ErrPlaylistAlreadyExists)
	}
	return nil
}

// SetMode changes how the recipe's tokens combine, without touching
// membership.
func (r *SQLitePlaylistRepository) SetMode(ctx context.Context, id int64, mode MatchMode) error {
	return r.db(ctx).SetPlaylistMode(ctx, store.SetPlaylistModeParams{ID: id, FragmentMode: mode.String()})
}

// playlistFromRow decodes the stored recipe. A corrupt token column means
// the playlist can no longer be explained, which is worth surfacing rather
// than silently treating the playlist as hand-built.
func playlistFromRow(row store.Playlist) (Playlist, error) {
	p := Playlist{
		ID:          row.ID,
		Name:        row.Name,
		SeedTrackID: TrackID(db.FromNullString(row.SeedTrackID)),
		// An empty mode means a row written before the column existed.
		MatchMode: ParseMatchMode(row.FragmentMode),
	}

	if row.FragmentTokens != "" && row.FragmentTokens != "[]" {
		if err := json.Unmarshal([]byte(row.FragmentTokens), &p.FragmentTokens); err != nil {
			return Playlist{}, fmt.Errorf("playlist %q has an unreadable recipe: %w", row.Name, err)
		}
	}

	return p, nil
}

func (r *SQLitePlaylistRepository) Delete(ctx context.Context, id int64) error {
	n, err := r.db(ctx).DeletePlaylist(ctx, id)
	if err != nil {
		return fmt.Errorf("delete playlist %d: %w", id, err)
	}
	if n == 0 {
		return ErrPlaylistNotFound
	}

	return nil
}

func (r *SQLitePlaylistRepository) AddTrack(ctx context.Context, playlistID int64, trackID TrackID) error {
	err := r.db(ctx).AddTrackToPlaylist(ctx, store.AddTrackToPlaylistParams{
		PlaylistID: playlistID,
		TrackID:    string(trackID),
	})
	if err != nil {
		return db.MapSQLError(err, nil, ErrTrackAlreadyInPlaylist)
	}

	return nil
}

func (r *SQLitePlaylistRepository) RemoveTrack(ctx context.Context, playlistID int64, trackID TrackID) error {
	n, err := r.db(ctx).RemoveTrackFromPlaylist(ctx, store.RemoveTrackFromPlaylistParams{
		PlaylistID: playlistID,
		TrackID:    string(trackID),
	})
	if err != nil {
		return fmt.Errorf("remove track %s from playlist %d: %w", trackID, playlistID, err)
	}
	if n == 0 {
		return ErrTrackNotInPlaylist
	}

	return nil
}

func (r *SQLitePlaylistRepository) Tracks(ctx context.Context, playlistID int64) ([]PlaylistEntry, error) {
	rows, err := r.db(ctx).GetPlaylistTracks(ctx, playlistID)
	if err != nil {
		return nil, fmt.Errorf("list playlist tracks: %w", err)
	}

	out := make([]PlaylistEntry, 0, len(rows))
	for _, row := range rows {
		t := row.Track
		out = append(out, PlaylistEntry{
			Track:    rowToTrack(t),
			Duration: int(db.FromNullInt64(t.DurationSec)),
			Position: row.Position,
		})
	}

	return out, nil
}

// TrackFinder supplies track existence checks without the full TrackService.
type TrackFinder interface {
	ByID(ctx context.Context, id TrackID) (Track, error)
	// All backs recipe derivation, which matches a token set against the
	// whole library rather than a single track.
	All(ctx context.Context) ([]Track, error)
}

// PlaylistServiceConfig follows the config-struct rule: four collaborators.
type PlaylistServiceConfig struct {
	Repo       PlaylistRepository
	Tracks     TrackFinder
	Paths      PathsProvider
	Transactor Transactor
}

// PlaylistService manages playlist entities and renders them to .m3u files.
type PlaylistService struct {
	repo       PlaylistRepository
	tracks     TrackFinder
	paths      PathsProvider
	transactor Transactor
}

func NewPlaylistService(cfg PlaylistServiceConfig) *PlaylistService {
	return &PlaylistService{
		repo:       cfg.Repo,
		tracks:     cfg.Tracks,
		paths:      cfg.Paths,
		transactor: cfg.Transactor,
	}
}

func (s *PlaylistService) Create(ctx context.Context, name string) (Playlist, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Playlist{}, fmt.Errorf("create playlist: name cannot be empty")
	}

	p, err := s.repo.Create(ctx, name)
	if err != nil {
		return Playlist{}, fmt.Errorf("create playlist %q: %w", name, err)
	}

	return p, nil
}

func (s *PlaylistService) All(ctx context.Context) ([]Playlist, error) {
	return s.repo.All(ctx)
}

func (s *PlaylistService) ByName(ctx context.Context, name string) (Playlist, error) {
	return s.repo.ByName(ctx, strings.TrimSpace(name))
}

// Delete removes the playlist; membership rows cascade. Files on disk are
// not touched — exported .m3u artifacts are the user's to remove.
func (s *PlaylistService) Delete(ctx context.Context, name string) error {
	p, err := s.repo.ByName(ctx, strings.TrimSpace(name))
	if err != nil {
		return fmt.Errorf("find playlist %q: %w", name, err)
	}

	if err := s.repo.Delete(ctx, p.ID); err != nil {
		return fmt.Errorf("delete playlist %q: %w", name, err)
	}

	return nil
}

// AddTrack appends an existing track to the end of the playlist.
func (s *PlaylistService) AddTrack(ctx context.Context, name string, trackID TrackID) error {
	return s.AddTracks(ctx, name, []TrackID{trackID})
}

// AddTracks materializes a whole playlist in one transaction. Building a
// playlist from a matched set adds dozens of rows, and committing them
// individually would leave a half-built playlist behind on any failure,
// which reads as a deliberate partial result.
func (s *PlaylistService) AddTracks(ctx context.Context, name string, trackIDs []TrackID) error {
	if len(trackIDs) == 0 {
		return nil
	}

	p, err := s.repo.ByName(ctx, strings.TrimSpace(name))
	if err != nil {
		return fmt.Errorf("find playlist %q: %w", name, err)
	}

	return s.transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		for _, trackID := range trackIDs {
			if _, err := s.tracks.ByID(txCtx, trackID); err != nil {
				return fmt.Errorf("find track %s: %w", trackID, err)
			}
			if err := s.repo.AddTrack(txCtx, p.ID, trackID); err != nil {
				return fmt.Errorf("add track %s to playlist %q: %w", trackID, name, err)
			}
		}
		return nil
	})
}

func (s *PlaylistService) RemoveTrack(ctx context.Context, name string, trackID TrackID) error {
	p, err := s.repo.ByName(ctx, strings.TrimSpace(name))
	if err != nil {
		return fmt.Errorf("find playlist %q: %w", name, err)
	}

	if err := s.repo.RemoveTrack(ctx, p.ID, trackID); err != nil {
		return fmt.Errorf("remove track %s from playlist %q: %w", trackID, name, err)
	}

	return nil
}

// Entries returns the playlist's tracks in playback order.
func (s *PlaylistService) Entries(ctx context.Context, name string) ([]PlaylistEntry, error) {
	p, err := s.repo.ByName(ctx, strings.TrimSpace(name))
	if err != nil {
		return nil, fmt.Errorf("find playlist %q: %w", name, err)
	}

	return s.repo.Tracks(ctx, p.ID)
}

// ExportDir is the subdirectory of the library holding rendered playlists.
// Living inside the library means sync tooling carries it alongside the
// music it references.
const ExportDir = "playlists"

// Export renders the playlist to <library>/playlists/<slug>.m3u with paths
// relative to the file's own directory (standard m3u semantics), so the
// bundle stays portable across machines with different absolute layouts.
// Rendering is wholesale and idempotent: the database is the only source
// of truth, hand edits to exported files are overwritten.
func (s *PlaylistService) Export(ctx context.Context, name string) (string, error) {
	p, err := s.repo.ByName(ctx, strings.TrimSpace(name))
	if err != nil {
		return "", fmt.Errorf("find playlist %q: %w", name, err)
	}

	entries, err := s.repo.Tracks(ctx, p.ID)
	if err != nil {
		return "", fmt.Errorf("list tracks of playlist %q: %w", name, err)
	}

	slug := SanitizeForExport(p.Name)
	dir := filepath.Join(s.paths.LibraryDir(), ExportDir)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create playlist export directory: %w", err)
	}

	path := filepath.Join(dir, slug+".m3u")
	content := RenderM3U(p.Name, entries, "../")

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write playlist file %q: %w", path, err)
	}

	return path, nil
}
