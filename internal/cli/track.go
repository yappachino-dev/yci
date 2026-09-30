package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/library"
)

const (
	flagDryRunRetitle = "dry-run"
	flagForce         = "force"
)

// changePreviewLimit caps how many rows are printed before summarizing;
// a full backfill over a large library would otherwise bury the summary.
var trackCmd = &cobra.Command{
	Use:   "track",
	Short: "Manage tracks in the library",
	Args:  cobra.NoArgs,
}

var trackListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tracks in the library (IDs truncated; prefixes work with delete)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listTracks(cmd.Context(), cmd)
	},
}

var trackProbeDurationCmd = &cobra.Command{
	Use:   "probe-duration",
	Short: "Probe library files and record track durations (writes; use --dry-run to preview)",
	Long: `Probe each track whose duration is still 0 and record what the file reports.

Only rows that are already 0 are considered, so a second run is harmless and
a known-good duration is never overwritten. This is deliberately not part of
sync or audit: those walk the whole library, and probing is a subprocess per
file, so it belongs here on demand.

Requires ffprobe, which ships with the ffmpeg this tool already needs for
downloads. Without it nothing is scanned and the command reports that rather
than failing per file.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		dryRun, _ := cmd.Flags().GetBool(flagDryRunRetitle)

		report, err := c.TrackService.BackfillDuration(ctx, c.Prober, c.Config.LibraryDir, dryRun)
		if err != nil {
			return fmt.Errorf("probe track durations: %w", err)
		}

		if report.Unavailable {
			return fmt.Errorf("ffprobe is not installed or not in PATH; install ffmpeg to record durations")
		}

		if report.Scanned == 0 {
			cmd.Println("Nothing to probe: every track already has a duration.")
			return nil
		}

		cmd.Printf("Scanned %d track(s) with no recorded duration.\n", report.Scanned)
		if report.Missing > 0 {
			cmd.Printf("  %d file(s) are not on disk; left alone.\n", report.Missing)
		}
		if report.Failed > 0 {
			cmd.Printf("  %d file(s) could not be probed.\n", report.Failed)
		}
		if report.Unchanged > 0 {
			cmd.Printf("  %d file(s) reported a zero length; left at 0.\n", report.Unchanged)
		}

		if dryRun {
			cmd.Printf("\nDry run: %d track(s) would record a duration. No changes made.\n", report.Updated)
			return nil
		}

		cmd.Printf("\nRecorded a duration for %d of %d track(s).\n", report.Updated, report.Scanned)
		if report.Updated == 0 {
			cmd.Printf("Try: yci track list\n")
		}
		return nil
	},
}

var trackRetitleCmd = &cobra.Command{
	Use:   "retitle",
	Short: "Re-derive clean titles from stored raw titles (database only; no files are renamed)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		dryRun, _ := cmd.Flags().GetBool(flagDryRunRetitle)

		report, err := c.TrackService.Retitle(ctx, dryRun)
		if err != nil {
			return fmt.Errorf("retitle tracks: %w", err)
		}

		if report.Updated == 0 {
			cmd.Printf("Nothing to interpret: all %d track(s) already have parsed titles.\n", report.Scanned)
			return nil
		}

		if report.Retitled > 0 {
			w := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintln(w, "TRACK ID\tOLD TITLE\tNEW TITLE\tVERSION")

			for i, change := range report.Changes {
				if i == changePreviewLimit {
					break
				}
				if !change.Retitled {
					continue
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					shortTrackID(change.ID),
					truncateCell(change.From, cellWidthDiff),
					truncateCell(change.To, cellWidthDiff),
					change.Version,
				)
			}

			w.Flush()
		}

		if report.Retitled > changePreviewLimit {
			cmd.Printf("... and %d more.\n", report.Retitled-changePreviewLimit)
		}
		cmd.Println()

		if dryRun {
			cmd.Printf("Dry run: %d of %d track(s) would be retitled, %d row(s) would record provenance. No changes made.\n",
				report.Retitled, report.Scanned, report.Updated)
			return nil
		}

		cmd.Printf("Retitled %d of %d track(s); %d row(s) now carry parsed titles.\n",
			report.Retitled, report.Scanned, report.Updated)
		return nil
	},
}

var trackDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a track from the database AND remove the physical file",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		id, err := idFromArgsOrFlag(cmd, flagID, args)
		if err != nil {
			return err
		}
		if id == "" {
			return fmt.Errorf("provide the track ID (or an unambiguous prefix) as an argument or via --%s", flagID)
		}

		trackID, err := resolveTrackID(ctx, c, id)
		if err != nil {
			return err
		}

		prompt := fmt.Sprintf(
			"Delete track %q? Its file will be permanently removed from disk.",
			trackID,
		)

		confirmed, err := confirmDestructive(cmd, prompt)
		if err != nil {
			return err
		}
		if !confirmed {
			cmd.Println("Aborted. Nothing was deleted.")
			return nil
		}

		if err := c.TrackService.Delete(ctx, trackID); err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrTrackNotFound: fmt.Sprintf("track %q does not exist", id),
			})
		}

		cmd.Printf("Deleted track and physical file for ID: %s\n", trackID)
		return nil
	},
}

func listTracks(ctx context.Context, cmd *cobra.Command) error {
	c := Container(ctx)

	tracks, err := c.TrackService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch tracks: %w", err)
	}

	// JSON before the empty-state message: a consumer asking for data should
	// get an empty array, not a sentence explaining why there is none.
	if jsonRequested(cmd) {
		out := make([]jsonTrack, 0, len(tracks))
		for _, t := range tracks {
			out = append(out, toJSONTrack(t))
		}

		return writeJSON(cmd.OutOrStdout(), out)
	}

	if len(tracks) == 0 {
		cmd.Println("No tracks in the library. Import some with `yci import staging`.")
		return nil
	}

	w := newTabWriter(cmd.OutOrStdout())
	fmt.Fprintln(w, "ID\tARTIST_ID\tSOURCE_ID\tTITLE")
	fmt.Fprintln(w, "--\t---------\t---------\t-----")

	for _, t := range tracks {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			shortTrackID(t.ID), t.ArtistID, t.SourceID, truncateCell(t.Title, cellWidthTitleWide))
	}

	w.Flush()
	return nil
}

func init() {
	trackCmd.AddCommand(
		trackListCmd,
		trackRetitleCmd,
		trackProbeDurationCmd,
		trackDeleteCmd,
	)

	trackRetitleCmd.Flags().Bool(flagDryRunRetitle, false, "Report what would change without writing")
	trackProbeDurationCmd.Flags().Bool(flagDryRunRetitle, false, "Report what would change without writing")

	trackDeleteCmd.Flags().String(flagID, "", "The UUID of the track to delete")
	trackDeleteCmd.Flags().Bool(flagYes, false, "Skip confirmation prompt (required in non-interactive sessions)")
	trackDeleteCmd.RegisterFlagCompletionFunc(flagID, trackIDCompletion)

	trackListCmd.Flags().Bool(flagJSON, false, "Emit JSON instead of an aligned table, for scripts")
}
