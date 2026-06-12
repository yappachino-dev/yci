package cmd

import (
	"fmt"
	"strings"

	"playlist-manager/internal/library"

	"github.com/spf13/cobra"
)

// --- Command Definitions ---

var editCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit an existing resource in the library",
	Args:  cobra.NoArgs,
}

var editFolderCmd = &cobra.Command{
	Use:   "folder",
	Short: "Rename or move a folder",
	RunE: func(cmd *cobra.Command, args []string) error {
		oldID, _ := cmd.Flags().GetString("id")
		oldID = strings.TrimSpace(oldID)

		params := library.FolderUpdateParams{}

		if cmd.Flags().Changed("name") {
			n, _ := cmd.Flags().GetString("name")
			params.Name = library.FolderName(strings.TrimSpace(n))
		}
		if cmd.Flags().Changed("parent") {
			p, _ := cmd.Flags().GetString("parent")
			params.Parent = library.FolderID(strings.TrimSpace(p))
		}
		if cmd.Flags().Changed("new-id") {
			n, _ := cmd.Flags().GetString("new-id")
			params.NewID = library.FolderID(strings.TrimSpace(n))
		}

		if err := appl.FolderService.Update(cmd.Context(), library.FolderID(oldID), params); err != nil {
			return err
		}

		fmt.Printf("  Updated folder: %s\n", oldID)
		return nil
	},
}

var editArtistCmd = &cobra.Command{
	Use:   "artist",
	Short: "Rename or move an artist",
	RunE: func(cmd *cobra.Command, args []string) error {
		oldID, _ := cmd.Flags().GetString("id")
		oldID = strings.TrimSpace(oldID)

		params := library.ArtistUpdateParams{}

		if cmd.Flags().Changed("name") {
			name, _ := cmd.Flags().GetString("name")
			params.Name = library.ArtistName(strings.TrimSpace(name))
		}
		if cmd.Flags().Changed("folder") {
			folder, _ := cmd.Flags().GetString("folder")
			params.Folder = library.FolderID(strings.TrimSpace(folder))
		}
		if cmd.Flags().Changed("new-id") {
			newID, _ := cmd.Flags().GetString("new-id")
			params.NewID = library.ArtistID(strings.TrimSpace(newID))
		}

		if err := appl.ArtistService.Update(cmd.Context(), library.ArtistID(oldID), params); err != nil {
			return err
		}

		fmt.Printf("  Updated artist: %s\n", oldID)
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

	// --- Dynamic Autocompletions ---

	editFolderCmd.RegisterFlagCompletionFunc("id", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
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

	editFolderCmd.RegisterFlagCompletionFunc("parent", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
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

	editArtistCmd.RegisterFlagCompletionFunc("id", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
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

	editArtistCmd.RegisterFlagCompletionFunc("folder", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
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
}
