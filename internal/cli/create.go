package cli

import (
	"fmt"
	"strings"

	"github.com/yappachino-dev/yci/internal/library"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new resource in the library",
	Args:  cobra.NoArgs,
}

var createFolderCmd = &cobra.Command{
	Use:   "folder [id]",
	Short: "Create a new folder",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := idFromArgsOrFlag(cmd, flagID, args)
		if err != nil {
			return err
		}

		name, _ := cmd.Flags().GetString(flagName)
		parent, _ := cmd.Flags().GetString(flagParent)

		c := Container(cmd.Context())

		name = strings.TrimSpace(name)
		parent = strings.TrimSpace(parent)

		f, err := c.FolderService.Create(cmd.Context(), library.FolderID(id), library.FolderID(parent), library.FolderName(name))
		if err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrFolderAlreadyExists: fmt.Sprintf("folder %q already exists", id),
				library.ErrParentNotFound:      fmt.Sprintf("parent folder %q does not exist", parent),
			})
		}

		cmd.Printf("Created folder: %q (ID: %s) under parent: %s\n", f.Name, f.ID, f.ParentID)
		return nil
	},
}

var createArtistCmd = &cobra.Command{
	Use:   "artist [id]",
	Short: "Create a new artist",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := idFromArgsOrFlag(cmd, flagID, args)
		if err != nil {
			return err
		}

		name, _ := cmd.Flags().GetString(flagName)
		folder, _ := cmd.Flags().GetString(flagFolder)

		c := Container(cmd.Context())

		name = strings.TrimSpace(name)
		folder = strings.TrimSpace(folder)

		a, err := c.ArtistService.Create(cmd.Context(), library.ArtistID(id), library.FolderID(folder), library.ArtistName(name))
		if err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrArtistAlreadyExists: fmt.Sprintf("artist %q already exists", id),
				library.ErrFolderNotFound:      fmt.Sprintf("folder %q does not exist", folder),
			})
		}

		cmd.Printf("Created artist: %q (ID: %s) in folder: %s\n", a.Name, a.ID, a.FolderID)
		return nil
	},
}

var createChannelCmd = &cobra.Command{
	Use:   "channel",
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

func init() {
	rootCmd.AddCommand(createCmd)

	createCmd.AddCommand(createFolderCmd)
	createCmd.AddCommand(createArtistCmd)
	createCmd.AddCommand(createChannelCmd)

	createFolderCmd.Flags().String(flagID, "", "The unique slug ID for the folder")
	createFolderCmd.Flags().String(flagName, "", "Display name of the folder (Required)")
	createFolderCmd.Flags().String(flagParent, "", "Parent folder ID (defaults to root)")
	createFolderCmd.MarkFlagRequired(flagName)
	createFolderCmd.RegisterFlagCompletionFunc(flagParent, folderIDCompletion)

	createArtistCmd.Flags().String(flagID, "", "The unique slug ID for the artist")
	createArtistCmd.Flags().String(flagName, "", "Name of the artist (Required)")
	createArtistCmd.Flags().String(flagFolder, "", "Parent folder ID (defaults to folder with same id; root)")
	createArtistCmd.MarkFlagRequired(flagName)
	createArtistCmd.RegisterFlagCompletionFunc(flagFolder, folderIDCompletion)

	createChannelCmd.Flags().String(flagArtist, "", "The target Artist ID (Required)")
	createChannelCmd.Flags().String(flagName, "", "The YouTube channel name to link (Required)")
	createChannelCmd.MarkFlagRequired(flagArtist)
	createChannelCmd.MarkFlagRequired(flagName)
	createChannelCmd.RegisterFlagCompletionFunc(flagArtist, artistIDCompletion)
}
