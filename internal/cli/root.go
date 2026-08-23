// Package cli provides entry point for cli tool
package cli

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/fs"

	_ "modernc.org/sqlite"
)

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

var rootCmd = &cobra.Command{
	Use:   "yci",
	Short: "A CLI to manage your local music library",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		autoMigrate := !strings.HasPrefix(cmd.CommandPath(), "yci debug migration")

		c, err := initApplication(cmd.Context(), autoMigrate)
		if err != nil {
			return err
		}

		cmd.SetContext(WithContainer(cmd.Context(), c))
		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if c, ok := ContainerOK(cmd.Context()); ok {
			if err := c.DB.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to cleanly close database connections: %v\n", err)
			}
		}
	},
}

func Execute(v string) {
	rootCmd.Version = v

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

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

// mapSentinels turns known domain sentinels into one clean user-facing
// sentence; unexpected errors keep their full wrap chain.
func mapSentinels(err error, messages map[error]string) error {
	for sentinel, msg := range messages {
		if errors.Is(err, sentinel) {
			return errors.New(msg)
		}
	}

	return err
}

// ContainerOK is the non-panicking variant for paths that run without the
// boot sequence, such as shell completion.
func ContainerOK(ctx context.Context) (*app.Container, bool) {
	c, ok := ctx.Value(containerKey{}).(*app.Container)
	return c, ok
}

func initApplication(ctx context.Context, autoMigrate bool) (*app.Container, error) {
	paths, err := fs.GetPaths()
	if err != nil {
		return nil, fmt.Errorf("load application paths: %w", err)
	}

	if err = os.MkdirAll(paths.ConfigDir(), 0o755); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}

	dbConn, err := sql.Open("sqlite", filepath.Join(paths.ConfigDir(), "data.db"))
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

	c, err := app.NewContainer(dbConn, paths)
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

func artistIDCompletion(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c, ok := ContainerOK(cmd.Context())
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	artists, err := c.ArtistService.All(cmd.Context())
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
	c, ok := ContainerOK(cmd.Context())
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	folders, err := c.FolderService.All(cmd.Context())
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
	c, ok := ContainerOK(cmd.Context())
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	tracks, err := c.TrackService.All(cmd.Context())
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
	c, ok := ContainerOK(cmd.Context())
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	channels, err := c.ChannelService.All(cmd.Context())
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
