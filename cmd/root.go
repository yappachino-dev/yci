// Package cmd provides entry point for cli tool
package cmd

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"playlist-manager/internal/app"
	"playlist-manager/internal/db"
	"playlist-manager/internal/library"

	_ "modernc.org/sqlite"
)

var appl *app.Container

var rootCmd = &cobra.Command{
	Use:   "yci",
	Short: "A CLI to manage your local music library",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		paths, err := library.GetPaths()
		if err != nil {
			return fmt.Errorf("load application paths: %w", err)
		}

		if err = os.MkdirAll(paths.Config, 0o755); err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}

		dbConn, err := sql.Open("sqlite", filepath.Join(paths.Config, "data.db"))
		if err != nil {
			return fmt.Errorf("open sqlite database: %w", err)
		}

		if _, err = dbConn.Exec("PRAGMA foreign_keys = ON;"); err != nil {
			dbConn.Close()
			return fmt.Errorf("enable foreign key constraints: %w", err)
		}

		if err = db.RunMigrations(ctx, dbConn); err != nil {
			dbConn.Close()
			return fmt.Errorf("execute database migration: %w", err)
		}

		appl, err = app.NewContainer(ctx, dbConn, paths)
		if err != nil {
			dbConn.Close()
			return fmt.Errorf("initialize application container statements: %w", err)
		}

		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if appl != nil && appl.DB != nil {
			if err := appl.DB.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to cleanly close database connections: %v\n", err)
			}
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
