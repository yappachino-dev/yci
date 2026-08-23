package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var repairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Replay pending and failed filesystem actions from the journal",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		failed, err := c.FileActions.Failed(ctx)
		if err != nil {
			return err
		}

		for _, entry := range failed {
			fmt.Printf("failed:  #%d %s %q -> %q: %s\n", entry.ID, entry.Op, entry.Src, entry.Dst.String, entry.Error.String)
		}

		pending, err := c.FileActions.Pending(ctx)
		if err != nil {
			return err
		}

		for _, entry := range pending {
			fmt.Printf("pending: #%d %s %q -> %q\n", entry.ID, entry.Op, entry.Src, entry.Dst.String)
		}

		if len(failed) > 0 {
			fmt.Print("\nRetrying failed actions...\n")

			if err := c.FileActions.ResetFailed(ctx); err != nil {
				return err
			}
		}

		if err := c.FileActions.ExecutePending(ctx); err != nil {
			return fmt.Errorf("replay journal: %w", err)
		}

		if err := c.FileActions.PurgeDone(ctx); err != nil {
			return err
		}

		fmt.Print("Journal replayed successfully\n")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(repairCmd)
}
