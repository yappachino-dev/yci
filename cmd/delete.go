package cmd

import (
	"fmt"
	"strings"

	"playlist-manager/internal/library"

	"github.com/spf13/cobra"
)

// --- Command Definitions ---

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a resource from the library",
	Args:  cobra.NoArgs,
}

var deleteFolderCmd = &cobra.Command{
	Use:   "folder",
	Short: "Delete a folder",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		id, _ := cmd.Flags().GetString("id")
		force, _ := cmd.Flags().GetBool("force")

		id = strings.TrimSpace(id)

		if !force {
			isEmpty, err := appl.FolderService.IsEmpty(ctx, library.FolderID(id))
			if err != nil {
				return fmt.Errorf("failed to check folder contents: %w", err)
			}
			if !isEmpty {
				return fmt.Errorf("folder %q is not empty. Use --force to delete it anyway", id)
			}
		}

		if err := appl.FolderService.Delete(ctx, library.FolderID(id)); err != nil {
			return err
		}

		fmt.Printf("Deleted folder: %s\n", id)
		return nil
	},
}

var deleteArtistCmd = &cobra.Command{
	Use:   "artist",
	Short: "Delete an artist (WARNING: channels cascade, tracks become orphaned)",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		id = strings.TrimSpace(id)

		if err := appl.ArtistService.Delete(cmd.Context(), library.ArtistID(id)); err != nil {
			return err
		}

		fmt.Printf("Deleted artist: %s\n", id)
		return nil
	},
}

var deleteChannelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Unlink a YouTube channel from an artist",
	RunE: func(cmd *cobra.Command, args []string) error {
		artistID, _ := cmd.Flags().GetString("artist")
		channelName, _ := cmd.Flags().GetString("name")

		artistID = strings.TrimSpace(artistID)
		channelName = strings.TrimSpace(channelName)

		if err := appl.ChannelService.Unlink(cmd.Context(), library.ArtistID(artistID), library.ChannelName(channelName)); err != nil {
			return err
		}

		fmt.Printf("Unlinked channel %q from artist %q\n", channelName, artistID)
		return nil
	},
}

var deleteTrackCmd = &cobra.Command{
	Use:   "track",
	Short: "Delete a track from the database AND remove the physical file",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		id = strings.TrimSpace(id)

		if err := appl.TrackService.Delete(cmd.Context(), library.TrackID(id)); err != nil {
			return err
		}

		fmt.Printf("Deleted track and physical file for ID: %s\n", id)
		return nil
	},
}

// --- Initialization ---

func init() {
	rootCmd.AddCommand(deleteCmd)

	deleteCmd.AddCommand(deleteFolderCmd)
	deleteCmd.AddCommand(deleteArtistCmd)
	deleteCmd.AddCommand(deleteChannelCmd)
	deleteCmd.AddCommand(deleteTrackCmd)

	deleteFolderCmd.Flags().StringP("id", "i", "", "The ID of the folder to delete (Required)")
	deleteFolderCmd.Flags().BoolP("force", "f", false, "Force delete a folder even if it contains artists or sub-folders")
	deleteFolderCmd.MarkFlagRequired("id")

	deleteArtistCmd.Flags().StringP("id", "i", "", "The ID of the artist to delete (Required)")
	deleteArtistCmd.MarkFlagRequired("id")

	deleteChannelCmd.Flags().StringP("artist", "a", "", "The target Artist ID (Required)")
	deleteChannelCmd.Flags().StringP("name", "n", "", "The YouTube channel name to unlink (Required)")
	deleteChannelCmd.MarkFlagRequired("artist")
	deleteChannelCmd.MarkFlagRequired("name")

	deleteTrackCmd.Flags().StringP("id", "i", "", "The UUID of the track to delete (Required)")
	deleteTrackCmd.MarkFlagRequired("id")

	// --- Dynamic Autocompletions ---

	deleteFolderCmd.RegisterFlagCompletionFunc("id", folderIDCompletion)

	deleteArtistCmd.RegisterFlagCompletionFunc("id", artistIDCompletion)

	deleteChannelCmd.RegisterFlagCompletionFunc("artist", artistIDCompletion)
	deleteChannelCmd.RegisterFlagCompletionFunc("name", channelNameCompletion)

	deleteTrackCmd.RegisterFlagCompletionFunc("id", trackIDCompletion)
}
