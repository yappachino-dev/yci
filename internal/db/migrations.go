package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/yappachino-dev/yci/db/migrations"

	"github.com/pressly/goose/v3"
)

// PendingMigrations lists the embedded migrations the database has not
// applied, oldest first.
func PendingMigrations(database *sql.DB) ([]*goose.Migration, error) {
	migrations, err := goose.CollectMigrations(".", 0, 1<<63-1)
	if err != nil {
		return nil, fmt.Errorf("collect migrations: %w", err)
	}

	current, err := goose.GetDBVersion(database)
	if err != nil {
		return nil, fmt.Errorf("read applied migration version: %w", err)
	}

	var pending []*goose.Migration
	for _, m := range migrations {
		if m.Version > current {
			pending = append(pending, m)
		}
	}

	return pending, nil
}

func GetMaxVersion() (int, error) {
	migrations, err := goose.CollectMigrations(".", 0, 1<<63-1)
	if err != nil {
		return 0, fmt.Errorf("collect migrations: %w", err)
	}

	last, err := migrations.Last()
	if err != nil {
		return 0, fmt.Errorf("find last migration: %w", err)
	}

	return int(last.Version), nil
}

func GetCurrentVersion(ctx context.Context, db *sql.DB) (int, error) {
	const activeVersionQuery = `
		SELECT version_id 
		FROM goose_db_version 
		WHERE is_applied = 1 
		ORDER BY version_id DESC 
		LIMIT 1;
	`

	var current int
	if err := db.QueryRowContext(ctx, activeVersionQuery).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}

		return 0, fmt.Errorf("read current migration version: %w", err)
	}

	return current, nil
}

func SetUpMigrationContext() error {
	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	goose.SetLogger(goose.NopLogger())

	return nil
}

// Migrate brings the database up to the newest embedded migration. It
// reports how many migrations were applied so callers can surface the
// change: a schema edit is too significant to happen invisibly.
//
// A snapshot is taken first when pending migrations exist, because goose
// rolls a failed migration back but not a successful one that turned out to
// be wrong.
// Migrate brings the database up to the newest embedded migration and
// reports what changed on w. A schema edit is too significant to happen
// silently, and goose rolls a failed migration back but not a successful one
// that turned out to be wrong, so a snapshot is taken first.
func Migrate(ctx context.Context, database *sql.DB, w io.Writer) (int, error) {
	pending, err := PendingMigrations(database)
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	current, err := GetCurrentVersion(ctx, database)
	if err != nil {
		return 0, err
	}

	snapshotPath, err := Snapshot(database, int64(current))
	if err != nil {
		return 0, err
	}

	if err := RunMigrations(ctx, database); err != nil {
		return 0, fmt.Errorf("run schema migrations: %w", err)
	}

	// Report the file that was actually written, not the constant: a second
	// run at the same schema version lands on a different name.
	fmt.Fprintf(w, "Applied %d migration(s): schema %d → %d (snapshot: %s)\n",
		len(pending), current, pending[len(pending)-1].Version, filepath.Base(snapshotPath))

	return len(pending), nil
}

// SnapshotName is the pre-migration snapshot written beside the database.
const SnapshotName = "pre-migration.bak.db"

// Snapshot writes a consistent copy of the database next to itself, taken
// atomically by SQLite so it cannot capture a half-written transaction.
// Restoring that copy is the supported way back: rolling the schema back
// with a Down migration discards the rows written since it applied, whereas
// the snapshot predates the change entirely.
//
// It returns the path written. The file is written under a temporary name
// and renamed into place, so an interrupted snapshot leaves any previous one
// intact rather than truncated.
//
// current is the schema version about to be migrated from; it is only used to
// name a second snapshot distinctly.
func Snapshot(database *sql.DB, current int64) (string, error) {
	// database_list is (seq, name, file); file is empty for in-memory.
	var seq int
	var name, file string
	if err := database.QueryRow("PRAGMA database_list;").Scan(&seq, &name, &file); err != nil {
		return "", fmt.Errorf("locate database file: %w", err)
	}
	if file == "" {
		return "", nil
	}

	dir := filepath.Dir(file)
	target, err := freeSnapshotPath(dir, current)
	if err != nil {
		return "", err
	}

	// VACUUM INTO refuses to write onto an existing file. Renaming the
	// finished temporary over the target is how the replacement happens
	// atomically, and it is also what keeps a crash from leaving a
	// truncated backup where a good one used to be.
	tmp, err := os.CreateTemp(dir, SnapshotName+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create temporary snapshot file: %w", err)
	}
	tmpName := tmp.Name()
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("close temporary snapshot file: %w", err)
	}
	// VACUUM INTO needs the path not to exist.
	if err := os.Remove(tmpName); err != nil {
		return "", fmt.Errorf("clear temporary snapshot path: %w", err)
	}

	if _, err := database.Exec("VACUUM INTO ?", tmpName); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("snapshot database: %w", err)
	}

	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("install snapshot at %q: %w", target, err)
	}

	return target, nil
}

// freeSnapshotPath picks a name that does not exist yet.
//
// A snapshot must never be the reason a migration cannot run. An earlier
// implementation used a fixed name and treated "output file already exists" as
// fatal, which meant: migrate once, let anything interrupt it, and every
// subsequent run was blocked before it reached the migrations — a recovery
// mechanism that could itself become the outage. An existing snapshot is kept
// rather than overwritten, because the one from a previous run is the only
// record of the state before the migrations that have already landed.
func freeSnapshotPath(dir string, current int64) (string, error) {
	// The versioned name is offered even for 0: "pre-migration-v0" is a
	// meaningful label (nothing had been applied yet) and keeps a fresh
	// database from having no free name.
	candidates := []string{SnapshotName, fmt.Sprintf("pre-migration-v%d.bak.db", current)}

	for _, candidate := range candidates {
		path := filepath.Join(dir, candidate)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", fmt.Errorf("check snapshot path %q: %w", path, err)
		}
	}

	// Every deterministic name is taken, which means several runs have
	// happened at the same schema version. A timestamp keeps each one.
	stamped := fmt.Sprintf("pre-migration-v%d-%d.bak.db", current, time.Now().Unix())
	path := filepath.Join(dir, stamped)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("snapshot %q already exists and no free name remains", path)
	}

	return path, nil
}

func RunMigrations(ctx context.Context, db *sql.DB) error {
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("run schema migrations: %w", err)
	}

	return nil
}
