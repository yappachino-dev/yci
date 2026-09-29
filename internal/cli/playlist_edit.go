package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/library"
)

var playlistEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Rename a playlist, or change its seed recipe",
	Long: `Change a playlist's name, or the recipe its membership is derived from.

Renaming also follows the exported .m3u. Leaving it behind would put a
playlist on the phone under the old name that no future export could update.

--match changes how the stored tokens combine: "all" requires every token to
be present, "any" requires at least one.

--exclude and --include adjust which tracks the recipe leaves out. Token
matching is lexical, so it also picks up collabs and titles that merely share
a word; these are how you record the exceptions. Neither re-derives membership
by itself: run ` + "`yci playlist reseed`" + ` afterwards to see the difference
and apply it.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		newName, _ := cmd.Flags().GetString(flagName)
		match, _ := cmd.Flags().GetString(flagMatch)
		hasMatch := cmd.Flags().Changed(flagMatch)
		exclude, _ := cmd.Flags().GetStringSlice(flagExclude)
		include, _ := cmd.Flags().GetStringSlice(flagInclude)

		if newName == "" && !hasMatch && len(exclude) == 0 && len(include) == 0 {
			return fmt.Errorf("nothing to change: pass --%s to rename, --%s to change matching, or --%s/--%s to adjust exclusions",
				flagName, flagMatch, flagExclude, flagInclude)
		}

		if newName != "" {
			if err := renamePlaylist(cmd, c, args[0], newName); err != nil {
				return err
			}
		}

		// The mode is applied after a rename, so it targets the new name;
		// a rename that also changes the mode would otherwise look the old
		// name up and fail to find it.
		target := args[0]
		if newName != "" {
			target = newName
		}

		if hasMatch {
			if err := c.PlaylistService.SetMatchMode(ctx, target, library.ParseMatchMode(match)); err != nil {
				var noRecipe *library.NoRecipeError
				if errors.As(err, &noRecipe) {
					return err
				}
				return fmt.Errorf("set match mode for %q: %w", target, err)
			}
			cmd.Printf("Matching mode for %q is now %q. Re-derive with: yci playlist reseed %s\n",
				target, library.ParseMatchMode(match), target)
		}

		if len(exclude) > 0 || len(include) > 0 {
			n, err := editExclusions(ctx, c, target, exclude, include)
			if err != nil {
				return err
			}
			cmd.Printf("Exclusions for %q now hold %d track(s). Re-derive with: yci playlist reseed %s\n",
				target, n, target)
		}

		return nil
	},
}

// editExclusions adds and removes entries from a recipe's exclusion list and
// returns the resulting size.
//
// The playlist is read first so the change is a merge rather than a replace:
// --exclude naming one track must not silently clear the other exclusions a
// previous wizard session recorded.
func editExclusions(ctx context.Context, c *app.Container, name string, exclude, include []string) (int, error) {
	p, err := c.PlaylistService.ByName(ctx, name)
	if err != nil {
		return 0, fmt.Errorf("find playlist %q: %w", name, err)
	}

	if !p.HasRecipe() {
		return 0, &library.NoRecipeError{Playlist: p.Name}
	}

	drop, err := resolveExclusions(ctx, c, include)
	if err != nil {
		return 0, err
	}
	add, err := resolveExclusions(ctx, c, exclude)
	if err != nil {
		return 0, err
	}

	dropSet := make(map[library.TrackID]bool, len(drop))
	for _, id := range drop {
		dropSet[id] = true
	}

	next := make([]library.TrackID, 0, len(p.FragmentExcludes)+len(add))
	seen := make(map[library.TrackID]bool, len(p.FragmentExcludes)+len(add))
	for _, id := range p.FragmentExcludes {
		if dropSet[id] || seen[id] {
			continue
		}
		seen[id] = true
		next = append(next, id)
	}
	for _, id := range add {
		if seen[id] {
			continue
		}
		seen[id] = true
		next = append(next, id)
	}

	if err := c.PlaylistService.SetExcludes(ctx, name, next); err != nil {
		return 0, fmt.Errorf("set exclusions for %q: %w", name, err)
	}

	return len(next), nil
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
	playlistEditCmd.Flags().StringSlice(flagExclude, nil, "Leave these track IDs (prefix allowed) out on re-derivation")
	playlistEditCmd.Flags().StringSlice(flagInclude, nil, "Stop excluding these track IDs (prefix allowed)")
}
