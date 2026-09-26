package cli

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/config"
	"github.com/yappachino-dev/yci/internal/db"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type containerKey struct{}

// WithContainer attaches the initialized application container to the
// command context; Container retrieves it.
func WithContainer(ctx context.Context, c *app.Container) context.Context {
	return context.WithValue(ctx, containerKey{}, c)
}

// Container returns the application container. Only valid on paths where
// PersistentPreRunE has already run.
func Container(ctx context.Context) *app.Container {
	c, ok := ContainerOK(ctx)
	if !ok {
		panic("container not initialized; PersistentPreRunE must run first")
	}

	return c
}

// ContainerOK is the non-panicking variant for paths that run without the
// boot sequence, such as shell completion.
func ContainerOK(ctx context.Context) (*app.Container, bool) {
	c, ok := ctx.Value(containerKey{}).(*app.Container)
	return c, ok
}

func initApplication(ctx context.Context, autoMigrate bool) (*app.Container, error) {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}

	configDir := cfg.ConfigDir()

	if err = os.MkdirAll(configDir, 0o755); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}

	dbConn, err := sql.Open("sqlite", filepath.Join(configDir, "data.db"))
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if _, err = dbConn.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		dbConn.Close()
		return nil, fmt.Errorf("enable foreign key constraints: %w", err)
	}

	if err = db.SetUpMigrationContext(); err != nil {
		dbConn.Close()
		return nil, fmt.Errorf("setup database migrations: %w", err)
	}

	if autoMigrate {
		if err = db.RunMigrations(ctx, dbConn); err != nil {
			dbConn.Close()
			return nil, fmt.Errorf("execute database migration: %w", err)
		}
	}

	c, err := app.NewContainer(dbConn, cfg)
	if err != nil {
		dbConn.Close()
		return nil, fmt.Errorf("initialize application container statements: %w", err)
	}

	if autoMigrate {
		// Replay journals left pending by a previous crashed run.
		if err = c.FileActions.ExecutePending(ctx); err != nil {
			dbConn.Close()
			return nil, fmt.Errorf("recover pending file actions: %w", err)
		}
	}

	return c, nil
}
