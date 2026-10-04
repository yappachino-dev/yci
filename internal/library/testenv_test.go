package library

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/fs"
	"github.com/yappachino-dev/yci/internal/probe"
	"github.com/yappachino-dev/yci/internal/store"
)

// newLibraryDB opens a database carrying the full real migration chain.
func newLibraryDB(t *testing.T) *store.Queries {
	return store.New(newLibraryDBConn(t))
}

// newLibraryDBConn opens a database migrated with the real schema from
// db/migrations — no hand-maintained fixture, so new columns and tables
// cannot drift out from under the tests (the failure mode that motivated
// this switch).
func newLibraryDBConn(t *testing.T) *sql.DB {
	t.Helper()

	if err := db.SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}

	dbConn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	if err := db.RunMigrations(t.Context(), dbConn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	return dbConn
}

// testPaths implements PathsProvider against temporary directories.
type testPaths struct {
	lib     string
	staging string
}

func (p testPaths) LibraryDir() string { return p.lib }
func (p testPaths) StagingDir() string { return p.staging }

// libraryEnv wires the full library stack the way app.NewContainer does:
// real SQLite, real temporary directories, journaled file moves flushed
// through the transactor's post-commit hook.
type libraryEnv struct {
	dbConn   *sql.DB
	queries  *store.Queries
	txr      *db.SQLiteTransactor
	actions  *db.FileActionExecutor
	folders  *FolderService
	artists  *ArtistService
	tracks   *TrackService
	channels *ChannelService
	// testFolder caches folders shared by helpers that seed several artists.
	testFolder     map[string]FolderID
	staging        *StagingService
	playlists      *PlaylistService
	journaledMover *db.JournaledMover
	trackJournal   *TrackJournalService
	sources        *SourceService
	repos          struct {
		folders *SQLiteFolderRepository
		artists *SQLiteArtistRepository
		tracks  *SQLiteTrackRepository
	}
	paths testPaths
}

func newLibraryEnv(t *testing.T) *libraryEnv {
	t.Helper()

	dbConn := newLibraryDBConn(t)
	queries := store.New(dbConn)
	env := &libraryEnv{dbConn: dbConn, queries: queries}

	rootID := FolderID(RootID)
	env.paths = testPaths{lib: t.TempDir(), staging: t.TempDir()}

	faRepo := db.NewFileActionRepository(queries)
	mover := fs.NewOSDiskMover()
	env.actions = db.NewFileActionExecutor(faRepo, mover)
	env.txr = db.NewSQLiteTransactor(dbConn)
	env.txr.SetPostCommit(env.actions.ExecutePending)
	journaled := db.NewJournaledMover(faRepo, mover)
	env.journaledMover = journaled

	env.repos.folders = NewSQLiteFolderRepository(queries, rootID)
	env.repos.artists = NewSQLiteArtistRepository(queries, rootID)
	env.repos.tracks = NewSQLiteTrackRepository(queries)
	channelRepo := NewSQLiteChannelRepository(queries)

	env.channels = NewChannelService(channelRepo)
	env.trackJournal = NewTrackJournalService(NewSQLiteTrackJournal(queries))
	env.sources = NewSourceService(NewSQLiteSourceRepository(queries))
	env.folders = NewFolderService(FolderServiceConfig{
		FolderRepo:  env.repos.folders,
		TrackWriter: env.repos.tracks,
		DiskMover:   journaled,
		Transactor:  env.txr,
		RootFolder:  rootID,
		Paths:       env.paths,
	})
	env.tracks = NewTrackService(TrackServiceConfig{
		TrackRepo:      env.repos.tracks,
		FolderResolver: env.folders,
		DiskMover:      journaled,
		Transactor:     env.txr,
		Paths:          env.paths,
		Journal:        env.trackJournal,
	})
	env.artists = NewArtistService(ArtistServiceConfig{
		ArtistRepo:    env.repos.artists,
		FolderChecker: env.repos.folders,
		FolderCreator: env.folders,
		TrackMover:    env.tracks,
		Transactor:    env.txr,
		RootFolder:    rootID,
	})
	env.staging = NewStagingService(StagingServiceConfig{
		TrackCreator:   env.tracks,
		ArtistReader:   env.repos.artists,
		FolderResolver: env.folders,
		Linker:         env.channels,
		MetaReader:     NewFileMetadataReader(probe.NewFFprobe()),
		DiskMover:      journaled,
		Transactor:     env.txr,
		Paths:          env.paths,
	})

	env.playlists = NewPlaylistService(PlaylistServiceConfig{
		Repo:       NewSQLitePlaylistRepository(queries),
		Tracks:     env.tracks,
		Paths:      env.paths,
		Transactor: env.txr,
	})

	return env
}

// mustCreateFolder creates a folder, deriving the slug ID from the name.
func (e *libraryEnv) mustCreateFolder(t *testing.T, name string) Folder {
	t.Helper()

	f, err := e.folders.Create(context.Background(), "", "", FolderName(name))
	if err != nil {
		t.Fatalf("create folder %q: %v", name, err)
	}

	return f
}

// mustPutTrack registers a track row and places a physical file at its
// declared location.
func (e *libraryEnv) mustPutTrack(t *testing.T, id, title, relPath string) Track {
	t.Helper()

	abs := filepath.Join(e.paths.LibraryDir(), filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("audio:"+title), 0o644); err != nil {
		t.Fatal(err)
	}

	track, err := e.tracks.Create(context.Background(), TrackCreateParams{
		ID:           TrackID(id),
		Title:        title,
		RelativePath: TrackPath(relPath),
	})
	if err != nil {
		t.Fatalf("create track %q: %v", id, err)
	}

	return track
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}

	return string(data)
}

// writeTaggedAudio writes a minimal ID3v2.3-tagged file whose tags the real
// TagMetadataReader can parse.
func writeTaggedAudio(t *testing.T, path, title, artist string) {
	t.Helper()

	frame := func(id, text string) []byte {
		// 0x03 declares UTF-8; the default 0x00 would be ISO-8859-1 and
		// mangle every non-ASCII tag, which the real corpus is full of.
		payload := append([]byte{0x03}, []byte(text)...)
		b := make([]byte, 0, 10+len(payload))
		b = append(b, id...)
		size := uint32(len(payload))
		b = append(b, byte(size>>24), byte(size>>16), byte(size>>8), byte(size))
		b = append(b, 0, 0) // flags
		return append(b, payload...)
	}

	body := append(frame("TIT2", title), frame("TPE1", artist)...)
	total := uint32(len(body))
	header := []byte{'I', 'D', '3', 3, 0, 0,
		byte(total >> 21 & 0x7f), byte(total >> 14 & 0x7f), byte(total >> 7 & 0x7f), byte(total & 0x7f)}

	if err := os.WriteFile(path, append(append(header, body...), []byte("audio-bytes")...), 0o644); err != nil {
		t.Fatalf("write tagged audio %q: %v", path, err)
	}
}

// TestLibraryFixtureEnforcesForeignKeys guards the test harness itself.
//
// Every library test used to enable foreign keys with a one-off
// `dbConn.Exec("PRAGMA foreign_keys = ON")` in this file. That sets the
// pragma on whichever pooled connection served the call and no other, so
// tests that depended on cascading deletes were exercising a schema whose
// constraints were off — and would have kept passing while the constraint
// they were testing never fired. Fixtures now go through db.Open, which sets
// it per connection; this asserts that rather than trusting it.
func TestLibraryFixtureEnforcesForeignKeys(t *testing.T) {
	dbConn := newLibraryDBConn(t)

	enabled, err := db.ForeignKeysEnabled(dbConn)
	if err != nil {
		t.Fatalf("ForeignKeysEnabled: %v", err)
	}
	if !enabled {
		t.Fatal("test fixture has foreign_keys disabled; cascade assertions below are meaningless")
	}

	// Assert the real behaviour on the real schema, not just the pragma.
	// playlist_tracks cascades from playlists.
	if _, err := dbConn.Exec(`INSERT INTO playlists (name) VALUES ('p');`); err != nil {
		t.Fatalf("insert playlist: %v", err)
	}
	var playlistID int64
	if err := dbConn.QueryRow(`SELECT id FROM playlists WHERE name = 'p';`).Scan(&playlistID); err != nil {
		t.Fatalf("read playlist id: %v", err)
	}
	if _, err := dbConn.Exec(
		`INSERT INTO tracks (id, title, relative_path) VALUES ('t', 'T', 'a/t.mp3');`,
	); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := dbConn.Exec(
		`INSERT INTO playlist_tracks (playlist_id, track_id, position) VALUES (?, 't', 0);`,
		playlistID,
	); err != nil {
		t.Fatalf("insert membership: %v", err)
	}

	if _, err := dbConn.Exec(`DELETE FROM playlists WHERE id = ?;`, playlistID); err != nil {
		t.Fatalf("delete playlist: %v", err)
	}

	var remaining int
	if err := dbConn.QueryRow(
		`SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id = ?;`, playlistID,
	).Scan(&remaining); err != nil {
		t.Fatalf("count membership: %v", err)
	}
	if remaining != 0 {
		t.Errorf("ON DELETE CASCADE did not fire: %d membership row(s) survived", remaining)
	}
}
