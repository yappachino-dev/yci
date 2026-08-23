package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/yappachino-dev/yci/db/migrations"

	"github.com/pressly/goose/v3"
)

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
