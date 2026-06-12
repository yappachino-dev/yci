package db

import (
	"context"
	"database/sql"
	"fmt"

	"playlist-manager/db/migrations"

	"github.com/pressly/goose/v3"
)

func RunMigrations(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	goose.SetLogger(goose.NopLogger())

	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("run schema migrations: %w", err)
	}

	return nil
}
