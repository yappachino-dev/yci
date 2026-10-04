package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/fs"
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
}

// defaultTrackLimit caps `track list` so a large library does not scroll
// past unread. 0 is the opt-out: the footer always states the real total, so
// a shortened list is never mistaken for the whole library.
const defaultTrackLimit = 50

var trackReconcilePathsCmd = &cobra.Command{
	Use:   "reconcile-paths",
	Short: "Rewrite track paths whose bytes differ from the file on disk",
	Long: `Rewrite stored track paths that name a file the filesystem cannot find.

NFC and NFD encode the same visible name as different bytes, and ext4 compares
names byte for byte, so a track can store a path that will never open even
though the file is present. Only paths whose alternate normalization form is
provably on disk are rewritten; a genuinely absent file is reported and left
alone rather than guessed at.

Normally there is nothing to do: imports write the path and move the file in
one transaction, so the two agree byte for byte. This repairs libraries whose
files were placed by something else.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		dryRun, _ := cmd.Flags().GetBool(flagDryRunRetitle)

		report, err := c.TrackService.ReconcilePaths(ctx, dryRun)
		if err != nil {
			return fmt.Errorf("reconcile track paths: %w", err)
		}

		cmd.Printf("Checked %s.\n", plural(report.Checked, "track", "tracks"))

		if report.Missing > 0 {
			cmd.Printf("  %s absent under either Unicode form; left alone.\n",
				plural(report.Missing, "file is", "files are"))
		}

		if len(report.Repairs) == 0 {
			cmd.Println("Every stored path matches a file on disk.")

			return nil
		}

		// The two paths render identically on screen: the difference is
		// in the bytes. Reporting the form is both narrower and the only
		// part of the change the user could not see.
		table := NewTable(cmd.OutOrStdout(), "TRACK", "TITLE", "FORM")
		for _, r := range report.Repairs {
			table.Row(
				shortTrackID(r.ID),
				truncateCell(r.Title, cellWidthTitle),
				fs.Form(string(r.OldPath))+" -> "+fs.Form(string(r.NewPath)),
			)
		}

		table.Flush()

		if dryRun {
			cmd.Printf("\nDry run: %s would be rewritten. No changes made.\n",
				plural(len(report.Repairs), "path", "paths"))

			return nil
		}

		cmd.Printf("\nRewrote %s.\n", plural(len(report.Repairs), "path", "paths"))

		return nil
	},
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

		cmd.Printf("Scanned %s with no recorded duration.\n",
			plural(report.Scanned, "track", "tracks"))
		if report.Missing > 0 {
			cmd.Printf("  %s not on disk; left alone.\n",
				plural(report.Missing, "file is", "files are"))
		}
		if report.Failed > 0 {
			cmd.Printf("  %s not be probed.\n",
				plural(report.Failed, "file could", "files could"))
		}
		if report.Unchanged > 0 {
			cmd.Printf("  %s a zero length; left at 0.\n",
				plural(report.Unchanged, "file reported", "files reported"))
		}

		if dryRun {
			cmd.Printf("\nDry run: %s would record a duration. No changes made.\n",
				plural(report.Updated, "track", "tracks"))
			return nil
		}

		cmd.Printf("\nRecorded a duration for %s of %s.\n",
			plural(report.Updated, "track", "tracks"),
			plural(report.Scanned, "track", "tracks"))
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
			cmd.Printf("Nothing to interpret: all %s already have parsed titles.\n",
				plural(report.Scanned, "track", "tracks"))
			return nil
		}

		if report.Retitled > 0 {
			table := NewTable(cmd.OutOrStdout(), "TRACK_ID", "OLD_TITLE", "NEW_TITLE", "VERSION")

			shown := 0
			for _, change := range report.Changes {
				if shown == changePreviewLimit {
					break
				}
				if !change.Retitled {
					continue
				}
				table.Row(
					shortTrackID(change.ID),
					truncateCell(change.From, cellWidthDiff),
					truncateCell(change.To, cellWidthDiff),
					change.Version,
				)
				shown++
			}

			table.Flush()
		}

		if report.Retitled > changePreviewLimit {
			cmd.Printf("... and %s more.\n", pluralOf(report.Retitled-changePreviewLimit, "changes"))
		}
		cmd.Println()

		if dryRun {
			cmd.Printf("Dry run: %s of %s would be retitled, %s would record provenance. No changes made.\n",
				plural(report.Retitled, "track", "tracks"),
				plural(report.Scanned, "track", "tracks"),
				plural(report.Updated, "row", "rows"))
			return nil
		}

		cmd.Printf("Retitled %s of %s; %s now carry parsed titles.\n",
			plural(report.Retitled, "track", "tracks"),
			plural(report.Scanned, "track", "tracks"),
			plural(report.Updated, "row", "rows"))
		return nil
	},
}

var trackDeleteCmd = &cobra.Command{
	Use:               "delete [id]",
	ValidArgsFunction: trackIDCompletion,
	Short:             "Delete a track from the database AND remove the physical file",
	Args:              cobra.MaximumNArgs(1),
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

	limit, err := cmd.Flags().GetInt(flagLimit)
	if err != nil {
		return fmt.Errorf("read --%s: %w", flagLimit, err)
	}
	if limit < 0 {
		return fmt.Errorf("--%s must not be negative, got %d", flagLimit, limit)
	}

	// Counted before trimming, so the footer can say what was left out.
	total := len(tracks)
	if limit > 0 && limit < total {
		tracks = tracks[:limit]
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

	// No SOURCE_ID column: it is the YouTube video ID, which no command takes
	// as an argument, and at 11 cells it is what pushes the table past the
	// width of a normal terminal. It is still in --json.
	table := NewTable(cmd.OutOrStdout(), "ID", "ARTIST_ID", "TITLE", "LENGTH")

	for _, t := range tracks {
		table.Row(
			shortTrackID(t.ID),
			string(t.ArtistID),
			truncateCell(t.Title, cellWidthTitle),
			formatDuration(int(t.DurationSec)),
		)
	}

	table.Flush()

	// A row count is the only way the reader learns the list was cut short.
	// The total takes the plural in both sentences: "50 of 341 tracks" reads
	// as a count out of a count, not as two unrelated nouns.
	if limit > 0 && len(tracks) < total {
		cmd.Printf("\nShowing %d of %s.\n", len(tracks), pluralOf(total, "tracks"))
		return nil
	}

	cmd.Printf("\n%s.\n", plural(total, "track", "tracks"))
	return nil
}

func init() {
	trackCmd.AddCommand(
		trackListCmd,
		trackRetitleCmd,
		trackProbeDurationCmd,
		trackReconcilePathsCmd,
		trackDeleteCmd,
	)

	trackRetitleCmd.Flags().Bool(flagDryRunRetitle, false, "Report what would change without writing")
	trackProbeDurationCmd.Flags().Bool(flagDryRunRetitle, false, "Report what would change without writing")
	trackReconcilePathsCmd.Flags().Bool(flagDryRunRetitle, false, "Report what would change without writing")

	trackDeleteCmd.Flags().String(flagID, "", "The UUID of the track to delete")
	trackDeleteCmd.Flags().Bool(flagYes, false, "Skip confirmation prompt (required in non-interactive sessions)")
	trackDeleteCmd.RegisterFlagCompletionFunc(flagID, trackIDCompletion)

	trackListCmd.Flags().Bool(flagJSON, false, "Emit JSON instead of an aligned table, for scripts")
	trackListCmd.Flags().Int(flagLimit, defaultTrackLimit,
		"Maximum rows to show; 0 shows every track")
}
