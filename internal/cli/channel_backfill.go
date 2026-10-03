package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/library"
)

var channelBackfillCmd = &cobra.Command{
	Use:   "backfill",
	Short: "Recover YouTube channel IDs for links created before they were recorded",
	Long: `Ask YouTube which channel each linked artist publishes on, and store the ID.

Channel links used to be identified by display name alone, so a performer who
renamed their channel left every download unresolvable. The ID does not change
on rename, which makes it the durable identity.

One video per artist is queried, so this makes one network request per artist
rather than one per track. Links that cannot be determined without guessing are
reported and left alone: an artist with several channel links gives nothing to
resolve against, and picking one would file downloads under the wrong artist.

Names that no longer match what YouTube reports are corrected in the same pass.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := Container(cmd.Context())

		dryRun, _ := cmd.Flags().GetBool(flagDryRun)
		results, err := c.ChannelService.BackfillChannels(cmd.Context(), dryRun)
		if err != nil {
			return fmt.Errorf("backfill channel ids: %w", err)
		}

		reportBackfill(cmd, results, dryRun)
		return nil
	},
}

// reportBackfill renders the outcome, one row per link.
//
// Counts come first because the run can be long and the interesting question
// is what changed, not the full list.
func reportBackfill(cmd *cobra.Command, results []library.BackfillResult, dryRun bool) {
	counts := map[library.BackfillOutcome]int{}
	for _, r := range results {
		counts[r.Outcome]++
	}

	if len(results) == 0 {
		cmd.Println("No channel links to backfill.")
		return
	}

	verb := "Attached"
	if dryRun {
		verb = "Would attach"
	}

	table := NewTable(cmd.OutOrStdout(), "ARTIST_ID", "STORED_NAME", "CURRENT_NAME", "CHANNEL_ID", "RESULT")

	for _, r := range results {
		name := string(r.Found)
		if r.Outcome != library.BackfillAttached && r.Outcome != library.BackfillRenamed {
			name = "-"
		}

		table.Row(
			string(r.ArtistID),
			string(r.Stored),
			name,
			channelCell(r.Channel),
			resultLabel(r, verb),
		)
	}

	table.Flush()

	// The summary says "attached" in a dry run too, which contradicts the
	// table's "Would attach" beside the same rows. Reads as a write that
	// happened when none did.
	past := "attached"
	if dryRun {
		past = "would attach"
	}

	cmd.Printf("\n%d link(s): %d %s, %d renamed, %d already known, %d unresolved, %d failed.\n",
		len(results),
		counts[library.BackfillAttached],
		past,
		counts[library.BackfillRenamed],
		counts[library.BackfillAlreadyKnown],
		counts[library.BackfillUnresolved],
		counts[library.BackfillFailed],
	)
}

func channelCell(id library.ChannelID) string {
	if id == "" {
		return "-"
	}
	return string(id)
}

func resultLabel(r library.BackfillResult, verb string) string {
	switch r.Outcome {
	case library.BackfillAttached:
		return verb
	case library.BackfillRenamed:
		return "renamed"
	case library.BackfillAlreadyKnown:
		return "known"
	case library.BackfillUnresolved:
		return "unresolved"
	default:
		return "failed"
	}
}

func init() {
	channelBackfillCmd.Flags().Bool(flagDryRun, false, "Report what would change without writing")
	channelCmd.AddCommand(channelBackfillCmd)
}
