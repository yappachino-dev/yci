package cli

import (
	"github.com/yappachino-dev/yci/internal/config"
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

		var missing []library.Track
		var err error

		switch {
		case artist != "":
			cmd.PrintErrf("Scanning missing files for artist: %s...\n", artist)
			missing, err = c.AuditService.MissingByArtist(ctx, library.ArtistID(artist))
		case folder != "":
			cmd.PrintErrf("Scanning missing files in folder: %s (Recursive: %t)...\n", folder, recursive)
			missing, err = c.AuditService.MissingByFolder(ctx, library.FolderID(folder), recursive)
		default:
			cmd.PrintErrln("Scanning entire library for missing files...")
			missing, err = c.AuditService.MissingAll(ctx)
		}

		if err != nil {
			return err
		}

		if len(missing) == 0 {
			cmd.Println("Library is fully synced. No missing files detected.")
			return nil
		}

		cmd.Printf("Audit complete. Found %s.\n", plural(len(missing), "missing file", "missing files"))
		cmd.Println()

		if dryRun {
			cmd.Println("Dry run: the following would be enqueued and downloaded.")
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
			cmd.PrintErrln()
			cmd.PrintErrln("▶ Flag --queue-only set. Run `yci download resume` to process them.")
			return nil
		}

		cmd.PrintErrln()
		cmd.PrintErrln("Processing queue...")
		return executeDownloadLoop(cmd)
	},
}

func init() {

	syncCmd.Flags().String(flagArtist, "", "Target a specific artist ID to sync")
	syncCmd.Flags().String(flagFolder, "", "Target a specific folder ID to sync")
	syncCmd.Flags().Bool(flagRecursive, true, "Include sub-folders when syncing (only applies with --folder)")
	syncCmd.Flags().Bool(flagDryRun, false, "Print missing files without downloading them")
	syncCmd.Flags().Bool(flagQueueOnly, false, "Enqueue missing files without starting the download")
	syncCmd.Flags().String(flagDelay, "", flagDelayComment+" (default: config or "+config.DefaultDelay+")")
	syncCmd.Flags().String(flagCookies, "", "Path to a cookies.txt export for age-restricted content (default: config)")
	syncCmd.MarkFlagsMutuallyExclusive(flagArtist, flagFolder)
	syncCmd.RegisterFlagCompletionFunc(flagArtist, artistIDCompletion)
	syncCmd.RegisterFlagCompletionFunc(flagFolder, folderIDCompletion)
}
