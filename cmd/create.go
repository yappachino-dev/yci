package cmd

import (
	"fmt"
	"strings"

	"playlist-manager/internal/library"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new resource in the library",
	Args:  cobra.NoArgs,
}

var createFolderCmd = &cobra.Command{
	Use:   "folder",
	Short: "Create a new folder",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString(flagID)
		name, _ := cmd.Flags().GetString(flagName)
		parent, _ := cmd.Flags().GetString(flagParent)

		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		parent = strings.TrimSpace(parent)

		if err := appl.FolderService.Create(cmd.Context(), library.FolderID(id), library.FolderID(parent), library.FolderName(name)); err != nil {
			return err
		}

		fmt.Printf("Created folder: %q (ID: %s) under parent: %s\n", name, id, parent)
		return nil
	},
}

var createArtistCmd = &cobra.Command{
	Use:   "artist",
	Short: "Create a new artist",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString(flagID)
		name, _ := cmd.Flags().GetString(flagName)
		folder, _ := cmd.Flags().GetString(flagFolder)

		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		folder = strings.TrimSpace(folder)

		if err := appl.ArtistService.Create(cmd.Context(), library.ArtistID(id), library.FolderID(folder), library.ArtistName(name)); err != nil {
			return err
		}

		fmt.Printf("Created artist: %q (ID: %s) in folder: %s\n", name, id, folder)
		return nil
	},
}

var createChannelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Link a YouTube channel name to an existing artist",
	RunE: func(cmd *cobra.Command, args []string) error {
		artistID, _ := cmd.Flags().GetString(flagArtist)
		channelName, _ := cmd.Flags().GetString(flagName)

		artistID = strings.TrimSpace(artistID)
		channelName = strings.TrimSpace(channelName)

		artist, err := appl.ArtistService.ByID(cmd.Context(), library.ArtistID(artistID))
		if err != nil {
			return fmt.Errorf("could not find artist with ID %q: %w", artistID, err)
		}

		if err := appl.ChannelService.Link(cmd.Context(), library.ArtistID(artistID), library.ChannelName(channelName)); err != nil {
			return err
		}

		fmt.Printf("Linked channel %q to Artist: %s\n", channelName, artist.Name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(createCmd)

	createCmd.AddCommand(createFolderCmd)
	createCmd.AddCommand(createArtistCmd)
	createCmd.AddCommand(createChannelCmd)

	createFolderCmd.Flags().StringP(flagID, "i", "", "The unique slug ID for the folder")
	createFolderCmd.Flags().StringP(flagName, "n", "", "Display name of the folder (Required)")
	createFolderCmd.Flags().StringP(flagParent, "p", library.RootID, "Parent folder ID (defaults to root)")
	createFolderCmd.MarkFlagRequired(flagName)
	createFolderCmd.RegisterFlagCompletionFunc(flagParent, folderIDCompletion)

	createArtistCmd.Flags().StringP(flagID, "i", "", "The unique slug ID for the artist")
	createArtistCmd.Flags().StringP(flagName, "n", "", "Name of the artist (Required)")
	createArtistCmd.Flags().StringP(flagFolder, "f", "", "Parent folder ID (defaults to folder with same id; root)")
	createArtistCmd.MarkFlagRequired(flagName)
	createArtistCmd.RegisterFlagCompletionFunc(flagFolder, folderIDCompletion)

	createChannelCmd.Flags().StringP(flagArtist, "a", "", "The target Artist ID (Required)")
	createChannelCmd.Flags().StringP(flagName, "n", "", "The YouTube channel name to link (Required)")
	createChannelCmd.MarkFlagRequired(flagArtist)
	createChannelCmd.MarkFlagRequired(flagName)
	createChannelCmd.RegisterFlagCompletionFunc(flagArtist, artistIDCompletion)
}
