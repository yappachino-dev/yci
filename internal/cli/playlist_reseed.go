package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/library"
)

const flagAccept = "accept"

// changePreviewLimit caps how many membership changes are listed before
// summarizing. A recipe re-derived against a much-changed library can
// differ by hundreds of rows, and the question being asked is how many,
// not each one.
var playlistReseedCmd = &cobra.Command{
	Use:   "reseed <name>",
	Short: "Re-derive a seeded playlist's membership and show what would change",
	Long: `Re-run a seeded playlist's recipe against the current library and show the
difference before writing anything.

A seeded playlist remembers the track it was seeded from and the title
tokens chosen at the time. This command matches those tokens against the
library now and reports what would be added and removed.

Nothing is applied unless you confirm. Membership never changes on its own,
so importing a file cannot silently alter a playlist you play on another
device.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)
		name := args[0]

		report, err := c.PlaylistService.ReseedPlan(ctx, name)
		if err != nil {
			// Both recipe failures have a remedy the user can act on, and
			// neither is worth a wrapped chain.
			var noRecipe *library.NoRecipeError
			var missingSeed *library.MissingSeedError
			if errors.As(err, &noRecipe) {
				return err
			}
			if errors.As(err, &missingSeed) {
				return fmt.Errorf("%s; re-create the playlist with --from-track to give it a new seed", err)
			}
			return fmt.Errorf("plan reseed for %q: %w", name, err)
		}

		// The plan already reported what it found, including "Nothing to
		// change". Returning here keeps the commonest outcome to one
		// sentence rather than two saying the same thing.
		printReseedPlan(cmd, name, report)

		if !report.Changed() {
			return nil
		}

		// --accept is the explicit opt-in for a non-interactive session;
		// --yes is accepted so the destructive-confirmation path is shared
		// with playlist delete rather than inventing a second convention.
		accept, _ := cmd.Flags().GetBool(flagAccept)
		yes, _ := cmd.Flags().GetBool(flagYes)

		if !accept && !yes {
			ok, err := confirmDestructive(cmd, fmt.Sprintf("Apply these %s to playlist %q?",
				plural(len(report.Changes), "change", "changes"), name))
			if err != nil {
				return err
			}
			if !ok {
				cmd.Println("Cancelled. The playlist is unchanged.")
				return nil
			}
		}

		if err := c.PlaylistService.Reseed(ctx, name, report); err != nil {
			return fmt.Errorf("apply reseed to %q: %w", name, err)
		}

		cmd.Printf("\nApplied: %d added, %d removed, %d unchanged.\n",
			report.Added(), report.Removed(), report.Unchanged)
		cmd.Printf("Re-export to update the .m3u: yci playlist export %s\n", name)

		return nil
	},
}

func printReseedPlan(cmd *cobra.Command, name string, report library.ReseedReport) {
	cmd.Printf("Playlist %q  (seed: %s)\n", name, truncateCell(report.SeedTitle, cellWidthSeed))
	cmd.Printf("Recipe: %s\n", library.FormatTokens(report.Recipe, report.MatchMode))
	if len(report.Excludes) > 0 {
		cmd.Printf("Excluded: %s, kept out on every re-derivation\n",
			plural(len(report.Excludes), "track", "tracks"))
	}
	if report.MatchMode == library.MatchAny {
		cmd.Printf("Matching: ANY token — wider than the AND default\n")
	}
	cmd.Printf("Recipe matches %s now; playlist holds %s, of which %s already match.\n\n",
		plural(report.Matched, "track", "tracks"),
		plural(report.Current, "track", "tracks"),
		plural(report.Unchanged, "track", "tracks"))

	if !report.Changed() {
		// Said explicitly because this is the commonest outcome, and the
		// counts alone read as an invitation to look for a diff that is not
		// there.
		cmd.Println("Nothing to change; the recipe already matches the membership.")
		return
	}

	table := NewTable(cmd.OutOrStdout(), "CHANGE", "TRACK_ID", "TITLE")

	shown := 0
	// Removals print first: they are the destructive half and the reason
	// this command exists.
	for _, change := range report.Changes {
		if change.Add {
			continue
		}
		if shown == changePreviewLimit {
			break
		}
		table.Row("remove", shortTrackID(change.Track.ID), truncateCell(change.Track.Title, cellWidthTitle))
		shown++
	}

	for _, change := range report.Changes {
		if !change.Add || shown == changePreviewLimit {
			continue
		}
		table.Row("add", shortTrackID(change.Track.ID), truncateCell(change.Track.Title, cellWidthTitle))
		shown++
	}

	table.Flush()

	if remaining := len(report.Changes) - shown; remaining > 0 {
		cmd.Printf("... and %s more.\n", pluralOf(remaining, "changes"))
	}
}

func init() {
	playlistReseedCmd.Flags().Bool(flagAccept, false, "Apply the changes without prompting (non-interactive)")
	playlistReseedCmd.Flags().Bool(flagYes, false, "Skip confirmation prompt (required in non-interactive sessions)")
}
