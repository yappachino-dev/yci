package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/library"
)

var playlistEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Rename a playlist, or change how its seed tokens combine",
	Long: `Change a playlist's name, or how the tokens in its recipe combine.

Renaming also follows the exported .m3u. Leaving it behind would put a
playlist on the phone under the old name that no future export could update.

--match changes how the stored tokens combine: "all" requires every token to
be present, "any" requires at least one. It does not re-derive membership;
run ` + "`yci playlist reseed`" + ` afterwards to see the difference and apply it.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		newName, _ := cmd.Flags().GetString(flagName)
		match, _ := cmd.Flags().GetString(flagMatch)
		hasMatch := cmd.Flags().Changed(flagMatch)

		if newName == "" && !hasMatch {
			return fmt.Errorf("nothing to change: pass --%s to rename or --%s to change matching", flagName, flagMatch)
		}

		if newName != "" {
			if err := renamePlaylist(cmd, c, args[0], newName); err != nil {
				return err
			}
		}

		if hasMatch {
			if err := c.PlaylistService.SetMatchMode(ctx, args[0], library.ParseMatchMode(match)); err != nil {
				var noRecipe *library.NoRecipeError
				if errors.As(err, &noRecipe) {
					return err
				}
				return fmt.Errorf("set match mode for %q: %w", args[0], err)
			}
			cmd.Printf("Matching mode for %q is now %q. Re-derive with: yci playlist reseed %s\n",
				args[0], library.ParseMatchMode(match), args[0])
		}

		return nil
	},
}

func renamePlaylist(cmd *cobra.Command, c *app.Container, from, to string) error {
	result, err := c.PlaylistService.Rename(cmd.Context(), from, to)
	if err != nil {
		return mapSentinels(err, map[error]string{
			library.ErrPlaylistAlreadyExists: fmt.Sprintf("a playlist named %q already exists", to),
		})
	}

	if result.OldName == result.NewName {
		cmd.Printf("Playlist is already named %q; nothing changed.\n", result.OldName)
	} else {
		cmd.Printf("Renamed playlist %q to %q.\n", result.OldName, result.NewName)
	}

	switch {
	case result.ExportMoved:
		cmd.Printf("Exported .m3u followed the rename.\n")
	case result.OrphanedExport != "":
		// Silently leaving this file would ship a playlist to the phone
		// that never updates again, so it is named explicitly.
		cmd.Printf("Warning: %s still exists under the old name and will sync to your phone as-is.\n", result.OrphanedExport)
		cmd.Printf("Remove it yourself, or re-export and delete: yci playlist export %s\n", result.NewName)
	}

	return nil
}

func init() {
	playlistEditCmd.Flags().String(flagName, "", "New name for the playlist")
	playlistEditCmd.Flags().String(flagMatch, "all", `How the seed tokens combine: "all" or "any"`)
}
