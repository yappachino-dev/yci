package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// --- Command Definitions ---

var editCmd = &cobra.Command{
	Use:     "edit",
	Aliases: []string{"mod", "update"},
	Short:   "Edit an existing resource in the library",
	Args:    cobra.NoArgs,
}

var editFolderCmd = &cobra.Command{
	Use:     "folder",
	Aliases: []string{"f"},
	Short:   "Rename or move a folder",
	RunE: func(cmd *cobra.Command, args []string) error {
		oldID, _ := cmd.Flags().GetString("id")
		oldID = strings.TrimSpace(oldID)

		folder, err := lib.GetFolder(oldID)
		if err != nil {
			return fmt.Errorf("could not fetch folder: %w", err)
		}

		if cmd.Flags().Changed("name") {
			name, _ := cmd.Flags().GetString("name")
			folder.DisplayName = strings.TrimSpace(name)
		}
		if cmd.Flags().Changed("parent") {
			parent, _ := cmd.Flags().GetString("parent")
			folder.ParentID = strings.TrimSpace(parent)
		}
		if cmd.Flags().Changed("new-id") {
			newID, _ := cmd.Flags().GetString("new-id")
			folder.ID = strings.TrimSpace(newID)
		}

		if err := lib.UpdateFolder(oldID, folder); err != nil {
			return err
		}

		fmt.Printf("✏️  Updated folder: %s\n", folder.ID)
		return nil
	},
}

var editArtistCmd = &cobra.Command{
	Use:     "artist",
	Aliases: []string{"a"},
	Short:   "Rename or move an artist",
	RunE: func(cmd *cobra.Command, args []string) error {
		oldID, _ := cmd.Flags().GetString("id")
		oldID = strings.TrimSpace(oldID)

		artist, err := lib.GetArtist(oldID)
		if err != nil {
			return fmt.Errorf("could not fetch artist: %w", err)
		}

		if cmd.Flags().Changed("name") {
			name, _ := cmd.Flags().GetString("name")
			artist.Artist = strings.TrimSpace(name)
		}
		if cmd.Flags().Changed("folder") {
			folder, _ := cmd.Flags().GetString("folder")
			artist.FolderID = strings.TrimSpace(folder)
		}
		if cmd.Flags().Changed("new-id") {
			newID, _ := cmd.Flags().GetString("new-id")
			artist.ID = strings.TrimSpace(newID)
		}

		if err := lib.UpdateArtist(oldID, artist); err != nil {
			return err
		}

		fmt.Printf("✏️  Updated artist: %s\n", artist.ID)
		return nil
	},
}

// --- Initialization ---

func init() {
	rootCmd.AddCommand(editCmd)

	editCmd.AddCommand(editFolderCmd)
	editCmd.AddCommand(editArtistCmd)

	editFolderCmd.Flags().StringP("id", "i", "", "The current ID of the folder to edit (Required)")
	editFolderCmd.Flags().StringP("name", "n", "", "Set a new display name")
	editFolderCmd.Flags().StringP("parent", "p", "", "Move to a new parent folder ID")
	editFolderCmd.Flags().String("new-id", "", "Change the slug ID (cascades automatically)")
	editFolderCmd.MarkFlagRequired("id")

	editArtistCmd.Flags().StringP("id", "i", "", "The current ID of the artist to edit (Required)")
	editArtistCmd.Flags().StringP("name", "n", "", "Set a new artist name")
	editArtistCmd.Flags().StringP("folder", "f", "", "Move to a new folder ID")
	editArtistCmd.Flags().String("new-id", "", "Change the slug ID (cascades automatically)")
	editArtistCmd.MarkFlagRequired("id")
}
