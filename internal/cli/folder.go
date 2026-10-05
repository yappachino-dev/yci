package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/yappachino-dev/yci/internal/dto"
	"github.com/yappachino-dev/yci/internal/library"

	"github.com/spf13/cobra"
)

var folderCmd = &cobra.Command{
	Use:   "folder",
	Short: "Manage folders in the library",
}

var folderCreateCmd = &cobra.Command{
	Use:   "create [id]",
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

var folderListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all folders in a visual tree hierarchy",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listFolders(cmd.Context(), cmd)
	},
}

var folderEditCmd = &cobra.Command{
	Use:               "edit [id]",
	ValidArgsFunction: folderIDCompletion,
	Short:             "Rename or move a folder",
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
			return fmt.Errorf("provide the folder ID as an argument or via --%s", flagID)
		}

		folderID, err := resolveFolderID(cmd.Context(), c, oldID)
		if err != nil {
			return err
		}

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

		f, err := c.FolderService.Update(cmd.Context(), folderID, params)
		if err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrFolderNotFound: fmt.Sprintf("folder %q does not exist", oldID),
				library.ErrParentNotFound: fmt.Sprintf("parent folder %q does not exist", params.Parent),
			})
		}

		cmd.Printf("Updated folder: %s\n", f.ID)
		return nil
	},
}

var folderDeleteCmd = &cobra.Command{
	Use:               "delete [id]",
	ValidArgsFunction: folderIDCompletion,
	Short:             "Delete a folder",
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		id, err := idFromArgsOrFlag(cmd, flagID, args)
		if err != nil {
			return err
		}
		if id == "" {
			return fmt.Errorf("provide the folder ID as an argument or via --%s", flagID)
		}

		folderID, err := resolveFolderID(ctx, c, id)
		if err != nil {
			return err
		}

		force, _ := cmd.Flags().GetBool(flagForce)

		confirmed, err := confirmDestructive(cmd, fmt.Sprintf("Delete folder %q?", folderID))
		if err != nil {
			return err
		}
		if !confirmed {
			cmd.Println("Aborted. Nothing was deleted.")
			return nil
		}

		if err := c.FolderService.Delete(ctx, folderID, library.DeleteOptions{Force: force}); err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrFolderNotFound: fmt.Sprintf("folder %q does not exist", id),
			})
		}

		cmd.Printf("Deleted folder: %s\n", id)
		return nil
	},
}

func listFolders(ctx context.Context, cmd *cobra.Command) error {
	c := Container(ctx)

	folders, err := c.FolderService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch folders: %w", err)
	}

	// The seeded root row always exists; only real folders make a tree.
	userFolders := 0
	for _, f := range folders {
		if f.ID != library.FolderID(library.RootID) {
			userFolders++
		}
	}

	if jsonRequested(cmd) {
		out := folderTreeJSON(folders)
		if out == nil {
			out = []dto.Folder{}
		}

		return dto.WriteJSON(cmd.OutOrStdout(), out)
	}

	if userFolders == 0 {
		cmd.PrintErrln("No folders yet. Create one with `yci folder create`.")
		return nil
	}

	table := NewTable(cmd.OutOrStdout(), "ID", "PARENT", "NAME")

	childrenOf := make(map[library.FolderID][]library.Folder)
	for _, f := range folders {
		childrenOf[f.ParentID] = append(childrenOf[f.ParentID], f)
	}

	var traverse func(parentID library.FolderID, prefix string)
	traverse = func(parentID library.FolderID, prefix string) {
		children := childrenOf[parentID]
		for i, child := range children {
			isLast := (i == len(children)-1)

			connector := "├─ "
			if isLast {
				connector = "└─ "
			}

			nameWithTree := prefix + connector + string(child.Name)
			table.Row(string(child.ID), string(child.ParentID), nameWithTree)

			newPrefix := prefix + "│  "
			if isLast {
				newPrefix = prefix + "   "
			}

			traverse(child.ID, newPrefix)
		}
	}

	traverse(library.FolderID(library.RootID), "")

	table.Flush()
	return nil
}

// folderTreeJSON flattens the folder hierarchy into parent-first order with a
// depth per row.
//
// The root is omitted: it exists to give the tree a starting point and is not
// a folder anyone creates or refers to. The order is the same one the human
// view draws, so a diff between the two views lines up row for row.
func folderTreeJSON(folders []library.Folder) []dto.Folder {
	childrenOf := make(map[library.FolderID][]library.Folder)
	for _, f := range folders {
		childrenOf[f.ParentID] = append(childrenOf[f.ParentID], f)
	}

	root := library.FolderID(library.RootID)

	var out []dto.Folder

	var traverse func(parentID library.FolderID, depth int)
	traverse = func(parentID library.FolderID, depth int) {
		for _, child := range childrenOf[parentID] {
			if child.ID == root {
				continue
			}
			out = append(out, dto.ToFolder(child, depth))
			traverse(child.ID, depth+1)
		}
	}

	traverse(root, 0)

	return out
}

func init() {
	folderCmd.AddCommand(
		folderCreateCmd,
		folderListCmd,
		folderEditCmd,
		folderDeleteCmd,
	)

	folderCreateCmd.Flags().String(flagID, "", "The unique slug ID for the folder")
	folderCreateCmd.Flags().String(flagName, "", "Display name of the folder (Required)")
	folderCreateCmd.Flags().String(flagParent, "", "Parent folder ID (defaults to root)")
	folderCreateCmd.MarkFlagRequired(flagName)
	folderCreateCmd.RegisterFlagCompletionFunc(flagParent, folderIDCompletion)

	folderEditCmd.Flags().String(flagID, "", "The current ID of the folder to edit")
	folderEditCmd.Flags().String(flagName, "", "Set a new display name")
	folderEditCmd.Flags().String(flagParent, "", "Move to a new parent folder ID")
	folderEditCmd.Flags().String(flagNewID, "", "Change the slug ID (cascades automatically)")
	folderEditCmd.Flags().Bool(flagUpdateIDSlug, false, "Update the slug ID on name change")
	folderEditCmd.MarkFlagsMutuallyExclusive(flagNewID, flagUpdateIDSlug)
	folderEditCmd.RegisterFlagCompletionFunc(flagID, folderIDCompletion)
	folderEditCmd.RegisterFlagCompletionFunc(flagParent, folderIDCompletion)

	folderDeleteCmd.Flags().String(flagID, "", "The ID of the folder to delete")
	folderDeleteCmd.Flags().Bool(flagYes, false, "Skip confirmation prompt (required in non-interactive sessions)")
	folderDeleteCmd.Flags().Bool(flagForce, false, "Force delete a folder even if it contains artists or sub-folders")
	folderDeleteCmd.RegisterFlagCompletionFunc(flagID, folderIDCompletion)

	folderListCmd.Flags().Bool(flagJSON, false, "Emit JSON instead of an aligned table, for scripts")
}
