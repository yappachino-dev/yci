package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yappachino-dev/yci/internal/library"

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
		c := Container(ctx)

		skipUnknown, _ := cmd.Flags().GetBool(flagSkipUnknown)
		skipCollabArtists, _ := cmd.Flags().GetBool(flagSkipCollabArtists)
		fallbackArtist, _ := cmd.Flags().GetString(flagFallbackArtist)

		if !stdinIsTTY() && fallbackArtist == "" && !skipUnknown && !skipCollabArtists {
			return fmt.Errorf("non-interactive session: unknown channels cannot prompt for a decision; use --fallback-artist, --%s, or --%s",
				flagSkipUnknown, flagSkipCollabArtists)
		}

		if fallbackArtist != "" {
			if _, err := c.ArtistService.ByID(ctx, library.ArtistID(fallbackArtist)); err != nil {
				return fmt.Errorf("invalid fallback artist %q: %w", fallbackArtist, err)
			}
		}

		artistIds, err := c.ArtistService.AllIDs(ctx)
		if err != nil {
			return fmt.Errorf("fetch artist ids: %w", err)
		}
		artistSuggestions := StringSlice(artistIds)

		folderIds, err := c.FolderService.AllIDs(ctx)
		if err != nil {
			return fmt.Errorf("fetch folder ids: %w", err)
		}
		folderSuggestions := StringSlice(folderIds)

		handler := func(channelName string, title string) (library.StagingResult, error) {
			switch {
			case skipUnknown, skipCollabArtists && strings.Contains(channelName, ","):
				return library.StagingResult{Action: library.ActionSkip}, nil
			case fallbackArtist != "":
				return library.StagingResult{Action: library.ActionImport, Artist: library.ArtistID(fallbackArtist)}, nil
			}

			var actionType string

			choiceForm := huh.NewForm(huh.NewGroup(
				huh.NewNote().Title(
					fmt.Sprintf("Unknown Channel: %q\nTrack: %q", channelName, title),
				),
				huh.NewSelect[string]().
					Title("How would you like to handle this track?").
					Options(
						huh.NewOption("Link Channel to an Artist (Permanent)", "artist-link"),
						huh.NewOption("Link this track to an Artist (One-off track)", "artist"),
						huh.NewOption("Route straight to a Folder (One-off track)", "folder"),
						huh.NewOption("Skip this track", "skip"),
						huh.NewOption("Abort processing loop", "abort"),
					).
					Value(&actionType),
			)).WithTheme(huh.ThemeBase())

			if err := choiceForm.Run(); err != nil {
				return library.StagingResult{}, fmt.Errorf("failed to read menu choice: %w", err)
			}

			switch actionType {
			case "skip":
				cmd.Printf("Skipped track from %q\n", channelName)
				return library.StagingResult{Action: library.ActionSkip}, nil
			case "abort":
				return library.StagingResult{Action: library.ActionAbort}, nil
			case "artist-link", "artist":
				description := "Enter an existing Artist ID to bind this track as a one-off"
				if actionType == "artist-link" {
					description = "Enter an existing Artist ID to permanently bind this channel"
				}

				var artistInput string
				artistForm := huh.NewForm(huh.NewGroup(
					huh.NewInput().
						Title("Target Artist ID").
						Description(description).
						Value(&artistInput).
						Suggestions(artistSuggestions).
						Validate(func(str string) error {
							if strings.TrimSpace(str) == "" {
								return errors.New("artist ID cannot be empty")
							}

							return nil
						}),
				)).WithTheme(huh.ThemeBase())

				if err := artistForm.Run(); err != nil {
					return library.StagingResult{}, fmt.Errorf("failed to read artist input: %w", err)
				}

				artistInput = strings.TrimSpace(artistInput)

				if actionType == "artist-link" {
					cmd.Printf("Linked Channel %q permanently to Artist %q\n", channelName, artistInput)
				} else {
					cmd.Printf("Linked track to Artist %q\n", artistInput)
				}

				return library.StagingResult{
					Action:     library.ActionImport,
					Artist:     library.ArtistID(artistInput),
					LinkArtist: actionType == "artist-link",
				}, nil
			case "folder":
				var folderInput string
				folderForm := huh.NewForm(huh.NewGroup(
					huh.NewInput().
						Title("Target Folder ID").
						Description("Enter a Folder ID (The channel will NOT be saved to the database)").
						Value(&folderInput).
						Suggestions(folderSuggestions).
						Validate(func(str string) error {
							if strings.TrimSpace(str) == "" {
								return errors.New("folder ID cannot be empty")
							}

							return nil
						}),
				)).WithTheme(huh.ThemeBase())

				if err := folderForm.Run(); err != nil {
					return library.StagingResult{}, fmt.Errorf("failed to read folder input: %w", err)
				}

				folderInput = strings.TrimSpace(folderInput)
				cmd.Printf("Routing track directly into folder %q\n", folderInput)

				return library.StagingResult{
					Action: library.ActionImport,
					Folder: library.FolderID(folderInput),
				}, nil
			}

			return library.StagingResult{Action: library.ActionSkip}, nil
		}

		cmd.Print("Processing staging directory...\n\n")

		if err := c.StagingService.ProcessStaging(ctx, handler); err != nil {
			if errors.Is(err, library.ErrProcessAborted) {
				cmd.Println("Processing loop aborted by user.")
				return nil
			}

			return err
		}

		cmd.Println("\nSuccess! Staging queue processed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(importCmd)

	importCmd.AddCommand(importStagingCmd)

	importStagingCmd.Flags().Bool(flagSkipUnknown, false, "Automatically skip tracks with unknown channels")
	importStagingCmd.Flags().Bool(flagSkipCollabArtists, false, "Automatically skip tracks with multiple channels (collabs) as artist")
	importStagingCmd.Flags().String(flagFallbackArtist, "", "Automatically link unknown channels to this Artist ID")
}

func StringSlice[T ~string](customSlice []T) []string {
	result := make([]string, len(customSlice))
	for i, v := range customSlice {
		result[i] = string(v)
	}
	return result
}
