package cli

import (
	"fmt"
	"strings"

	"github.com/yappachino-dev/yci/internal/library"

	"github.com/spf13/cobra"
)

const flagForce = "force"

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
		c := Container(ctx)

		id, _ := cmd.Flags().GetString(flagID)
		force, _ := cmd.Flags().GetBool(flagForce)

		id = strings.TrimSpace(id)

		if !force {
			isEmpty, err := c.FolderService.IsEmpty(ctx, library.FolderID(id))
			if err != nil {
				return fmt.Errorf("failed to check folder contents: %w", err)
			}
			if !isEmpty {
				return fmt.Errorf("folder %q is not empty. Use --force to delete it anyway", id)
			}
		}

		if err := c.FolderService.Delete(ctx, library.FolderID(id)); err != nil {
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
		c := Container(cmd.Context())

		id, _ := cmd.Flags().GetString(flagID)
		id = strings.TrimSpace(id)

		if err := c.ArtistService.Delete(cmd.Context(), library.ArtistID(id)); err != nil {
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
		c := Container(cmd.Context())

		artistID, _ := cmd.Flags().GetString(flagArtist)
		channelName, _ := cmd.Flags().GetString(flagName)

		artistID = strings.TrimSpace(artistID)
		channelName = strings.TrimSpace(channelName)

		if err := c.ChannelService.Unlink(cmd.Context(), library.ArtistID(artistID), library.ChannelName(channelName)); err != nil {
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
		c := Container(cmd.Context())

		id, _ := cmd.Flags().GetString(flagID)
		id = strings.TrimSpace(id)

		if err := c.TrackService.Delete(cmd.Context(), library.TrackID(id)); err != nil {
			return err
		}

		fmt.Printf("Deleted track and physical file for ID: %s\n", id)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(deleteCmd)

	deleteCmd.AddCommand(deleteFolderCmd)
	deleteCmd.AddCommand(deleteArtistCmd)
	deleteCmd.AddCommand(deleteChannelCmd)
	deleteCmd.AddCommand(deleteTrackCmd)

	deleteFolderCmd.Flags().StringP(flagID, "i", "", "The ID of the folder to delete (Required)")
	deleteFolderCmd.Flags().Bool(flagForce, false, "Force delete a folder even if it contains artists or sub-folders")
	deleteFolderCmd.MarkFlagRequired(flagID)
	deleteFolderCmd.RegisterFlagCompletionFunc(flagID, folderIDCompletion)

	deleteArtistCmd.Flags().StringP(flagID, "i", "", "The ID of the artist to delete (Required)")
	deleteArtistCmd.MarkFlagRequired(flagID)
	deleteArtistCmd.RegisterFlagCompletionFunc(flagID, artistIDCompletion)

	deleteChannelCmd.Flags().String(flagArtist, "", "The target Artist ID (Required)")
	deleteChannelCmd.Flags().String(flagName, "", "The YouTube channel name to unlink (Required)")
	deleteChannelCmd.MarkFlagRequired(flagArtist)
	deleteChannelCmd.MarkFlagRequired(flagName)
	deleteChannelCmd.RegisterFlagCompletionFunc(flagArtist, artistIDCompletion)
	deleteChannelCmd.RegisterFlagCompletionFunc(flagName, channelNameCompletion)

	deleteTrackCmd.Flags().StringP(flagID, "i", "", "The UUID of the track to delete (Required)")
	deleteTrackCmd.MarkFlagRequired(flagID)
	deleteTrackCmd.RegisterFlagCompletionFunc(flagID, trackIDCompletion)
}
