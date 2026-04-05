package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"playlist-manager/internal/library"

	"github.com/spf13/cobra"
)

// --- Command Definitions ---

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List library entities (defaults to folders)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		listFolders()
	},
}

var lsFoldersCmd = &cobra.Command{
	Use:   "folders",
	Short: "List all folders in a visual tree hierarchy",
	Run: func(cmd *cobra.Command, args []string) {
		listFolders()
	},
}

var lsArtistsCmd = &cobra.Command{
	Use:   "artists",
	Short: "List all managed artists",
	Run: func(cmd *cobra.Command, args []string) {
		listArtists()
	},
}

var lsChannelsCmd = &cobra.Command{
	Use:   "channels",
	Short: "List all YouTube channel aliases linked to artists",
	Run: func(cmd *cobra.Command, args []string) {
		listChannels()
	},
}

var lsTracksCmd = &cobra.Command{
	Use:   "tracks",
	Short: "List all tracks in the library",
	Run: func(cmd *cobra.Command, args []string) {
		listTracks()
	},
}

// --- Initialization ---

func init() {
	rootCmd.AddCommand(lsCmd)

	lsCmd.AddCommand(lsFoldersCmd)
	lsCmd.AddCommand(lsArtistsCmd)
	lsCmd.AddCommand(lsChannelsCmd)
	lsCmd.AddCommand(lsTracksCmd)
}

// --- Helper Functions ---

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
}

func listFolders() {
	folders, err := lib.GetFolders()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching folders: %v\n", err)
		return
	}

	w := newTabWriter()
	fmt.Fprintln(w, "ID\tNAME\tPARENT")
	fmt.Fprintln(w, "--\t----\t------")

	childrenOf := make(map[string][]library.Folder)
	for _, f := range folders {
		childrenOf[f.ParentID] = append(childrenOf[f.ParentID], f)
	}

	var traverse func(parentID string, prefix string)
	traverse = func(parentID string, prefix string) {
		children := childrenOf[parentID]
		for i, child := range children {
			isLast := (i == len(children)-1)

			connector := "├─ "
			if isLast {
				connector = "└─ "
			}

			nameWithTree := prefix + connector + child.DisplayName
			fmt.Fprintf(w, "%s\t%s\t%s\n", child.ID, nameWithTree, child.ParentID)

			newPrefix := prefix + "│  "
			if isLast {
				newPrefix = prefix + "   "
			}

			traverse(child.ID, newPrefix)
		}
	}

	traverse(library.RootID, "")

	w.Flush()
	fmt.Println()
}

func listArtists() {
	artists, err := lib.GetArtists()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching artists: %v\n", err)
		return
	}

	w := newTabWriter()
	fmt.Fprintln(w, "ID\tNAME\tFOLDER_ID")
	fmt.Fprintln(w, "--\t----\t---------")

	for _, a := range artists {
		fmt.Fprintf(w, "%s\t%s\t%s\n", a.ID, a.Artist, a.FolderID)
	}

	w.Flush()
	fmt.Println()
}

func listChannels() {
	channels, err := lib.GetChannels()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching channels: %v\n", err)
		return
	}

	w := newTabWriter()
	fmt.Fprintln(w, "ID\tARTIST_ID\tCHANNEL_NAME")
	fmt.Fprintln(w, "--\t---------\t------------")

	for _, c := range channels {
		fmt.Fprintf(w, "%d\t%s\t%s\n", c.ID, c.ArtistID, c.ChannelName)
	}

	w.Flush()
	fmt.Println()
}

func listTracks() {
	tracks, err := lib.GetAllTracks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching tracks: %v\n", err)
		return
	}

	w := newTabWriter()
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
	fmt.Println()
}
