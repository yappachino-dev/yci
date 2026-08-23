package cli

import (
	"fmt"
	"strings"

	"github.com/yappachino-dev/yci/internal/library"

	"github.com/spf13/cobra"
)

const (
	flagNewID        = "new-id"
	flagUpdateIDSlug = "update-id-slug"
)

var editCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit an existing resource in the library",
	Args:  cobra.NoArgs,
}

var editFolderCmd = &cobra.Command{
	Use:   "folder",
	Short: "Rename or move a folder",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		newSlug, _ := cmd.Flags().GetBool(flagUpdateIDSlug)
		nameChanged := cmd.Flags().Changed(flagName)

		if newSlug && !nameChanged {
			return fmt.Errorf("error: --%s requires --%s to be provided", flagUpdateIDSlug, flagName)
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		c := Container(cmd.Context())

		oldID, _ := cmd.Flags().GetString(flagID)
		oldID = strings.TrimSpace(oldID)

		name, _ := cmd.Flags().GetString(flagName)
		folder, _ := cmd.Flags().GetString(flagParent)
		newID, _ := cmd.Flags().GetString(flagNewID)
		newSlug, _ := cmd.Flags().GetBool(flagUpdateIDSlug)

		params := library.FolderUpdateParams{
			NewID:            library.FolderID(newID),
			Name:             library.FolderName(name),
			Parent:           library.FolderID(folder),
			RegenerateSlugID: newSlug,
		}

		f, err := c.FolderService.Update(cmd.Context(), library.FolderID(oldID), params)
		if err != nil {
			return err
		}

		fmt.Printf("Updated folder: %s\n", f.ID)
		return nil
	},
}

var editArtistCmd = &cobra.Command{
	Use:   "artist",
	Short: "Rename or move an artist",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		newSlug, _ := cmd.Flags().GetBool(flagUpdateIDSlug)
		nameChanged := cmd.Flags().Changed(flagName)

		if newSlug && !nameChanged {
			return fmt.Errorf("error: --%s requires --%s to be provided", flagUpdateIDSlug, flagName)
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		c := Container(cmd.Context())

		oldID, _ := cmd.Flags().GetString(flagID)
		oldID = strings.TrimSpace(oldID)

		name, _ := cmd.Flags().GetString(flagName)
		folder, _ := cmd.Flags().GetString(flagFolder)
		newID, _ := cmd.Flags().GetString(flagNewID)
		newSlug, _ := cmd.Flags().GetBool(flagUpdateIDSlug)

		params := library.ArtistUpdateParams{
			NewID:            library.ArtistID(newID),
			Name:             library.ArtistName(name),
			Folder:           library.FolderID(folder),
			RegenerateSlugID: newSlug,
		}

		a, err := c.ArtistService.Update(cmd.Context(), library.ArtistID(oldID), params)
		if err != nil {
			return err
		}

		fmt.Printf("Updated artist: %s\n", a.ID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(editCmd)

	editCmd.AddCommand(editFolderCmd)
	editCmd.AddCommand(editArtistCmd)

	editFolderCmd.Flags().String(flagID, "", "The current ID of the folder to edit (Required)")
	editFolderCmd.Flags().String(flagName, "", "Set a new display name")
	editFolderCmd.Flags().String(flagParent, "", "Move to a new parent folder ID")
	editFolderCmd.Flags().String(flagNewID, "", "Change the slug ID (cascades automatically)")
	editFolderCmd.Flags().Bool(flagUpdateIDSlug, false, "Update the slug ID on name change")
	editFolderCmd.MarkFlagRequired(flagID)
	editFolderCmd.MarkFlagsMutuallyExclusive(flagNewID, flagUpdateIDSlug)
	editFolderCmd.RegisterFlagCompletionFunc(flagID, folderIDCompletion)
	editFolderCmd.RegisterFlagCompletionFunc(flagParent, folderIDCompletion)

	editArtistCmd.Flags().String(flagID, "", "The current ID of the artist to edit (Required)")
	editArtistCmd.Flags().String(flagName, "", "Set a new artist name")
	editArtistCmd.Flags().String(flagFolder, "", "Move to a new folder ID")
	editArtistCmd.Flags().String(flagNewID, "", "Change the slug ID (cascades automatically)")
	editArtistCmd.Flags().Bool(flagUpdateIDSlug, false, "Update the slug ID on name change")
	editArtistCmd.MarkFlagRequired(flagID)
	editArtistCmd.MarkFlagsMutuallyExclusive(flagNewID, flagUpdateIDSlug)
	editArtistCmd.RegisterFlagCompletionFunc(flagID, artistIDCompletion)
	editArtistCmd.RegisterFlagCompletionFunc(flagFolder, folderIDCompletion)
}
