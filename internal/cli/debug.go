package cli

import (
	"context"
	"fmt"
	"strconv"

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
	Short: "Database migration utilities (get, set, up, down)",
	Args:  cobra.NoArgs,
}

var debugMigrationGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get the current database schema version",
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

		fmt.Printf("Current schema version: %d\n", current)
		fmt.Printf("Maximum available version: %d\n", maxVersion)

		if current < maxVersion {
			fmt.Printf("Database is %d version(s) behind.\n", maxVersion-current)
		} else if current == maxVersion {
			fmt.Println("Database is up to date.")
		}
		return nil
	},
}

var debugMigrationSetCmd = &cobra.Command{
	Use:   "set [version]",
	Short: "Manually set the migration to a specific version",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		target, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid target version: %s (must be an integer)", args[0])
		}

		if target < 0 {
			return fmt.Errorf("invalid target version: %d (must be 0 or greater)", target)
		}

		return performMigration(ctx, target)
	},
}

var debugMigrationUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Advance the database schema by one version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		current, err := db.GetCurrentVersion(ctx, c.DB)
		if err != nil {
			return fmt.Errorf("check current version: %w", err)
		}

		return performMigration(ctx, current+1)
	},
}

var debugMigrationDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Rollback the database schema by one version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		current, err := db.GetCurrentVersion(ctx, c.DB)
		if err != nil {
			return fmt.Errorf("check current version: %w", err)
		}

		if current == 0 {
			fmt.Println("Schema is already at version 0. No changes made.")
			return nil
		}

		return performMigration(ctx, current-1)
	},
}

func performMigration(ctx context.Context, target int) error {
	c := Container(ctx)

	maxVersion, err := db.GetMaxVersion()
	if err != nil {
		return fmt.Errorf("check max version: %w", err)
	}

	if target > maxVersion {
		return fmt.Errorf("requested version %d exceeds the maximum available local migration (%d)", target, maxVersion)
	}

	current, err := db.GetCurrentVersion(ctx, c.DB)
	if err != nil {
		return fmt.Errorf("check current version: %w", err)
	}

	if current == target {
		fmt.Printf("Schema is already at version %d. No changes made.\n", target)
		return nil
	}

	if current > target {
		fmt.Printf("Rolling back database schema from version %d down to %d...\n", current, target)
	} else {
		fmt.Printf("Advancing database schema from version %d up to %d...\n", current, target)
	}

	if err := db.SetMigration(ctx, c.DB, current, target); err != nil {
		return fmt.Errorf("set schema version %d: %w", target, err)
	}

	fmt.Println("Migration successfully completed!")
	return nil
}

func init() {
	rootCmd.AddCommand(debugCmd)
	debugCmd.AddCommand(debugMigrationCmd)

	debugMigrationCmd.AddCommand(debugMigrationGetCmd)
	debugMigrationCmd.AddCommand(debugMigrationSetCmd)
	debugMigrationCmd.AddCommand(debugMigrationUpCmd)
	debugMigrationCmd.AddCommand(debugMigrationDownCmd)
}
