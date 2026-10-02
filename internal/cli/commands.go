package cli

import "github.com/spf13/cobra"

// Command groups outline the tree by purpose rather than listing twelve
// unrelated verbs side by side. Order within each group is fixed (sorting is
// off), and the groups themselves are declared in reading order, so the
// first thing listed is the part of the library most often touched.
const (
	groupEntities = "entities"
	groupSetup    = "setup"
	groupFiles    = "files"
	groupDiag     = "diagnostics"
)

// asGroup turns a noun group into a runnable command that shows help.
//
// A command with no Run is treated by Cobra as a help command: it prints help
// and returns nil, so Args is never evaluated. Left that way, `Args: NoArgs`
// is dead code and an unknown subcommand exits 0 with help on stdout, which
// silently passes a typo to any script running under `set -e`.
func asGroup(cmd *cobra.Command) *cobra.Command {
	cmd.Args = cobra.NoArgs
	cmd.RunE = func(c *cobra.Command, _ []string) error { return c.Help() }
	return cmd
}

func init() {
	// Cobra sorts commands alphabetically unless this is turned off, which
	// would scatter related commands and bury the rarely used ones.
	cobra.EnableCommandSorting = false

	rootCmd.AddGroup(
		&cobra.Group{ID: groupEntities, Title: "Library entities:"},
		&cobra.Group{ID: groupSetup, Title: "Configuration:"},
		&cobra.Group{ID: groupFiles, Title: "Downloads and files:"},
		&cobra.Group{ID: groupDiag, Title: "Diagnostics:"},
	)

	folderCmd.GroupID = groupEntities
	artistCmd.GroupID = groupEntities
	channelCmd.GroupID = groupEntities
	trackCmd.GroupID = groupEntities
	playlistCmd.GroupID = groupEntities

	configCmd.GroupID = groupSetup

	downloadCmd.GroupID = groupFiles
	importCmd.GroupID = groupFiles
	syncCmd.GroupID = groupFiles
	repairCmd.GroupID = groupFiles

	debugCmd.GroupID = groupDiag
	versionCmd.GroupID = groupDiag

	// Cobra appends these two after all explicit commands; grouping them
	// with diagnostics keeps the entity lists uncluttered.
	rootCmd.SetHelpCommandGroupID(groupDiag)
	rootCmd.SetCompletionCommandGroupID(groupDiag)

	rootCmd.AddCommand(
		asGroup(folderCmd),
		asGroup(artistCmd),
		asGroup(channelCmd),
		asGroup(trackCmd),
		asGroup(playlistCmd),

		asGroup(configCmd),

		downloadCmd,
		asGroup(importCmd),
		syncCmd,
		repairCmd,

		asGroup(debugCmd),
		versionCmd,
	)
}
