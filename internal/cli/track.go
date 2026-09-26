package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/library"
)

const (
	flagDryRunRetitle = "dry-run"
	flagForce         = "force"
)

// retitlePreviewLimit caps how many rows are printed before summarizing;
// a full backfill over a large library would otherwise bury the summary.
const retitlePreviewLimit = 20

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
				if i == retitlePreviewLimit {
					break
				}
				if !change.Retitled {
					continue
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					shortTrackID(change.ID),
					truncateCell(change.From, 40),
					truncateCell(change.To, 40),
					change.Version,
				)
			}

			w.Flush()
		}

		if report.Retitled > retitlePreviewLimit {
			cmd.Printf("... and %d more.\n", report.Retitled-retitlePreviewLimit)
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

	if len(tracks) == 0 {
		cmd.Println("No tracks in the library. Import some with `yci import staging`.")
		return nil
	}

	w := newTabWriter(cmd.OutOrStdout())
	fmt.Fprintln(w, "ID\tARTIST_ID\tSOURCE_ID\tTITLE")
	fmt.Fprintln(w, "--\t---------\t---------\t-----")

	for _, t := range tracks {
		shortID := t.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", shortID, t.ArtistID, t.SourceID, t.Title)
	}

	w.Flush()
	return nil
}

func shortTrackID(id library.TrackID) string {
	const keep = 8

	s := string(id)
	if len(s) <= keep {
		return s
	}

	return s[:keep]
}

func truncateCell(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= limit {
		return s
	}

	return string([]rune(s)[:limit-1]) + "…"
}

func init() {
	trackCmd.AddCommand(
		trackListCmd,
		trackRetitleCmd,
		trackDeleteCmd,
	)

	trackRetitleCmd.Flags().Bool(flagDryRunRetitle, false, "Report what would change without writing")

	trackDeleteCmd.Flags().String(flagID, "", "The UUID of the track to delete")
	trackDeleteCmd.Flags().Bool(flagYes, false, "Skip confirmation prompt (required in non-interactive sessions)")
	trackDeleteCmd.RegisterFlagCompletionFunc(flagID, trackIDCompletion)
}
