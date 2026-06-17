package cli

import (
	"errors"
	"fmt"
	"strings"

	"playlist-manager/internal/library"

	"github.com/charmbracelet/huh"
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

		ids, err := appl.ArtistService.AllIDs(ctx)
		if err != nil {
			return fmt.Errorf("fetch artist ids: %w", err)
		}

		suggestions := StringSlice(ids)

		handler := func(channelName string, title string) (library.StagingResult, error) {
			switch {
			case skipUnknown, skipCollabArtists && strings.Contains(channelName, ","):
				return library.StagingResult{Action: library.ActionSkip}, nil
			case fallbackArtist != "":
				return library.StagingResult{Action: library.ActionImport, Artist: library.ArtistID(fallbackArtist)}, nil
			}

			var input string

			form := huh.NewForm(huh.NewGroup(
				huh.NewNote().Title(
					fmt.Sprintf("Unknown Channel: %q\nTrack: %q", channelName, title),
				),
				huh.NewInput().
					Description("Enter an existing Artist ID to link (or type 'skip'/'exit')").
					Value(&input).
					Suggestions(append([]string{"skip", "exit"}, suggestions...)),
			)).WithTheme(huh.ThemeBase())

			if err := form.Run(); err != nil {
				return library.StagingResult{}, fmt.Errorf("failed to read terminal input: %w", err)
			}

			input = strings.TrimSpace(input)

			switch input {
			case "exit", "quit":
				return library.StagingResult{Action: library.ActionAbort}, nil
			case "skip", "":
				fmt.Printf("Skipped %q\n", channelName)
				return library.StagingResult{Action: library.ActionSkip}, nil
			default:
				fmt.Printf("Linked %q to %q\n", channelName, input)
				return library.StagingResult{Action: library.ActionImport, Artist: library.ArtistID(input)}, nil
			}
		}

		fmt.Print("Processing staging directory...\n\n")

		if err := appl.StagingService.ProcessStaging(ctx, handler); err != nil {
			if errors.Is(err, library.ErrProcessAborted) {
				fmt.Println("Processing loop aborted by user.")
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

func StringSlice[T ~string](customSlice []T) []string {
	result := make([]string, len(customSlice))
	for i, v := range customSlice {
		result[i] = string(v)
	}
	return result
}
