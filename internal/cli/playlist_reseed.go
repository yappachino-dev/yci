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

		printReseedPlan(cmd, name, report)

		if !report.Changed() {
			cmd.Println("\nAlready up to date; nothing to do.")
			return nil
		}

		// --accept is the explicit opt-in for a non-interactive session;
		// --yes is accepted so the destructive-confirmation path is shared
		// with playlist delete rather than inventing a second convention.
		accept, _ := cmd.Flags().GetBool(flagAccept)
		yes, _ := cmd.Flags().GetBool(flagYes)

		if !accept && !yes {
			ok, err := confirmDestructive(cmd, fmt.Sprintf("Apply these %d change(s) to playlist %q?", len(report.Changes), name))
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
		cmd.Printf("Excluded: %d track(s), kept out on every re-derivation\n", len(report.Excludes))
	}
	if report.MatchMode == library.MatchAny {
		cmd.Printf("Matching: ANY token — wider than the AND default\n")
	}
	cmd.Printf("Matches %d track(s) now; playlist holds %d, of which %d already match.\n\n",
		report.Matched, report.Current, report.Unchanged)

	if !report.Changed() {
		// "Nothing to do" is the most common outcome of this command, and
		// it used to be reported only implicitly, by leaving out the change
		// table. Without the sentence the counts read as an invitation to go
		// looking for a diff that is not there.
		cmd.Println("Nothing to change; the recipe already matches the membership.")
		return
	}

	w := newTabWriter(cmd.OutOrStdout())
	fmt.Fprintln(w, "CHANGE\tID\tTITLE")

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
		fmt.Fprintf(w, "remove\t%s\t%s\n", shortTrackID(change.Track.ID), truncateCell(change.Track.Title, cellWidthTitle))
		shown++
	}

	for _, change := range report.Changes {
		if !change.Add || shown == changePreviewLimit {
			continue
		}
		fmt.Fprintf(w, "add\t%s\t%s\n", shortTrackID(change.Track.ID), truncateCell(change.Track.Title, cellWidthTitle))
		shown++
	}

	w.Flush()

	if remaining := len(report.Changes) - shown; remaining > 0 {
		cmd.Printf("... and %d more change(s).\n", remaining)
	}
}

func init() {
	playlistReseedCmd.Flags().Bool(flagAccept, false, "Apply the changes without prompting (non-interactive)")
	playlistReseedCmd.Flags().Bool(flagYes, false, "Skip confirmation prompt (required in non-interactive sessions)")
}
