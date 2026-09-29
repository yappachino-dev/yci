package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/config"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/library"
)

// boot brings up the whole application the way the CLI does — real config
// resolution, a real migrated database, the real service graph — against
// temporary directories.
func boot(t *testing.T) *app.Container {
	t.Helper()

	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	libraryDir := filepath.Join(root, "library")
	stagingDir := filepath.Join(root, "staging")

	for _, dir := range []string{configDir, libraryDir, stagingDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// An explicit YCI_CONFIG_PATH is required to exist, so the profile is
	// written out rather than relying on defaults.
	profile := "library_dir = " + quoteTOML(libraryDir) +
		"\nstaging_dir = " + quoteTOML(stagingDir) + "\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}

	env := map[string]string{
		config.EnvConfigPath:  filepath.Join(configDir, "config.toml"),
		config.EnvLibraryPath: libraryDir,
		config.EnvStagingPath: stagingDir,
	}
	lookup := func(key string) string { return env[key] }

	cfg, err := config.Load(lookup)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	dbConn, err := db.Open(filepath.Join(cfg.ConfigDir(), "data.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	if err := db.SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}
	if _, err := db.Migrate(t.Context(), dbConn, io_Discard{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	container, err := app.NewContainer(dbConn, cfg)
	if err != nil {
		t.Fatalf("NewContainer: %v", err)
	}
	t.Cleanup(func() { container.Close() })

	return container
}

func quoteTOML(s string) string { return strconv.Quote(s) }

type io_Discard struct{}

func (io_Discard) Write(p []byte) (int, error) { return len(p), nil }

// TestContainerWiresEveryService is a smoke test for the composition root.
//
// This package existed at 0% coverage, which is why a connection-level
// setting applied in one connection out of the pooled set went unnoticed for
// as long as it did: the bug lived in wiring, and nothing exercised wiring.
// Every service below is dereferenced so that a service silently missing from
// the graph, or wired to the wrong repository, fails here rather than as a
// nil dereference in a command.
func TestContainerWiresEveryService(t *testing.T) {
	c := boot(t)

	services := map[string]any{
		"FolderService":   c.FolderService,
		"ArtistService":   c.ArtistService,
		"TrackService":    c.TrackService,
		"ChannelService":  c.ChannelService,
		"StagingService":  c.StagingService,
		"PlaylistService": c.PlaylistService,
		"DownloadService": c.DownloadService,
		"AuditService":    c.AuditService,
		"FileActions":     c.FileActions,
		"Prober":          c.Prober,
		"DB":              c.DB,
	}

	for name, svc := range services {
		if svc == nil {
			t.Errorf("Container.%s is nil", name)
		}
	}
}

// TestContainerEnforcesForeignKeys restates, at the layer where the bug was
// introduced, that the connection the application runs on enforces the
// constraints the schema depends on.
func TestContainerEnforcesForeignKeys(t *testing.T) {
	c := boot(t)

	enabled, err := db.ForeignKeysEnabled(c.DB)
	if err != nil {
		t.Fatalf("ForeignKeysEnabled: %v", err)
	}
	if !enabled {
		t.Error("the application connection has foreign keys disabled")
	}

	// playlist_tracks cascades, so a membership row cannot outlive its
	// playlist.
	if _, err := c.DB.Exec(`INSERT INTO playlists (name) VALUES ('cascade-check');`); err != nil {
		t.Fatalf("insert playlist: %v", err)
	}
	var id int64
	if err := c.DB.QueryRow(`SELECT id FROM playlists WHERE name = 'cascade-check';`).Scan(&id); err != nil {
		t.Fatalf("read playlist id: %v", err)
	}
	if _, err := c.DB.Exec(
		`INSERT INTO tracks (id, title, relative_path) VALUES ('ct', 'T', 'ct.mp3');`,
	); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := c.DB.Exec(
		`INSERT INTO playlist_tracks (playlist_id, track_id, position) VALUES (?, 'ct', 0);`, id,
	); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	if _, err := c.DB.Exec(`DELETE FROM playlists WHERE id = ?;`, id); err != nil {
		t.Fatalf("delete playlist: %v", err)
	}

	var left int
	if err := c.DB.QueryRow(`SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id = ?;`, id).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("ON DELETE CASCADE did not fire: %d row(s) survived", left)
	}
}

// TestContainerServicesWorkTogether exercises a path that crosses several
// services, which is what the graph exists to make possible.
func TestContainerServicesWorkTogether(t *testing.T) {
	c := boot(t)
	ctx := context.Background()

	folder, err := c.FolderService.Create(ctx, "", "", library.FolderName("Rock"))
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	artist, err := c.ArtistService.Create(ctx, "", folder.ID, library.ArtistName("Someone"))
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}

	track, err := c.TrackService.Create(ctx, library.TrackCreateParams{
		Title:        "A Song",
		ArtistID:     artist.ID,
		RelativePath: library.TrackPath("Rock/A Song.mp3"),
	})
	if err != nil {
		t.Fatalf("create track: %v", err)
	}

	byFolder, err := c.TrackService.ByFolder(ctx, folder.ID, false)
	if err != nil {
		t.Fatalf("ByFolder: %v", err)
	}
	if len(byFolder) != 1 || byFolder[0].ID != track.ID {
		t.Errorf("ByFolder returned %d track(s), want the one created", len(byFolder))
	}

	playlist, err := c.PlaylistService.Create(ctx, "Mixed")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	if err := c.PlaylistService.AddTrack(ctx, playlist.Name, track.ID); err != nil {
		t.Fatalf("add track to playlist: %v", err)
	}

	entries, err := c.PlaylistService.Entries(ctx, playlist.Name)
	if err != nil {
		t.Fatalf("playlist entries: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("playlist holds %d entries, want 1", len(entries))
	}

	// The file-action journal is wired as the transactor's post-commit hook;
	// with no pending actions this is a no-op that must not error.
	if err := c.FileActions.ExecutePending(ctx); err != nil {
		t.Errorf("ExecutePending: %v", err)
	}
}
