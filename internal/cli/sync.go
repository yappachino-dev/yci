package cli

import (
	"fmt"

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

		artist, _ := cmd.Flags().GetString(flagArtist)
		folder, _ := cmd.Flags().GetString(flagFolder)
		recursive, _ := cmd.Flags().GetBool(flagRecursive)
		dryRun, _ := cmd.Flags().GetBool(flagDryRun)
		delayFlag, _ := cmd.Flags().GetString(flagDelay)

		var missing []library.Track
		var err error

		switch {
		case artist != "":
			fmt.Printf("Scanning missing files for artist: %s...\n", artist)
			missing, err = appl.AuditService.MissingByArtist(ctx, library.ArtistID(artist))
		case folder != "":
			fmt.Printf("Scanning missing files in folder: %s (Recursive: %t)...\n", folder, recursive)
			missing, err = appl.AuditService.MissingByFolder(ctx, library.FolderID(folder), recursive)
		default:
			fmt.Println("Scanning entire library for missing files...")
			missing, err = appl.AuditService.MissingAll(ctx)
		}

		if err != nil {
			return err
		}

		if len(missing) == 0 {
			fmt.Println("Library is fully synced. No missing files detected.")
			return nil
		}

		fmt.Printf("Audit complete. Found %d missing files.\n\n", len(missing))

		if dryRun {
			fmt.Println("--- DRY RUN: The following files would be enqueued and downloaded ---")
			for _, t := range missing {
				fmt.Printf("- %s (Target: %s)\n", t.Title, t.RelativePath)
			}

			return nil
		}

		ids := make([]string, 0, len(missing))
		for _, t := range missing {
			ids = append(ids, t.SourceID)
		}

		result, err := appl.DownloadService.Enqueue(ctx, download.EnqueueParams{
			URLs:     ids,
			Reporter: &EnqueueReporter{},
		})
		if err != nil {
			return err
		}

		fmt.Printf("Queue complete! Enqueued: %d | Skipped: %d | Failed: %d\n",
			result.Enqueued, result.Skipped, result.Failed)

		fmt.Print("\nProcessing queue...\n")
		return executeDownloadLoop(ctx, delayFlag)
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)

	syncCmd.Flags().String(flagArtist, "", "Target a specific artist ID to sync")
	syncCmd.Flags().String(flagFolder, "", "Target a specific folder ID to sync")
	syncCmd.Flags().Bool(flagRecursive, true, "When syncing a folder, include all sub-folders")
	syncCmd.Flags().Bool(flagDryRun, false, "Print missing files without downloading them")
	syncCmd.Flags().String(flagDelay, defaultDelay, flagDelayComment)
	syncCmd.MarkFlagsMutuallyExclusive(flagArtist, flagFolder)
	syncCmd.RegisterFlagCompletionFunc(flagArtist, artistIDCompletion)
	syncCmd.RegisterFlagCompletionFunc(flagFolder, folderIDCompletion)
}
