package cli

import (
	"fmt"

	"github.com/yappachino-dev/yci/internal/db"

	"github.com/spf13/cobra"
)

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Collection of debug utilities",
	Args:  cobra.NoArgs,
}

var debugMigrationCmd = &cobra.Command{
	Use:   "migration",
	Short: "Report database schema version (migrations apply automatically on boot)",
	Args:  cobra.NoArgs,
}

var debugMigrationGetCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current and available database schema version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		current, err := db.GetCurrentVersion(ctx, c.DB)
		if err != nil {
			return fmt.Errorf("check current version: %w", err)
		}

		maxVersion, err := db.GetMaxVersion()
		if err != nil {
			return fmt.Errorf("check max version: %w", err)
		}

		cmd.Printf("Current schema version: %d\n", current)
		cmd.Printf("Maximum available version: %d\n", maxVersion)

		switch {
		case current < maxVersion:
			cmd.Printf("Database is %s behind; they apply on the next command.\n",
				plural(int(maxVersion-current), "version", "versions"))
		case current == maxVersion:
			cmd.Println("Database is up to date.")
		default:
			cmd.Printf("Database is AHEAD of this build (%d > %d); it was migrated by a newer yci.\n", current, maxVersion)
		}

		return nil
	},
}

func init() {
	debugCmd.AddCommand(debugMigrationCmd)

	debugMigrationCmd.AddCommand(debugMigrationGetCmd)
}
