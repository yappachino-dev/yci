package cmd

import (
	"fmt"
	"strings"

	"playlist-manager/internal/library"

	"github.com/spf13/cobra"
)

const flagNewID = "new-id"

var editCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit an existing resource in the library",
	Args:  cobra.NoArgs,
}

var editFolderCmd = &cobra.Command{
	Use:   "folder",
	Short: "Rename or move a folder",
	RunE: func(cmd *cobra.Command, args []string) error {
		oldID, _ := cmd.Flags().GetString(flagID)
		oldID = strings.TrimSpace(oldID)

		params := library.FolderUpdateParams{}

		if cmd.Flags().Changed(flagName) {
			n, _ := cmd.Flags().GetString(flagName)
			params.Name = library.FolderName(strings.TrimSpace(n))
		}
		if cmd.Flags().Changed(flagParent) {
			p, _ := cmd.Flags().GetString(flagParent)
			params.Parent = library.FolderID(strings.TrimSpace(p))
		}
		if cmd.Flags().Changed(flagNewID) {
			n, _ := cmd.Flags().GetString(flagNewID)
			params.NewID = library.FolderID(strings.TrimSpace(n))
		}

		if err := appl.FolderService.Update(cmd.Context(), library.FolderID(oldID), params); err != nil {
			return err
		}

		fmt.Printf("Updated folder: %s\n", oldID)
		return nil
	},
}

var editArtistCmd = &cobra.Command{
	Use:   "artist",
	Short: "Rename or move an artist",
	RunE: func(cmd *cobra.Command, args []string) error {
		oldID, _ := cmd.Flags().GetString(flagID)
		oldID = strings.TrimSpace(oldID)

		params := library.ArtistUpdateParams{}

		if cmd.Flags().Changed(flagName) {
			name, _ := cmd.Flags().GetString(flagName)
			params.Name = library.ArtistName(strings.TrimSpace(name))
		}
		if cmd.Flags().Changed(flagFolder) {
			folder, _ := cmd.Flags().GetString(flagFolder)
			params.Folder = library.FolderID(strings.TrimSpace(folder))
		}
		if cmd.Flags().Changed(flagNewID) {
			newID, _ := cmd.Flags().GetString(flagNewID)
			params.NewID = library.ArtistID(strings.TrimSpace(newID))
		}

		if err := appl.ArtistService.Update(cmd.Context(), library.ArtistID(oldID), params); err != nil {
			return err
		}

		fmt.Printf("Updated artist: %s\n", oldID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(editCmd)

	editCmd.AddCommand(editFolderCmd)
	editCmd.AddCommand(editArtistCmd)

	editFolderCmd.Flags().StringP(flagID, "i", "", "The current ID of the folder to edit (Required)")
	editFolderCmd.Flags().StringP(flagName, "n", "", "Set a new display name")
	editFolderCmd.Flags().StringP(flagParent, "p", "", "Move to a new parent folder ID")
	editFolderCmd.Flags().String(flagNewID, "", "Change the slug ID (cascades automatically)")
	editFolderCmd.MarkFlagRequired(flagID)
	editFolderCmd.RegisterFlagCompletionFunc(flagID, folderIDCompletion)
	editFolderCmd.RegisterFlagCompletionFunc(flagParent, folderIDCompletion)

	editArtistCmd.Flags().StringP(flagID, "i", "", "The current ID of the artist to edit (Required)")
	editArtistCmd.Flags().StringP(flagName, "n", "", "Set a new artist name")
	editArtistCmd.Flags().StringP(flagFolder, "f", "", "Move to a new folder ID")
	editArtistCmd.Flags().String(flagNewID, "", "Change the slug ID (cascades automatically)")
	editArtistCmd.MarkFlagRequired(flagID)
	editArtistCmd.RegisterFlagCompletionFunc(flagID, artistIDCompletion)
	editArtistCmd.RegisterFlagCompletionFunc(flagFolder, folderIDCompletion)
}
