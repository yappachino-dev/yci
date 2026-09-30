package cli

import (
	"context"
	"fmt"
	"strings"

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
	Args:  cobra.NoArgs,
}

var artistCreateCmd = &cobra.Command{
	Use:   "create [id]",
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

var artistListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all managed artists",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listArtists(cmd.Context(), cmd)
	},
}

var artistEditCmd = &cobra.Command{
	Use:   "edit [id]",
	Short: "Rename or move an artist",
	Args:  cobra.MaximumNArgs(1),
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
	Use:   "delete [id]",
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
		cmd.Println("No artists yet. Create one with `yci artist create`.")
		return nil
	}

	if jsonRequested(cmd) {
		out := make([]jsonArtist, 0, len(artists))
		for _, a := range artists {
			out = append(out, toJSONArtist(a))
		}

		return writeJSON(cmd.OutOrStdout(), out)
	}

	w := newTabWriter(cmd.OutOrStdout())
	fmt.Fprintln(w, "ID\tNAME\tFOLDER_ID")
	fmt.Fprintln(w, "--\t----\t---------")

	for _, a := range artists {
		fmt.Fprintf(w, "%s\t%s\t%s\n", a.ID, a.Name, a.FolderID)
	}

	w.Flush()
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
	artistCreateCmd.Flags().String(flagFolder, "", "Parent folder ID (defaults to folder with same id; root)")
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
