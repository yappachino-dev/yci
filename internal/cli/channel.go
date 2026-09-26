package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/yappachino-dev/yci/internal/library"

	"github.com/spf13/cobra"
)

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Manage YouTube channel links",
	Args:  cobra.NoArgs,
}

var channelCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Link a YouTube channel name to an existing artist",
	RunE: func(cmd *cobra.Command, args []string) error {
		artistID, _ := cmd.Flags().GetString(flagArtist)
		channelName, _ := cmd.Flags().GetString(flagName)

		c := Container(cmd.Context())

		artistID = strings.TrimSpace(artistID)
		channelName = strings.TrimSpace(channelName)

		resolvedID, err := resolveArtistID(cmd.Context(), c, artistID)
		if err != nil {
			return err
		}

		artist, err := c.ArtistService.ByID(cmd.Context(), resolvedID)
		if err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrArtistNotFound: fmt.Sprintf("artist %q does not exist", artistID),
			})
		}

		if err := c.ChannelService.Link(cmd.Context(), resolvedID, library.ChannelName(channelName)); err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrChannelAlreadyLinked: fmt.Sprintf("channel %q is already linked to an artist", channelName),
			})
		}

		cmd.Printf("Linked channel %q to Artist: %s\n", channelName, artist.Name)
		return nil
	},
}

var channelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all YouTube channel aliases linked to artists",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listChannels(cmd.Context(), cmd)
	},
}

var channelDeleteCmd = &cobra.Command{
	Use:   "delete",
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

func listChannels(ctx context.Context, cmd *cobra.Command) error {
	c := Container(ctx)

	channels, err := c.ChannelService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch channels: %w", err)
	}

	if len(channels) == 0 {
		cmd.Println("No channels linked. Link one with `yci channel create`.")
		return nil
	}

	w := newTabWriter(cmd.OutOrStdout())
	fmt.Fprintln(w, "ID\tARTIST_ID\tCHANNEL_NAME")
	fmt.Fprintln(w, "--\t---------\t------------")

	for _, c := range channels {
		fmt.Fprintf(w, "%d\t%s\t%s\n", c.ID, c.ArtistID, c.Name)
	}

	w.Flush()
	return nil
}

func init() {
	channelCmd.AddCommand(
		channelCreateCmd,
		channelListCmd,
		channelDeleteCmd,
	)

	channelCreateCmd.Flags().String(flagArtist, "", "The target Artist ID (Required)")
	channelCreateCmd.Flags().String(flagName, "", "The YouTube channel name to link (Required)")
	channelCreateCmd.MarkFlagRequired(flagArtist)
	channelCreateCmd.MarkFlagRequired(flagName)
	channelCreateCmd.RegisterFlagCompletionFunc(flagArtist, artistIDCompletion)

	channelDeleteCmd.Flags().String(flagArtist, "", "The target Artist ID (Required)")
	channelDeleteCmd.Flags().String(flagName, "", "The YouTube channel name to unlink (Required)")
	channelDeleteCmd.MarkFlagRequired(flagArtist)
	channelDeleteCmd.MarkFlagRequired(flagName)
	channelDeleteCmd.RegisterFlagCompletionFunc(flagArtist, artistIDCompletion)
	channelDeleteCmd.RegisterFlagCompletionFunc(flagName, channelNameCompletion)
}
