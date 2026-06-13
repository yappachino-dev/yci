package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"playlist-manager/internal/library"

	"github.com/spf13/cobra"
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List library entities (defaults to folders)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listFolders(cmd.Context())
	},
}

var lsFoldersCmd = &cobra.Command{
	Use:   "folders",
	Short: "List all folders in a visual tree hierarchy",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listFolders(cmd.Context())
	},
}

var lsArtistsCmd = &cobra.Command{
	Use:   "artists",
	Short: "List all managed artists",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listArtists(cmd.Context())
	},
}

var lsChannelsCmd = &cobra.Command{
	Use:   "channels",
	Short: "List all YouTube channel aliases linked to artists",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listChannels(cmd.Context())
	},
}

var lsTracksCmd = &cobra.Command{
	Use:   "tracks",
	Short: "List all tracks in the library",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listTracks(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(lsCmd)

	lsCmd.AddCommand(lsFoldersCmd)
	lsCmd.AddCommand(lsArtistsCmd)
	lsCmd.AddCommand(lsChannelsCmd)
	lsCmd.AddCommand(lsTracksCmd)
}

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
}

func listFolders(ctx context.Context) error {
	folders, err := appl.FolderService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch folders: %w", err)
	}

	w := newTabWriter()
	fmt.Fprintln(w, "ID\tNAME\tPARENT")
	fmt.Fprintln(w, "--\t----\t------")

	childrenOf := make(map[library.FolderID][]library.Folder)
	for _, f := range folders {
		childrenOf[f.ParentID] = append(childrenOf[f.ParentID], f)
	}

	var traverse func(parentID library.FolderID, prefix string)
	traverse = func(parentID library.FolderID, prefix string) {
		children := childrenOf[parentID]
		for i, child := range children {
			isLast := (i == len(children)-1)

			connector := "├─ "
			if isLast {
				connector = "└─ "
			}

			nameWithTree := prefix + connector + string(child.Name)
			fmt.Fprintf(w, "%s\t%s\t%s\n", child.ID, nameWithTree, child.ParentID)

			newPrefix := prefix + "│  "
			if isLast {
				newPrefix = prefix + "   "
			}

			traverse(child.ID, newPrefix)
		}
	}

	traverse(library.FolderID(library.RootID), "")

	w.Flush()
	fmt.Println()

	return nil
}

func listArtists(ctx context.Context) error {
	artists, err := appl.ArtistService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch artists: %w", err)
	}

	w := newTabWriter()
	fmt.Fprintln(w, "ID\tNAME\tFOLDER_ID")
	fmt.Fprintln(w, "--\t----\t---------")

	for _, a := range artists {
		fmt.Fprintf(w, "%s\t%s\t%s\n", a.ID, a.Name, a.FolderID)
	}

	w.Flush()
	fmt.Println()

	return nil
}

func listChannels(ctx context.Context) error {
	channels, err := appl.ChannelService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch channels: %w", err)
	}

	w := newTabWriter()
	fmt.Fprintln(w, "ID\tARTIST_ID\tCHANNEL_NAME")
	fmt.Fprintln(w, "--\t---------\t------------")

	for _, c := range channels {
		fmt.Fprintf(w, "%d\t%s\t%s\n", c.ID, c.ArtistID, c.Name)
	}

	w.Flush()
	fmt.Println()

	return nil
}

func listTracks(ctx context.Context) error {
	tracks, err := appl.TrackService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch tracks: %w", err)
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

	return nil
}
