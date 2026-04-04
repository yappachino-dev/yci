// Package cmd provides entry point for cli tool
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"playlist-manager/internal/library"
)

var lib *library.Library

var rootCmd = &cobra.Command{
	Use:   "yt-cli",
	Short: "A CLI to manage your local music library",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error

		lib, err = library.NewLibrary()
		if err != nil {
			return fmt.Errorf("failed to initialize library: %w", err)
		}

		if err := lib.Init(); err != nil {
			return fmt.Errorf("failed to initialize db: %w", err)
		}

		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if lib != nil {
			if err := lib.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to cleanly close database: %v\n", err)
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
