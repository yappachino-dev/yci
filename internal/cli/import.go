package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"playlist-manager/internal/library"

	"github.com/spf13/cobra"
)

const (
	flagSkipUnknown       = "skip-unknown"
	flagSkipCollabArtists = "skip-collab-artists"
	flagFallbackArtist    = "fallback-artist"
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Process and import downloaded files",
}

var importStagingCmd = &cobra.Command{
	Use:   "staging",
	Short: "Process the staging directory and import .opus files",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		skipUnknown, _ := cmd.Flags().GetBool(flagSkipUnknown)
		skipCollabArtists, _ := cmd.Flags().GetBool(flagSkipCollabArtists)
		fallbackArtist, _ := cmd.Flags().GetString(flagFallbackArtist)

		if fallbackArtist != "" {
			if _, err := appl.ArtistService.ByID(ctx, library.ArtistID(fallbackArtist)); err != nil {
				return fmt.Errorf("invalid fallback artist %q: %w", fallbackArtist, err)
			}
		}

		handler := func(channelName string, title string) (library.StagingResult, error) {
			if skipUnknown {
				return library.StagingResult{Action: library.ActionSkip}, nil
			}
			if skipCollabArtists && strings.Contains(channelName, ",") {
				return library.StagingResult{Action: library.ActionSkip}, nil
			}
			if fallbackArtist != "" {
				return library.StagingResult{Action: library.ActionImport, Artist: library.ArtistID(fallbackArtist)}, nil
			}

			reader := bufio.NewReader(os.Stdin)

			fmt.Printf("\n[?] Unknown Channel: %q\n", channelName)
			fmt.Printf("    Track: %q\n", title)
			fmt.Print("    Enter an existing Artist ID to link (or type 'skip'/'exit'): ")

			input, err := reader.ReadString('\n')
			if err != nil {
				return library.StagingResult{}, fmt.Errorf("failed to read terminal input: %w", err)
			}

			input = strings.TrimSpace(input)

			if input == "exit" || input == "quit" {
				return library.StagingResult{Action: library.ActionAbort}, nil
			}

			if input == "skip" || input == "" {
				return library.StagingResult{Action: library.ActionSkip}, nil
			}

			return library.StagingResult{Action: library.ActionImport, Artist: library.ArtistID(input)}, nil
		}

		fmt.Println("Processing staging directory...")

		if err := appl.StagingService.ProcessStaging(ctx, handler); err != nil {
			if errors.Is(err, library.ErrProcessAborted) {
				fmt.Println("\nProcessing loop aborted by user.")
				return nil
			}

			return err
		}

		fmt.Println("\nSuccess! Staging queue processed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(importCmd)

	importCmd.AddCommand(importStagingCmd)

	importStagingCmd.Flags().Bool(flagSkipUnknown, false, "Automatically skip tracks with unknown channels")
	importStagingCmd.Flags().Bool(flagSkipCollabArtists, false, "Automatically skip tracks multiple channels as artist")
	importStagingCmd.Flags().String(flagFallbackArtist, "", "Automatically link unknown channels to this Artist ID")
}
