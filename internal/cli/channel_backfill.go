package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/config"
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

A stored name that no longer matches what YouTube reports is reported as
renamed and left alone. Correcting it here would edit artist attribution
without knowing the link is right; import corrects the label once the ID has
matched, which is the proof.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := Container(cmd.Context())

		dryRun, _ := cmd.Flags().GetBool(flagDryRun)

		// Paced like a download: each lookup is a request to the same host
		// that rate-limits, and an unspaced loop is how that turns into a
		// 403 mid-run.
		minWait, maxWait, err := backfillPacing(c.Config.DownloadDelay)
		if err != nil {
			return err
		}

		reporter := newBackfillReporter(cmd)

		results, err := c.ChannelService.BackfillChannels(cmd.Context(), library.BackfillParams{
			DryRun:   dryRun,
			MinWait:  minWait,
			MaxWait:  maxWait,
			Reporter: reporter,
		})
		if err != nil {
			return fmt.Errorf("backfill channel ids: %w", err)
		}

		reportBackfill(cmd, results, dryRun)
		return nil
	},
}

// backfillPacing reads the shared min:max delay. A malformed value falls back
// to the built-in default rather than failing the command, because the pacing
// is a courtesy to the source and not the work the user asked for.
func backfillPacing(delay string) (time.Duration, time.Duration, error) {
	if delay == "" {
		delay = config.DefaultDelay
	}

	minStr, maxStr, err := config.ParseDelay(delay)
	if err != nil {
		return 0, 0, fmt.Errorf("backfill: %w", err)
	}

	minWait, err := time.ParseDuration(minStr + "s")
	if err != nil {
		return 0, 0, fmt.Errorf("backfill: %w", err)
	}

	maxWait, err := time.ParseDuration(maxStr + "s")
	if err != nil {
		return 0, 0, fmt.Errorf("backfill: %w", err)
	}

	return minWait, maxWait, nil
}

// consoleBackfillReporter narrates the run to stderr.
//
// Stderr because stdout is reserved for the table and for --json, matching the
// rule that data goes to stdout and advisory output does not.
type consoleBackfillReporter struct {
	cmd *cobra.Command
}

func newBackfillReporter(cmd *cobra.Command) *consoleBackfillReporter {
	return &consoleBackfillReporter{cmd: cmd}
}

func (r *consoleBackfillReporter) OnBackfillStart(artists, links int) {
	if artists == 0 {
		r.cmd.PrintErrln("[*] Every link already has a channel ID; nothing to look up.")
		return
	}

	r.cmd.PrintErrf("[*] Resolving %s over %s, one request per artist (this talks to YouTube).\n",
		plural(artists, "artist", "artists"), plural(links, "link", "links"))
}

func (r *consoleBackfillReporter) OnArtistStart(current, artists int, artistID library.ArtistID) {
	r.cmd.PrintErrf("[%d/%d] %s\n", current, artists, artistID)
}

func (r *consoleBackfillReporter) OnArtistResolved(artistID library.ArtistID, id library.ChannelID, name library.ChannelName) {
	r.cmd.PrintErrf("      %s -> %s (%s)\n", artistID, id, name)
}

func (r *consoleBackfillReporter) OnArtistUnresolved(artistID library.ArtistID, reason string) {
	r.cmd.PrintErrf("      %s -> unresolved: %s\n", artistID, reason)
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
