package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"playlist-manager/internal/downloader"
	"playlist-manager/internal/library"
	"playlist-manager/internal/utils"

	"github.com/spf13/cobra"
)

// --- Command Definitions ---

var syncCmd = &cobra.Command{
	Use:     "sync",
	Aliases: []string{"repair", "recover"},
	Short:   "Scan the database and download missing physical files",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := utils.CheckDependencies(); err != nil {
			return err
		}

		artist, _ := cmd.Flags().GetString("artist")
		folder, _ := cmd.Flags().GetString("folder")
		recursive, _ := cmd.Flags().GetBool("recursive")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		delayFlag, _ := cmd.Flags().GetString("delay")
		delayParts := strings.Split(delayFlag, ":")
		if len(delayParts) != 2 {
			return fmt.Errorf("invalid delay format. Expected min:max (e.g., 5:15)")
		}
		minSleep, maxSleep := delayParts[0], delayParts[1]

		var tracks []library.Track
		var err error

		if artist != "" {
			fmt.Printf(" Scanning missing files for artist: %s...\n", artist)
			tracks, err = lib.GetTracksByArtist(artist)
		} else if folder != "" {
			fmt.Printf(" Scanning missing files in folder: %s (Recursive: %t)...\n", folder, recursive)
			tracks, err = lib.GetTracksByFolder(folder, recursive)
		} else {
			fmt.Println(" Scanning entire library for missing files...")
			tracks, err = lib.GetAllTracks()
		}

		if err != nil {
			return fmt.Errorf("database query failed: %w", err)
		}

		var missing []library.Track
		for _, t := range tracks {
			if t.SourceID == "" {
				continue
			}

			absPath := filepath.Join(lib.Paths.Library, filepath.FromSlash(t.RelativePath))
			if _, err := os.Stat(absPath); os.IsNotExist(err) {
				missing = append(missing, t)
			}
		}

		if len(missing) == 0 {
			fmt.Println(" Library is fully synced. No missing files detected.")
			return nil
		}

		fmt.Printf(" Audit complete. Found %d missing files.\n\n", len(missing))

		if dryRun {
			fmt.Println("--- DRY RUN: The following files would be downloaded ---")
			for _, t := range missing {
				fmt.Printf("- %s (Target: %s)\n", t.Title, t.RelativePath)
			}
			return nil
		}

		fmt.Println(" Starting recovery process...")
		successCount := 0

		for i, t := range missing {
			fmt.Printf("--- [Recovering %d / %d] ---\n", i+1, len(missing))
			fmt.Printf("Track: %s\n", t.Title)

			targetPath := filepath.Join(lib.Paths.Library, filepath.FromSlash(t.RelativePath))

			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				fmt.Printf(" Failed to create directory for %s: %v\n\n", t.Title, err)
				continue
			}

			if err := downloader.Download(targetPath, t.SourceID, minSleep, maxSleep); err != nil {
				fmt.Printf(" Error recovering %s: %v\n\n", t.Title, err)
				continue
			}

			successCount++
			fmt.Println()
		}

		fmt.Printf(" Sync complete! Successfully recovered %d/%d files.\n", successCount, len(missing))
		return nil
	},
}

// --- Initialization ---

func init() {
	rootCmd.AddCommand(syncCmd)

	syncCmd.Flags().StringP("artist", "a", "", "Target a specific artist ID to sync")
	syncCmd.Flags().StringP("folder", "f", "", "Target a specific folder ID to sync")
	syncCmd.Flags().BoolP("recursive", "r", true, "When syncing a folder, include all sub-folders")
	syncCmd.Flags().Bool("dry-run", false, "Print missing files without downloading them")

	syncCmd.Flags().StringP("delay", "d", "5:30", "Randomized delay between downloads in seconds (format min:max)")

	syncCmd.MarkFlagsMutuallyExclusive("artist", "folder")

	// --- Dynamic Autocompletions ---

	syncCmd.RegisterFlagCompletionFunc("artist", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		artists, err := lib.GetArtists()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		var comps []string
		for _, a := range artists {
			if strings.HasPrefix(a.ID, toComplete) {
				comps = append(comps, fmt.Sprintf("%s\t%s", a.ID, a.Artist))
			}
		}

		return comps, cobra.ShellCompDirectiveNoFileComp
	})

	syncCmd.RegisterFlagCompletionFunc("folder", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		folders, err := lib.GetFolders()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		var comps []string
		for _, f := range folders {
			if strings.HasPrefix(f.ID, toComplete) {
				comps = append(comps, fmt.Sprintf("%s\t%s", f.ID, f.DisplayName))
			}
		}

		return comps, cobra.ShellCompDirectiveNoFileComp
	})
}
