package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/library"
)

const flagDryRunRetitle = "dry-run"

// retitlePreviewLimit caps how many rows are printed before summarizing;
// a full backfill over a large library would otherwise bury the summary.
const retitlePreviewLimit = 20

var retitleCmd = &cobra.Command{
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
	rootCmd.AddCommand(retitleCmd)
	retitleCmd.Flags().Bool(flagDryRunRetitle, false, "Report what would change without writing")
}
