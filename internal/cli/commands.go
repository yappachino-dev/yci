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
