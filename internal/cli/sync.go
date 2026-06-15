package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"playlist-manager/internal/downloader"
	"playlist-manager/internal/library"

	"github.com/spf13/cobra"
)

const (
	flagRecursive = "recursive"
	flagDryRun    = "dry-run"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Scan the database and download missing physical files",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if err := downloader.CheckDependencies(); err != nil {
			return err
		}

		artist, _ := cmd.Flags().GetString(flagArtist)
		folder, _ := cmd.Flags().GetString(flagFolder)
		recursive, _ := cmd.Flags().GetBool(flagRecursive)
		dryRun, _ := cmd.Flags().GetBool(flagDryRun)

		delayFlag, _ := cmd.Flags().GetString(flagDelay)
		delayParts := strings.Split(delayFlag, ":")
		if len(delayParts) != 2 {
			return fmt.Errorf("invalid delay format. Expected min:max (e.g., 5:15)")
		}
		minSleep, maxSleep := delayParts[0], delayParts[1]

		var tracks []library.Track
		var err error

		if artist != "" {
			fmt.Printf("Scanning missing files for artist: %s...\n", artist)
			tracks, err = appl.TrackService.ByArtistID(ctx, library.ArtistID(artist))
		} else if folder != "" {
			fmt.Printf("Scanning missing files in folder: %s (Recursive: %t)...\n", folder, recursive)
			tracks, err = appl.TrackService.ByFolder(ctx, library.FolderID(folder), recursive)
		} else {
			fmt.Println("Scanning entire library for missing files...")
			tracks, err = appl.TrackService.All(ctx)
		}

		if err != nil {
			return fmt.Errorf("database query failed: %w", err)
		}

		var missing []library.Track
		for _, t := range tracks {
			if t.SourceID == "" {
				continue
			}

			absPath := filepath.Join(appl.Paths.LibraryDir(), filepath.FromSlash(string(t.RelativePath)))
			if _, err := os.Stat(absPath); os.IsNotExist(err) {
				missing = append(missing, t)
			}
		}

		if len(missing) == 0 {
			fmt.Println("Library is fully synced. No missing files detected.")
			return nil
		}

		fmt.Printf("Audit complete. Found %d missing files.\n\n", len(missing))

		if dryRun {
			fmt.Println("--- DRY RUN: The following files would be downloaded ---")
			for _, t := range missing {
				fmt.Printf("- %s (Target: %s)\n", t.Title, t.RelativePath)
			}
			return nil
		}

		fmt.Println("Starting recovery process...")
		successCount := 0

		for i, t := range missing {
			fmt.Printf("--- [Recovering %d / %d] ---\n", i+1, len(missing))
			fmt.Printf("Track: %s\n", t.Title)

			targetPath := filepath.Join(appl.Paths.LibraryDir(), filepath.FromSlash(string(t.RelativePath)))

			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				fmt.Printf("Failed to create directory for %s: %v\n\n", t.Title, err)
				continue
			}

			cfg := downloader.Config{
				AbsoluteOutputPath: targetPath,
				URL:                t.SourceID,
				MinSleep:           minSleep,
				MaxSleep:           maxSleep,
			}

			if err := downloader.Download(ctx, cfg); err != nil {
				fmt.Printf("Error recovering %s: %v\n\n", t.Title, err)
				continue
			}

			successCount++
			fmt.Println()
		}

		fmt.Printf("Sync complete! Successfully recovered %d/%d files.\n", successCount, len(missing))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)

	syncCmd.Flags().StringP(flagArtist, "a", "", "Target a specific artist ID to sync")
	syncCmd.Flags().StringP(flagFolder, "f", "", "Target a specific folder ID to sync")
	syncCmd.Flags().BoolP(flagRecursive, "r", true, "When syncing a folder, include all sub-folders")
	syncCmd.Flags().Bool(flagDryRun, false, "Print missing files without downloading them")
	syncCmd.Flags().String(flagDelay, "5:30", "Randomized delay between downloads in seconds (format min:max)")
	syncCmd.MarkFlagsMutuallyExclusive(flagArtist, flagFolder)
	syncCmd.RegisterFlagCompletionFunc(flagArtist, artistIDCompletion)
	syncCmd.RegisterFlagCompletionFunc(flagFolder, folderIDCompletion)
}
