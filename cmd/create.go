package cmd

import (
	"fmt"
	"strings"

	"playlist-manager/internal/library"

	"github.com/spf13/cobra"
)

// --- Command Definitions ---

var createCmd = &cobra.Command{
	Use:     "create",
	Aliases: []string{"mk", "add"},
	Short:   "Create a new resource in the library",
	Args:    cobra.NoArgs,
}

var createFolderCmd = &cobra.Command{
	Use:     "folder",
	Aliases: []string{"f"},
	Short:   "Create a new folder",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		name, _ := cmd.Flags().GetString("name")
		parent, _ := cmd.Flags().GetString("parent")

		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		parent = strings.TrimSpace(parent)

		if err := appl.FolderService.Create(cmd.Context(), library.FolderID(id), library.FolderID(parent), library.FolderName(name)); err != nil {
			return err
		}

		fmt.Printf(" Created folder: %q (ID: %s) under parent: %s\n", name, id, parent)
		return nil
	},
}

var createArtistCmd = &cobra.Command{
	Use:     "artist",
	Aliases: []string{"a"},
	Short:   "Create a new artist",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		name, _ := cmd.Flags().GetString("name")
		folder, _ := cmd.Flags().GetString("folder")

		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		folder = strings.TrimSpace(folder)

		if err := appl.ArtistService.Create(cmd.Context(), library.ArtistID(id), library.FolderID(folder), library.ArtistName(name)); err != nil {
			return err
		}

		fmt.Printf(" Created artist: %q (ID: %s) in folder: %s\n", name, id, folder)
		return nil
	},
}

var createChannelCmd = &cobra.Command{
	Use:     "channel",
	Aliases: []string{"c", "link"},
	Short:   "Link a YouTube channel name to an existing artist",
	RunE: func(cmd *cobra.Command, args []string) error {
		artistID, _ := cmd.Flags().GetString("artist")
		channelName, _ := cmd.Flags().GetString("name")

		artistID = strings.TrimSpace(artistID)
		channelName = strings.TrimSpace(channelName)

		artist, err := appl.ArtistService.ByID(cmd.Context(), library.ArtistID(artistID))
		if err != nil {
			return fmt.Errorf("could not find artist with ID %q: %w", artistID, err)
		}

		if err := appl.ChannelLinker.LinkChannelToArtist(cmd.Context(), library.ArtistID(artistID), channelName); err != nil {
			return err
		}

		fmt.Printf(" Linked channel %q to Artist: %s\n", channelName, artist.Name)
		return nil
	},
}

// --- Initialization ---

func init() {
	rootCmd.AddCommand(createCmd)

	createCmd.AddCommand(createFolderCmd)
	createCmd.AddCommand(createArtistCmd)
	createCmd.AddCommand(createChannelCmd)

	createFolderCmd.Flags().StringP("id", "i", "", "The unique slug ID for the folder")
	createFolderCmd.Flags().StringP("name", "n", "", "Display name of the folder (Required)")
	createFolderCmd.Flags().StringP("parent", "p", library.RootID, "Parent folder ID (defaults to root)")
	// createFolderCmd.MarkFlagRequired("id")
	createFolderCmd.MarkFlagRequired("name")

	createArtistCmd.Flags().StringP("id", "i", "", "The unique slug ID for the artist")
	createArtistCmd.Flags().StringP("name", "n", "", "Name of the artist (Required)")
	createArtistCmd.Flags().StringP("folder", "f", "", "Parent folder ID (defaults to folder with same id; root)")
	// createArtistCmd.MarkFlagRequired("id")
	createArtistCmd.MarkFlagRequired("name")

	createChannelCmd.Flags().StringP("artist", "a", "", "The target Artist ID (Required)")
	createChannelCmd.Flags().StringP("name", "n", "", "The YouTube channel name to link (Required)")
	createChannelCmd.MarkFlagRequired("artist")
	createChannelCmd.MarkFlagRequired("name")

	// --- Dynamic Autocompletions ---

	createFolderCmd.RegisterFlagCompletionFunc("parent", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		folders, err := appl.FolderService.All(cmd.Context())
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		var comps []string
		for _, f := range folders {
			if strings.HasPrefix(string(f.ID), toComplete) {
				comps = append(comps, fmt.Sprintf("%s\t%s", f.ID, f.Name))
			}
		}

		return comps, cobra.ShellCompDirectiveNoFileComp
	})

	createArtistCmd.RegisterFlagCompletionFunc("folder", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		folders, err := appl.FolderService.All(cmd.Context())
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		var comps []string
		for _, f := range folders {
			if strings.HasPrefix(string(f.ID), toComplete) {
				comps = append(comps, fmt.Sprintf("%s\t%s", f.ID, f.Name))
			}
		}

		return comps, cobra.ShellCompDirectiveNoFileComp
	})

	createChannelCmd.RegisterFlagCompletionFunc("artist", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		artists, err := appl.ArtistService.All(cmd.Context())
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		var comps []string
		for _, a := range artists {
			if strings.HasPrefix(string(a.ID), toComplete) {
				comps = append(comps, fmt.Sprintf("%s\t%s", a.ID, a.Name))
			}
		}

		return comps, cobra.ShellCompDirectiveNoFileComp
	})
}
