package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path/filepath"

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

	if err := Snapshot(database); err != nil {
		return 0, err
	}

	if err := RunMigrations(ctx, database); err != nil {
		return 0, fmt.Errorf("run schema migrations: %w", err)
	}

	fmt.Fprintf(w, "Applied %d migration(s): schema %d → %d (snapshot: %s)\n",
		len(pending), current, pending[len(pending)-1].Version, SnapshotName)

	return len(pending), nil
}

// SnapshotName is the pre-migration snapshot written beside the database.
const SnapshotName = "pre-migration.bak.db"

// Snapshot writes a consistent copy of the database next to itself, taken
// atomically by SQLite so it cannot capture a half-written transaction.
// Restoring that copy is the supported way back: rolling the schema back
// with a Down migration discards the rows written since it applied, whereas
// the snapshot predates the change entirely.
func Snapshot(database *sql.DB) error {
	// database_list is (seq, name, file); file is empty for in-memory.
	var seq int
	var name, file string
	if err := database.QueryRow("PRAGMA database_list;").Scan(&seq, &name, &file); err != nil {
		return fmt.Errorf("locate database file: %w", err)
	}
	if file == "" {
		return nil
	}

	snapshot := filepath.Join(filepath.Dir(file), SnapshotName)
	if _, err := database.Exec("VACUUM INTO ?", snapshot); err != nil {
		return fmt.Errorf("snapshot database to %q: %w", snapshot, err)
	}

	return nil
}

func RunMigrations(ctx context.Context, db *sql.DB) error {
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("run schema migrations: %w", err)
	}

	return nil
}

func SetMigration(ctx context.Context, db *sql.DB, current int, target int) error {
	switch {
	case current > target:
		if err := goose.DownToContext(ctx, db, ".", int64(target)); err != nil {
			return fmt.Errorf("roll back schema migrations to %d: %w", target, err)
		}
	case current < target:
		if err := goose.UpToContext(ctx, db, ".", int64(target)); err != nil {
			return fmt.Errorf("apply schema migrations to %d: %w", target, err)
		}
	}

	return nil
}
