package library

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/store"

	_ "modernc.org/sqlite"
)

// newSentinelTestDB opens a database with the schema subset under test
// (mirrors migrations 001 + 004).
func newSentinelTestDB(t *testing.T) *store.Queries {
	t.Helper()

	dbConn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	const schema = `
		CREATE TABLE folders (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			parent_id TEXT,
			FOREIGN KEY (parent_id) REFERENCES folders (id)
		);

		CREATE TABLE artists (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			folder_id TEXT,
			FOREIGN KEY (folder_id) REFERENCES folders (id)
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
			FOREIGN KEY (artist_id) REFERENCES artists (id),
			FOREIGN KEY (folder_id) REFERENCES folders (id)
		);
	`

	if _, err := dbConn.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return store.New(dbConn)
}

// TestRepositorySentinels verifies each repository method returns its
// documented sentinel, not a driver error or a sibling entity's sentinel.
func TestRepositorySentinels(t *testing.T) {
	queries := newSentinelTestDB(t)
	ctx := context.Background()

	folderRepo := NewSQLiteFolderRepository(queries, FolderID(RootID))
	artistRepo := NewSQLiteArtistRepository(queries, FolderID(RootID))
	trackRepo := NewSQLiteTrackRepository(queries)
	channelRepo := NewSQLiteChannelRepository(queries)

	tests := []struct {
		name      string
		exercise  func() error
		wantError error
	}{
		{
			name: "folder ByID on missing id",
			exercise: func() error {
				_, err := folderRepo.ByID(ctx, "missing")
				return err
			},
			wantError: ErrFolderNotFound,
		},
		{
			name: "folder Delete on missing id",
			exercise: func() error {
				return folderRepo.Delete(ctx, "missing")
			},
			wantError: ErrFolderNotFound,
		},
		{
			name: "folder Create on duplicate id",
			exercise: func() error {
				_, err := folderRepo.Create(ctx, Folder{ID: "dup", Name: "Dup"})
				if err != nil {
					return err
				}

				_, err = folderRepo.Create(ctx, Folder{ID: "dup", Name: "Dup"})
				return err
			},
			wantError: ErrFolderAlreadyExists,
		},
		{
			name: "artist ByID on missing id",
			exercise: func() error {
				_, err := artistRepo.ByID(ctx, "missing")
				return err
			},
			wantError: ErrArtistNotFound,
		},
		{
			name: "artist Delete on missing id",
			exercise: func() error {
				return artistRepo.Delete(ctx, "missing")
			},
			wantError: ErrArtistNotFound,
		},
		{
			name: "artist Create on duplicate id",
			exercise: func() error {
				_, err := artistRepo.Create(ctx, Artist{ID: "dup", Name: "Dup"})
				if err != nil {
					return err
				}

				_, err = artistRepo.Create(ctx, Artist{ID: "dup", Name: "Dup"})
				return err
			},
			wantError: ErrArtistAlreadyExists,
		},
		{
			name: "track ByID on missing id",
			exercise: func() error {
				_, err := trackRepo.ByID(ctx, "missing")
				return err
			},
			wantError: ErrTrackNotFound,
		},
		{
			name: "track Delete on missing id",
			exercise: func() error {
				return trackRepo.Delete(ctx, "missing")
			},
			wantError: ErrTrackNotFound,
		},
		{
			name: "track Create on duplicate path",
			exercise: func() error {
				first := Track{ID: "t1", Title: "One", RelativePath: "same.opus"}
				if err := trackRepo.Create(ctx, first); err != nil {
					return err
				}

				return trackRepo.Create(ctx, Track{ID: "t2", Title: "Two", RelativePath: "same.opus"})
			},
			wantError: ErrTrackPathExists,
		},
		{
			name: "channel Unlink on non-linked pair",
			exercise: func() error {
				return channelRepo.Delete(ctx, "ghost-artist", "ghost-channel")
			},
			wantError: ErrChannelNotLinked,
		},
		{
			name: "channel Link duplicate pair",
			exercise: func() error {
				if _, err := artistRepo.Create(ctx, Artist{ID: "a1", Name: "One"}); err != nil {
					return err
				}

				if err := channelRepo.Create(ctx, "a1", "Channel"); err != nil {
					return err
				}

				return channelRepo.Create(ctx, "a1", "Channel")
			},
			wantError: ErrChannelAlreadyLinked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.exercise()
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("err = %v, want %v", err, tt.wantError)
			}

			// Driver text must not escape the repository boundary.
			if err != nil && containsDriverText(err) {
				t.Errorf("error leaks driver detail: %v", err)
			}
		})
	}
}

// containsDriverText reports raw driver phrasing that must not escape
// repositories.
func containsDriverText(err error) bool {
	return strings.Contains(err.Error(), "sql:") ||
		strings.Contains(err.Error(), "SQLITE_")
}

// TestMapSQLErrorLeavesUnknownErrorsUntouched verifies pass-through.
func TestMapSQLErrorLeavesUnknownErrorsUntouched(t *testing.T) {
	sentinel := errors.New("boom")

	got := db.MapSQLError(sentinel, ErrArtistNotFound, ErrArtistAlreadyExists)
	if !errors.Is(got, sentinel) {
		t.Fatalf("unrelated error mutated: got %v, want %v", got, sentinel)
	}
}
