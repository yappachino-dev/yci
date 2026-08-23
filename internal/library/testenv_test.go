package library

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/fs"
	"github.com/yappachino-dev/yci/internal/store"

	_ "modernc.org/sqlite"
)

// newLibraryDB opens a database with the schema subset the repositories rely
// on (mirrors migrations 001 + 004).
func newLibraryDB(t *testing.T) *store.Queries {
	return store.New(newLibraryDBConn(t))
}

func newLibraryDBConn(t *testing.T) *sql.DB {
	t.Helper()

	dbConn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	// Match initApplication: the FK actions in the schema below are only
	// enforced while the pragma is on.
	if _, err := dbConn.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	const schema = `
		CREATE TABLE folders (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			parent_id TEXT,
			FOREIGN KEY (parent_id) REFERENCES folders (id) ON DELETE CASCADE ON UPDATE CASCADE
		);

		CREATE TABLE artists (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			folder_id TEXT,
			FOREIGN KEY (folder_id) REFERENCES folders (id) ON DELETE SET NULL ON UPDATE CASCADE
		);

		CREATE TABLE channels (
			id INTEGER PRIMARY KEY,
			artist_id TEXT NOT NULL,
			channel_name TEXT,
			FOREIGN KEY (artist_id) REFERENCES artists (id),
			UNIQUE (artist_id, channel_name)
		);

		CREATE TABLE tracks (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			album TEXT,
			artist_id TEXT,
			duration_sec INTEGER DEFAULT 0,
			relative_path TEXT NOT NULL UNIQUE,
			source_id TEXT,
			folder_id TEXT,
			FOREIGN KEY (artist_id) REFERENCES artists (id) ON DELETE SET NULL ON UPDATE CASCADE,
			FOREIGN KEY (folder_id) REFERENCES folders (id) ON DELETE SET NULL ON UPDATE CASCADE
		);

		CREATE TABLE fs_journal (
			id INTEGER PRIMARY KEY,
			op TEXT NOT NULL CHECK (op IN ('move_file', 'move_dir', 'delete_file')),
			src TEXT NOT NULL,
			dst TEXT,
			status TEXT NOT NULL CHECK (status IN ('pending', 'done', 'failed')) DEFAULT 'pending',
			error TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`

	if _, err := dbConn.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	// Match migration 003: the root folder row exists.
	if _, err := dbConn.Exec(`INSERT INTO folders (id, display_name, parent_id) VALUES ('root', 'Root', NULL)`); err != nil {
		t.Fatalf("seed root folder: %v", err)
	}

	return dbConn
}

// testPaths implements StagingConfig against temporary directories.
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
	txr      *db.SQLiteTransactor
	actions  *db.FileActionExecutor
	folders  *FolderService
	artists  *ArtistService
	tracks   *TrackService
	channels *ChannelService
	staging  *StagingService
	repos    struct {
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
	env := &libraryEnv{dbConn: dbConn}

	rootID := FolderID(RootID)
	env.paths = testPaths{lib: t.TempDir(), staging: t.TempDir()}

	faRepo := db.NewFileActionRepository(queries)
	mover := fs.NewOSDiskMover()
	env.actions = db.NewFileActionExecutor(faRepo, mover)
	env.txr = db.NewSQLiteTransactor(dbConn)
	env.txr.SetPostCommit(env.actions.ExecutePending)
	journaled := db.NewJournaledMover(faRepo, mover)

	env.repos.folders = NewSQLiteFolderRepository(queries, rootID)
	env.repos.artists = NewSQLiteArtistRepository(queries, rootID)
	env.repos.tracks = NewSQLiteTrackRepository(queries)
	channelRepo := NewSQLiteChannelRepository(queries)

	env.channels = NewChannelService(channelRepo)
	env.folders = NewFolderService(FolderServiceConfig{
		FolderRepo:  env.repos.folders,
		TrackWriter: env.repos.tracks,
		DiskMover:   journaled,
		Transactor:  env.txr,
		RootFolder:  rootID,
		Config:      env.paths,
	})
	env.tracks = NewTrackService(TrackServiceConfig{
		TrackRepo:      env.repos.tracks,
		FolderResolver: env.folders,
		DiskMover:      journaled,
		Transactor:     env.txr,
		Config:         env.paths,
	})
	env.artists = NewArtistService(ArtistServiceConfig{
		ArtistRepo:    env.repos.artists,
		FolderChecker: env.repos.folders,
		TrackMover:    env.tracks,
		Transactor:    env.txr,
		RootFolder:    rootID,
	})
	env.staging = NewStagingService(StagingServiceConfig{
		TrackCreator:   env.tracks,
		ArtistReader:   env.repos.artists,
		FolderResolver: env.folders,
		Linker:         env.channels,
		MetaReader:     NewTagMetadataReader(),
		DiskMover:      journaled,
		Transactor:     env.txr,
		Config:         env.paths,
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
		payload := append([]byte{0}, text...) // encoding byte + text
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
