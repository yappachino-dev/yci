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
	Use:   "folder [id]",
	Short: "Delete a folder",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		id, err := idFromArgsOrFlag(cmd, flagID, args)
		if err != nil {
			return err
		}
		if id == "" {
			return fmt.Errorf("provide the folder ID as an argument or via --%s", flagID)
		}

		force, _ := cmd.Flags().GetBool(flagForce)

		confirmed, err := confirmDestructive(cmd, fmt.Sprintf("Delete folder %q?", id))
		if err != nil {
			return err
		}
		if !confirmed {
			cmd.Println("Aborted. Nothing was deleted.")
			return nil
		}

		if err := c.FolderService.Delete(ctx, library.FolderID(id), library.DeleteOptions{Force: force}); err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrFolderNotFound: fmt.Sprintf("folder %q does not exist", id),
			})
		}

		cmd.Printf("Deleted folder: %s\n", id)
		return nil
	},
}

var deleteArtistCmd = &cobra.Command{
	Use:   "artist [id]",
	Short: "Delete an artist (linked channels cascade, tracks become orphaned)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		id, err := idFromArgsOrFlag(cmd, flagID, args)
		if err != nil {
			return err
		}
		if id == "" {
			return fmt.Errorf("provide the artist ID as an argument or via --%s", flagID)
		}

		prompt := fmt.Sprintf(
			"Delete artist %q? Linked channels are removed and its tracks become orphaned.",
			id,
		)

		confirmed, err := confirmDestructive(cmd, prompt)
		if err != nil {
			return err
		}
		if !confirmed {
			cmd.Println("Aborted. Nothing was deleted.")
			return nil
		}

		if err := c.ArtistService.Delete(ctx, library.ArtistID(id)); err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrArtistNotFound: fmt.Sprintf("artist %q does not exist", id),
			})
		}

		cmd.Printf("Deleted artist: %s\n", id)
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
			return mapSentinels(err, map[error]string{
				library.ErrArtistNotFound:   fmt.Sprintf("artist %q does not exist", artistID),
				library.ErrChannelNotLinked: fmt.Sprintf("channel %q is not linked to artist %q", channelName, artistID),
			})
		}

		cmd.Printf("Unlinked channel %q from artist %q\n", channelName, artistID)
		return nil
	},
}

var deleteTrackCmd = &cobra.Command{
	Use:   "track [id]",
	Short: "Delete a track from the database AND remove the physical file",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		id, err := idFromArgsOrFlag(cmd, flagID, args)
		if err != nil {
			return err
		}
		if id == "" {
			return fmt.Errorf("provide the track ID (or an unambiguous prefix) as an argument or via --%s", flagID)
		}

		trackID, err := resolveTrackID(ctx, c, id)
		if err != nil {
			return err
		}

		prompt := fmt.Sprintf(
			"Delete track %q? Its file will be permanently removed from disk.",
			trackID,
		)

		confirmed, err := confirmDestructive(cmd, prompt)
		if err != nil {
			return err
		}
		if !confirmed {
			cmd.Println("Aborted. Nothing was deleted.")
			return nil
		}

		if err := c.TrackService.Delete(ctx, trackID); err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrTrackNotFound: fmt.Sprintf("track %q does not exist", id),
			})
		}

		cmd.Printf("Deleted track and physical file for ID: %s\n", trackID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(deleteCmd)

	deleteCmd.AddCommand(deleteFolderCmd)
	deleteCmd.AddCommand(deleteArtistCmd)
	deleteCmd.AddCommand(deleteChannelCmd)
	deleteCmd.AddCommand(deleteTrackCmd)

	deleteCmd.PersistentFlags().Bool(flagYes, false, "Skip confirmation prompt (required in non-interactive sessions)")

	deleteFolderCmd.Flags().String(flagID, "", "The ID of the folder to delete")
	deleteFolderCmd.Flags().Bool(flagForce, false, "Force delete a folder even if it contains artists or sub-folders")
	deleteFolderCmd.RegisterFlagCompletionFunc(flagID, folderIDCompletion)

	deleteArtistCmd.Flags().String(flagID, "", "The ID of the artist to delete")
	deleteArtistCmd.RegisterFlagCompletionFunc(flagID, artistIDCompletion)

	deleteChannelCmd.Flags().String(flagArtist, "", "The target Artist ID (Required)")
	deleteChannelCmd.Flags().String(flagName, "", "The YouTube channel name to unlink (Required)")
	deleteChannelCmd.MarkFlagRequired(flagArtist)
	deleteChannelCmd.MarkFlagRequired(flagName)
	deleteChannelCmd.RegisterFlagCompletionFunc(flagArtist, artistIDCompletion)
	deleteChannelCmd.RegisterFlagCompletionFunc(flagName, channelNameCompletion)

	deleteTrackCmd.Flags().String(flagID, "", "The UUID of the track to delete")
	deleteTrackCmd.RegisterFlagCompletionFunc(flagID, trackIDCompletion)
}
