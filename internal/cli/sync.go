package cli

import (
	"github.com/yappachino-dev/yci/internal/download"
	"github.com/yappachino-dev/yci/internal/library"

	"github.com/spf13/cobra"
)

const (
	flagRecursive = "recursive"
	flagDryRun    = "dry-run"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Scan the database and download missing physical files",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return download.CheckDependencies()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		artist, _ := cmd.Flags().GetString(flagArtist)
		folder, _ := cmd.Flags().GetString(flagFolder)
		recursive, _ := cmd.Flags().GetBool(flagRecursive)
		dryRun, _ := cmd.Flags().GetBool(flagDryRun)
		queueOnly, _ := cmd.Flags().GetBool(flagQueueOnly)
		delayFlag, _ := cmd.Flags().GetString(flagDelay)

		var missing []library.Track
		var err error

		switch {
		case artist != "":
			cmd.Printf("Scanning missing files for artist: %s...\n", artist)
			missing, err = c.AuditService.MissingByArtist(ctx, library.ArtistID(artist))
		case folder != "":
			cmd.Printf("Scanning missing files in folder: %s (Recursive: %t)...\n", folder, recursive)
			missing, err = c.AuditService.MissingByFolder(ctx, library.FolderID(folder), recursive)
		default:
			cmd.Println("Scanning entire library for missing files...")
			missing, err = c.AuditService.MissingAll(ctx)
		}

		if err != nil {
			return err
		}

		if len(missing) == 0 {
			cmd.Println("Library is fully synced. No missing files detected.")
			return nil
		}

		cmd.Printf("Audit complete. Found %d missing files.\n\n", len(missing))

		if dryRun {
			cmd.Println("--- DRY RUN: The following files would be enqueued and downloaded ---")
			for _, t := range missing {
				cmd.Printf("- %s (Target: %s)\n", t.Title, t.RelativePath)
			}

			return nil
		}

		ids := make([]string, 0, len(missing))
		for _, t := range missing {
			ids = append(ids, t.SourceID)
		}

		result, err := c.DownloadService.Enqueue(ctx, download.EnqueueParams{
			URLs:     ids,
			Reporter: newConsoleReporter(cmd),
		})
		if err != nil {
			return err
		}

		cmd.Printf("Queue complete! Enqueued: %d | Skipped: %d | Failed: %d\n",
			result.Enqueued, result.Skipped, result.Failed)

		if queueOnly {
			cmd.Println("\n▶ Flag --queue-only set. Run `yci download resume` to process them.")
			return nil
		}

		cmd.Print("\nProcessing queue...\n")
		return executeDownloadLoop(cmd, delayFlag)
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)

	syncCmd.Flags().String(flagArtist, "", "Target a specific artist ID to sync")
	syncCmd.Flags().String(flagFolder, "", "Target a specific folder ID to sync")
	syncCmd.Flags().Bool(flagRecursive, true, "Include sub-folders when syncing (only applies with --folder)")
	syncCmd.Flags().Bool(flagDryRun, false, "Print missing files without downloading them")
	syncCmd.Flags().Bool(flagQueueOnly, false, "Enqueue missing files without starting the download")
	syncCmd.Flags().String(flagDelay, defaultDelay, flagDelayComment)
	syncCmd.MarkFlagsMutuallyExclusive(flagArtist, flagFolder)
	syncCmd.RegisterFlagCompletionFunc(flagArtist, artistIDCompletion)
	syncCmd.RegisterFlagCompletionFunc(flagFolder, folderIDCompletion)
}
