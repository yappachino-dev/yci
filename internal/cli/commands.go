package cli

import "github.com/spf13/cobra"

// Entity groups come first so related commands sit together, then the
// operations that act on the whole library, then the narrow escape hatches.
// Cobra sorts commands alphabetically unless this is turned off, which would
// scatter the groups and bury the rarely used ones.
func init() {
	cobra.EnableCommandSorting = false

	rootCmd.AddCommand(
		folderCmd,
		artistCmd,
		channelCmd,
		trackCmd,
		playlistCmd,
		configCmd,

		downloadCmd,
		importCmd,
		syncCmd,
		repairCmd,

		debugCmd,
		versionCmd,
	)
}
