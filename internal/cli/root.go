// Package cli provides entry point for cli tool
package cli

import (
	"errors"
	"os"
	"strings"

	"github.com/spf13/cobra"
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
	flagDelayComment = "Randomized delay between downloads in seconds (format min:max)"
)

var rootCmd = &cobra.Command{
	Use:   "yci",
	Short: "A CLI to manage your local music library",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		silenceUsageOnRuntimeError(cmd)

		path := cmd.CommandPath()

		// Commands that never touch the library skip the boot sequence
		// entirely; ContainerOK guards their access to the container.
		if path == "yci version" || strings.HasPrefix(path, "yci config") ||
			strings.HasPrefix(path, "yci completion") || path == "yci help" {
			return nil
		}

		autoMigrate := !strings.HasPrefix(path, "yci debug migration")

		c, err := initApplication(cmd.Context(), autoMigrate, cmd.ErrOrStderr())
		if err != nil {
			return err
		}

		cmd.SetContext(WithContainer(cmd.Context(), c))
		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if c, ok := ContainerOK(cmd.Context()); ok {
			if err := c.DB.Close(); err != nil {
				cmd.PrintErrln("Warning: failed to cleanly close database connections:", err)
			}
		}
	},
}

func Execute(v string) {
	rootCmd.Version = v
	// Route cmd.Print* to stdout so data and errors (which cobra sends to
	// stderr) are stream-separated for piping.
	rootCmd.SetOut(os.Stdout)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// silenceUsageOnRuntimeError suppresses cobra's usage dump for errors raised
// once a command is already running. Flag parsing and argument validation
// both fail before this hook, so genuine usage mistakes keep their help text;
// only failures from real work — an unreadable library root, a missing
// yt-dlp — reach the user as a bare message they can act on.
func silenceUsageOnRuntimeError(cmd *cobra.Command) {
	cmd.SilenceUsage = true
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
