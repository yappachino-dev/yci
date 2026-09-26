package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const flagDryRunRepair = "dry-run"

var repairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Replay pending and failed filesystem actions from the journal",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		failed, err := c.FileActions.Failed(ctx)
		if err != nil {
			return fmt.Errorf("list failed file actions: %w", err)
		}

		for _, entry := range failed {
			cmd.Printf("failed:  #%d %s %q -> %q: %s\n", entry.ID, entry.Op, entry.Src, entry.Dst.String, entry.Error.String)
		}

		pending, err := c.FileActions.Pending(ctx)
		if err != nil {
			return fmt.Errorf("list pending file actions: %w", err)
		}

		for _, entry := range pending {
			cmd.Printf("pending: #%d %s %q -> %q\n", entry.ID, entry.Op, entry.Src, entry.Dst.String)
		}

		dryRun, _ := cmd.Flags().GetBool(flagDryRunRepair)
		if dryRun {
			cmd.Println("Dry run: no changes made.")
			return nil
		}

		if len(failed) > 0 {
			cmd.Println()
			cmd.Println("Retrying failed actions...")

			if err := c.FileActions.ResetFailed(ctx); err != nil {
				return fmt.Errorf("requeue failed file actions: %w", err)
			}
		}

		if err := c.FileActions.ExecutePending(ctx); err != nil {
			return fmt.Errorf("replay journal: %w", err)
		}

		if err := c.FileActions.PurgeDone(ctx); err != nil {
			return fmt.Errorf("purge completed file actions: %w", err)
		}

		cmd.Println("Journal replayed successfully")
		return nil
	},
}

func init() {

	repairCmd.Flags().Bool(flagDryRunRepair, false, "List journal entries without replaying them")
}
