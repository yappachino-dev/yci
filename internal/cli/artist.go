package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/yappachino-dev/yci/internal/dto"
	"github.com/yappachino-dev/yci/internal/library"

	"github.com/spf13/cobra"
)

const (
	flagNewID        = "new-id"
	flagUpdateIDSlug = "update-id-slug"
)

var artistCmd = &cobra.Command{
	Use:   "artist",
	Short: "Manage artists in the library",
}

var artistCreateCmd = &cobra.Command{
	Use:   "create [id]",
	Short: "Create a new artist",
	Long: "Create an artist.\n\n" +
		"With --parent the artist's own folder is created under that parent and\n" +
		"the artist is linked to it in one transaction. --parent and --folder are\n" +
		"mutually exclusive.",
	Args: cobra.MaximumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed(flagParent) && cmd.Flags().Changed(flagFolder) {
			return fmt.Errorf("--%s and --%s are mutually exclusive: the first creates a folder, the second links an existing one", flagParent, flagFolder)
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := idFromArgsOrFlag(cmd, flagID, args)
		if err != nil {
			return err
		}

		name, _ := cmd.Flags().GetString(flagName)
		folder, _ := cmd.Flags().GetString(flagFolder)
		parent, _ := cmd.Flags().GetString(flagParent)

		c := Container(cmd.Context())

		name = strings.TrimSpace(name)
		folder = strings.TrimSpace(folder)
		parent = strings.TrimSpace(parent)

		if parent != "" {
			a, err := c.ArtistService.CreateInFolder(cmd.Context(), library.FolderID(parent), library.ArtistName(name))
			if err != nil {
				return mapSentinels(err, map[error]string{
					library.ErrArtistAlreadyExists: fmt.Sprintf("artist %q already exists", name),
					library.ErrFolderAlreadyExists: fmt.Sprintf("a folder named after %q already exists; pass --%s to link it instead", name, flagFolder),
					library.ErrFolderNotFound:      fmt.Sprintf("parent folder %q does not exist", parent),
				})
			}

			cmd.Printf("Created folder: %s\n", a.FolderID)
			cmd.Printf("Created artist: %q (ID: %s) in folder: %s\n", a.Name, a.ID, a.FolderID)

			return nil
		}

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

var artistListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all managed artists",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listArtists(cmd.Context(), cmd)
	},
}

var artistEditCmd = &cobra.Command{
	Use:               "edit [id]",
	ValidArgsFunction: artistIDCompletion,
	Short:             "Rename or move an artist",
	Args:              cobra.MaximumNArgs(1),
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

		oldID, err := idFromArgsOrFlag(cmd, flagID, args)
		if err != nil {
			return err
		}
		if oldID == "" {
			return fmt.Errorf("provide the artist ID as an argument or via --%s", flagID)
		}

		artistID, err := resolveArtistID(cmd.Context(), c, oldID)
		if err != nil {
			return err
		}

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

		a, err := c.ArtistService.Update(cmd.Context(), artistID, params)
		if err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrArtistNotFound: fmt.Sprintf("artist %q does not exist", oldID),
				library.ErrFolderNotFound: fmt.Sprintf("folder %q does not exist", params.Folder),
			})
		}

		cmd.Printf("Updated artist: %s\n", a.ID)
		return nil
	},
}

var artistDeleteCmd = &cobra.Command{
	Use:               "delete [id]",
	ValidArgsFunction: artistIDCompletion,
	Short:             "Delete an artist (linked channels cascade, tracks become orphaned)",
	Args:              cobra.MaximumNArgs(1),
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

		artistID, err := resolveArtistID(ctx, c, id)
		if err != nil {
			return err
		}

		prompt := fmt.Sprintf(
			"Delete artist %q? Linked channels are removed and its tracks become orphaned.",
			artistID,
		)

		confirmed, err := confirmDestructive(cmd, prompt)
		if err != nil {
			return err
		}
		if !confirmed {
			cmd.Println("Aborted. Nothing was deleted.")
			return nil
		}

		if err := c.ArtistService.Delete(ctx, artistID); err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrArtistNotFound: fmt.Sprintf("artist %q does not exist", id),
			})
		}

		cmd.Printf("Deleted artist: %s\n", id)
		return nil
	},
}

func listArtists(ctx context.Context, cmd *cobra.Command) error {
	c := Container(ctx)

	artists, err := c.ArtistService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch artists: %w", err)
	}

	if len(artists) == 0 {
		cmd.PrintErrln("No artists yet. Create one with `yci artist create`.")
		return nil
	}

	if jsonRequested(cmd) {
		out := make([]dto.Artist, 0, len(artists))
		for _, a := range artists {
			out = append(out, dto.ToArtist(a))
		}

		return dto.WriteJSON(cmd.OutOrStdout(), out)
	}

	table := NewTable(cmd.OutOrStdout(), "ID", "NAME", "FOLDER_ID")

	for _, a := range artists {
		table.Row(string(a.ID), string(a.Name), string(a.FolderID))
	}

	table.Flush()
	return nil
}

func init() {
	artistCmd.AddCommand(
		artistCreateCmd,
		artistListCmd,
		artistEditCmd,
		artistDeleteCmd,
	)

	artistCreateCmd.Flags().String(flagID, "", "The unique slug ID for the artist")
	artistCreateCmd.Flags().String(flagName, "", "Name of the artist (Required)")
	artistCreateCmd.Flags().String(flagFolder, "", "Existing folder ID to place the artist in (defaults to folder with same id; root)")
	artistCreateCmd.Flags().String(flagParent, "", "Create a folder named after the artist under this parent ID, and link the artist to it")
	artistCreateCmd.RegisterFlagCompletionFunc(flagParent, folderIDCompletion)
	artistCreateCmd.MarkFlagRequired(flagName)
	artistCreateCmd.RegisterFlagCompletionFunc(flagFolder, folderIDCompletion)

	artistEditCmd.Flags().String(flagID, "", "The current ID of the artist to edit")
	artistEditCmd.Flags().String(flagName, "", "Set a new artist name")
	artistEditCmd.Flags().String(flagFolder, "", "Move to a new folder ID")
	artistEditCmd.Flags().String(flagNewID, "", "Change the slug ID (cascades automatically)")
	artistEditCmd.Flags().Bool(flagUpdateIDSlug, false, "Update the slug ID on name change")
	artistEditCmd.MarkFlagsMutuallyExclusive(flagNewID, flagUpdateIDSlug)
	artistEditCmd.RegisterFlagCompletionFunc(flagID, artistIDCompletion)
	artistEditCmd.RegisterFlagCompletionFunc(flagFolder, folderIDCompletion)

	artistDeleteCmd.Flags().String(flagID, "", "The ID of the artist to delete")
	artistDeleteCmd.Flags().Bool(flagYes, false, "Skip confirmation prompt (required in non-interactive sessions)")
	artistDeleteCmd.RegisterFlagCompletionFunc(flagID, artistIDCompletion)

	artistListCmd.Flags().Bool(flagJSON, false, "Emit JSON instead of an aligned table, for scripts")
}
