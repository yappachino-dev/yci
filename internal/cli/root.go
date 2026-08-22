// Package cli provides entry point for cli tool
package cli

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/fs"

	_ "modernc.org/sqlite"
)

const version = "1.0.0"

const (
	flagID     = "id"
	flagName   = "name"
	flagParent = "parent"
	flagArtist = "artist"
	flagFolder = "folder"
	flagDelay  = "delay"
)

const (
	defaultDelay = "5:30"
)

const (
	flagDelayComment = "Randomized delay between downloads in seconds (format min:max)"
)

var huhTheme = huh.ThemeBase()

var appl *app.Container

var rootCmd = &cobra.Command{
	Use:     "yci",
	Short:   "A CLI to manage your local music library",
	Version: version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		autoMigrate := !strings.HasPrefix(cmd.CommandPath(), "yci debug migration")
		return initApplication(cmd.Context(), autoMigrate)
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
		os.Exit(1)
	}
}

func initApplication(ctx context.Context, autoMigrate bool) error {
	paths, err := fs.GetPaths()
	if err != nil {
		return fmt.Errorf("load application paths: %w", err)
	}

	if err = os.MkdirAll(paths.ConfigDir(), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	dbConn, err := sql.Open("sqlite", filepath.Join(paths.ConfigDir(), "data.db"))
	if err != nil {
		return fmt.Errorf("open sqlite database: %w", err)
	}

	if _, err = dbConn.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		dbConn.Close()
		return fmt.Errorf("enable foreign key constraints: %w", err)
	}

	if err = db.SetUpMigrationContext(); err != nil {
		return fmt.Errorf("setup database migrations: %w", err)
	}

	if autoMigrate {
		if err = db.RunMigrations(ctx, dbConn); err != nil {
			dbConn.Close()
			return fmt.Errorf("execute database migration: %w", err)
		}
	}

	appl, err = app.NewContainer(ctx, dbConn, paths)
	if err != nil {
		dbConn.Close()
		return fmt.Errorf("initialize application container statements: %w", err)
	}

	if autoMigrate {
		// Replay filesystem operations journaled by a previous crashed run so
		// the physical library converges on the committed metadata state.
		if err = appl.FileActions.ExecutePending(ctx); err != nil {
			dbConn.Close()
			return fmt.Errorf("recover pending file actions: %w", err)
		}
	}

	return nil
}

func artistIDCompletion(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	artists, err := appl.ArtistService.All(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, a := range artists {
		if strings.HasPrefix(string(a.ID), toComplete) {
			comps = append(comps, fmt.Sprintf("%s\t%s", a.ID, a.Name))
		}
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}

func folderIDCompletion(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	folders, err := appl.FolderService.All(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, f := range folders {
		if strings.HasPrefix(string(f.ID), toComplete) {
			comps = append(comps, fmt.Sprintf("%s\t%s", f.ID, f.Name))
		}
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}

func trackIDCompletion(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	tracks, err := appl.TrackService.All(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, t := range tracks {
		if strings.HasPrefix(string(t.ID), toComplete) {
			comps = append(comps, fmt.Sprintf("%s\t%s", t.ID, t.Title))
		}
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}

func channelNameCompletion(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	channels, err := appl.ChannelService.All(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, c := range channels {
		if strings.HasPrefix(string(c.Name), toComplete) {
			comps = append(comps, fmt.Sprintf("%s\tLinked to: %s", c.Name, c.ArtistID))
		}
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}
