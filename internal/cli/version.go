package cli

import (
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Write current version of the app",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println(cmd.Root().Version)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
