package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/yappachino-dev/yci/internal/library"

	"github.com/spf13/cobra"
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List library entities (defaults to folders)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listFolders(cmd.Context(), cmd)
	},
}

var lsFoldersCmd = &cobra.Command{
	Use:   "folders",
	Short: "List all folders in a visual tree hierarchy",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listFolders(cmd.Context(), cmd)
	},
}

var lsArtistsCmd = &cobra.Command{
	Use:   "artists",
	Short: "List all managed artists",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listArtists(cmd.Context(), cmd)
	},
}

var lsChannelsCmd = &cobra.Command{
	Use:   "channels",
	Short: "List all YouTube channel aliases linked to artists",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listChannels(cmd.Context(), cmd)
	},
}

var lsTracksCmd = &cobra.Command{
	Use:   "tracks",
	Short: "List all tracks in the library (IDs truncated; prefixes work with delete)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listTracks(cmd.Context(), cmd)
	},
}

var lsQueueCmd = &cobra.Command{
	Use:   "queue",
	Short: "List all queued for download tracks",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listQueue(cmd.Context(), cmd)
	},
}

func init() {
	rootCmd.AddCommand(lsCmd)

	lsCmd.AddCommand(lsFoldersCmd)
	lsCmd.AddCommand(lsArtistsCmd)
	lsCmd.AddCommand(lsChannelsCmd)
	lsCmd.AddCommand(lsTracksCmd)
	lsCmd.AddCommand(lsQueueCmd)
}

func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
}

func listFolders(ctx context.Context, cmd *cobra.Command) error {
	c := Container(ctx)

	folders, err := c.FolderService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch folders: %w", err)
	}

	// The seeded root row always exists; only real folders make a tree.
	userFolders := 0
	for _, f := range folders {
		if f.ID != library.FolderID(library.RootID) {
			userFolders++
		}
	}

	if userFolders == 0 {
		cmd.Println("No folders yet. Create one with `yci create folder`.")
		return nil
	}

	w := newTabWriter(cmd.OutOrStdout())
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
	return nil
}

func listArtists(ctx context.Context, cmd *cobra.Command) error {
	c := Container(ctx)

	artists, err := c.ArtistService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch artists: %w", err)
	}

	if len(artists) == 0 {
		cmd.Println("No artists yet. Create one with `yci create artist`.")
		return nil
	}

	w := newTabWriter(cmd.OutOrStdout())
	fmt.Fprintln(w, "ID\tNAME\tFOLDER_ID")
	fmt.Fprintln(w, "--\t----\t---------")

	for _, a := range artists {
		fmt.Fprintf(w, "%s\t%s\t%s\n", a.ID, a.Name, a.FolderID)
	}

	w.Flush()
	return nil
}

func listChannels(ctx context.Context, cmd *cobra.Command) error {
	c := Container(ctx)

	channels, err := c.ChannelService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch channels: %w", err)
	}

	if len(channels) == 0 {
		cmd.Println("No channels linked. Link one with `yci create channel`.")
		return nil
	}

	w := newTabWriter(cmd.OutOrStdout())
	fmt.Fprintln(w, "ID\tARTIST_ID\tCHANNEL_NAME")
	fmt.Fprintln(w, "--\t---------\t------------")

	for _, c := range channels {
		fmt.Fprintf(w, "%d\t%s\t%s\n", c.ID, c.ArtistID, c.Name)
	}

	w.Flush()
	return nil
}

func listTracks(ctx context.Context, cmd *cobra.Command) error {
	c := Container(ctx)

	tracks, err := c.TrackService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch tracks: %w", err)
	}

	if len(tracks) == 0 {
		cmd.Println("No tracks in the library. Import some with `yci import staging`.")
		return nil
	}

	w := newTabWriter(cmd.OutOrStdout())
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
	return nil
}

func listQueue(ctx context.Context, cmd *cobra.Command) error {
	c := Container(ctx)

	queue, err := c.DownloadService.All(ctx)
	if err != nil {
		return fmt.Errorf("fetch queue: %w", err)
	}

	if len(queue) == 0 {
		cmd.Println("Download queue is empty. Add items with `yci download` or `yci sync`.")
		return nil
	}

	w := newTabWriter(cmd.OutOrStdout())
	fmt.Fprintln(w, "ID\tSOURCE_ID\tSTATUS\tATTEMPTS\tERROR_MSG")
	fmt.Fprintln(w, "--\t---------\t------\t--------\t---------")

	for _, q := range queue {
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s\n", q.ID, q.SourceID, q.Status, q.Attempts, q.ErrorMsg)
	}

	w.Flush()
	return nil
}
