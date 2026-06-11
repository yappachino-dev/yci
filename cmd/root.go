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
	"playlist-manager/internal/library"
)

// var lib *library.Library

var appl *app.Container

var rootCmd = &cobra.Command{
	Use:   "yci",
	Short: "A CLI to manage your local music library",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		paths, err := library.GetPaths()
		if err != nil {
			return fmt.Errorf("failed to retrieve system configurations: %w", err)
		}

		if err := os.MkdirAll(paths.Config, 0o755); err != nil {
			return fmt.Errorf("failed creating application configuration directory: %w", err)
		}

		dbConn, err := sql.Open("sqlite", filepath.Join(paths.Config, "data.db"))
		if err != nil {
			return fmt.Errorf("failed opening SQLite storage engine: %w", err)
		}

		if _, err := dbConn.Exec("PRAGMA foreign_keys = ON;"); err != nil {
			dbConn.Close()
			return fmt.Errorf("failed enabling foreign key constraints: %w", err)
		}

		appl = app.NewContainer(dbConn, paths)

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
