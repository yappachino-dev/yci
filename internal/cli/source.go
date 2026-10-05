package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/download"
	"github.com/yappachino-dev/yci/internal/library"
)

const ()

// savedSelector replays a stored channel selection instead of asking.
//
// Without it a poll would re-prompt for every playlist on every run, which is
// the manual work the command exists to remove. The refs are matched by ID so
// a performer renaming their channel does not silently end the selection.
type savedSelector struct {
	channels []download.ChannelRef
}

func (s savedSelector) SelectChannels(_ context.Context, _ []download.ChannelRef) ([]download.ChannelRef, error) {
	return s.channels, nil
}

// toRefs crosses the package boundary for the stored selection. Neither
// internal/library nor internal/download imports the other, so the conversion
// lives here, at the composition root that holds both services.
func toRefs(channels []library.SourceChannel) []download.ChannelRef {
	refs := make([]download.ChannelRef, 0, len(channels))
	for _, ch := range channels {
		refs = append(refs, download.ChannelRef{ID: ch.ID, Name: ch.Name})
	}

	return refs
}

var sourceCmd = &cobra.Command{
	Use:   "source",
	Short: "Watch YouTube playlists and pull their new videos",
	Long: `Track YouTube playlists you follow and pull videos they gain.

A watched playlist is not a playlist. A playlist is local membership you
curate; a source is a remote list that keeps changing on its own. Pulling
from one adds videos to the download queue and never touches playlist
membership, which stays exactly as you left it.

Each source remembers which channels you picked. Selection is matched on the
immutable channel ID, so a performer renaming their channel does not quietly
stop the playlist.`,
}

var sourceAddCmd = &cobra.Command{
	Use:   "add <url>",
	Short: "Watch a YouTube playlist",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		url := args[0]
		name, _ := cmd.Flags().GetString(flagName)

		parsed, err := download.ParseURL(url)
		if err != nil || parsed.Type == download.URLTypeUnknown {
			return fmt.Errorf("%q is not a YouTube playlist or channel URL", url)
		}

		// Fetched here rather than at pull time so the channel list can be
		// shown once and stored, rather than re-asked on every poll.
		output, err := c.DownloadService.FetchPlaylist(ctx, url)
		if err != nil {
			return fmt.Errorf("read playlist: %w", err)
		}

		if len(output.Channels) == 0 {
			return fmt.Errorf("%q offers no videos; nothing to watch", url)
		}

		selector := &EnqueueSelector{
			cmd: cmd,
			preset: func(ctx context.Context, offered []download.ChannelRef) ([]download.ChannelRef, error) {
				return preselectFor(ctx, c, cmd, offered)
			},
		}

		refs, err := selector.SelectChannels(ctx, output.Channels)
		if err != nil {
			if errors.Is(err, errNoChannelsSelected) {
				return errors.New("no channels selected; nothing to watch")
			}

			return err
		}

		channels := make([]library.SourceChannel, 0, len(refs))
		for _, ref := range refs {
			channels = append(channels, library.SourceChannel{ID: ref.ID, Name: ref.Name})
		}

		if name == "" {
			name = defaultSourceName(output, url)
		}

		if err := c.SourceService.Add(ctx, name, url, channels); err != nil {
			return err
		}

		cmd.Printf("Watching %q (%d channels). Pull new videos with: yci source pull\n",
			name, len(channels))

		return nil
	},
}

// defaultSourceName prefers the playlist's own title, falling back to the
// ID in the URL when the listing did not report one.
func defaultSourceName(output download.PlaylistMetadata, url string) string {
	if output.Title != "" {
		return output.Title
	}

	if parsed, err := download.ParseURL(url); err == nil && parsed.PlaylistID != "" {
		return parsed.PlaylistID
	}

	return "watched playlist"
}

var sourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List watched YouTube playlists",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listSources(cmd.Context(), cmd)
	},
}

func listSources(ctx context.Context, cmd *cobra.Command) error {
	c := Container(ctx)

	sources, err := c.SourceService.List(ctx)
	if err != nil {
		return err
	}

	if jsonRequested(cmd) {
		linked, err := c.ChannelService.LinkedChannels(ctx)
		if err != nil {
			return err
		}

		return writeJSON(cmd.OutOrStdout(), sourceListJSON(sources, linked))
	}

	if len(sources) == 0 {
		cmd.PrintErrln("No watched playlists. Add one with: yci source add <url>")
		return nil
	}

	linked, err := c.ChannelService.LinkedChannels(ctx)
	if err != nil {
		return err
	}

	hanging := 0

	table := NewTable(cmd.OutOrStdout(), "NAME", "LINKED", "CHANNELS", "URL")
	for _, src := range sources {
		linkedCount, unlinked := linkStatus(src.Channels, linked)
		hanging += unlinked

		table.Row(src.Name, fmt.Sprintf("%d/%d", linkedCount, len(src.Channels)),
			summarizeNames(src.ChannelNames(), 3), src.URL)
	}
	table.Flush()

	// A selected channel that was never attached to an artist delivers
	// nothing and says nothing on its own, so it is called out here
	// rather than left to be noticed as a playlist that went quiet.
	if hanging > 0 {
		cmd.PrintErrf("\n%d selected channel(s) are not linked to an artist. Downloads from them "+
			"will prompt to create one on import, or link one up front with yci channel create.\n", hanging)
	}

	return nil
}

func sourceListJSON(sources []library.Source, linked library.LinkedChannels) []sourceDTO {
	rows := make([]sourceDTO, 0, len(sources))
	for _, src := range sources {
		linkedCount, unlinked := linkStatus(src.Channels, linked)

		rows = append(rows, sourceDTO{
			Name:           src.Name,
			URL:            src.URL,
			Channels:       src.ChannelNames(),
			LinkedChannels: linkedCount,
			UnlinkedCount:  unlinked,
		})
	}

	return rows
}

// sourceDTO is the JSON contract for a watched playlist: full strings, stable
// names, and an array that is always present.
type sourceDTO struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Channels []string `json:"channels"`
	// LinkedChannels counts selections already attached to an artist.
	// UnlinkedCount is the remainder, which is what delivers nothing.
	LinkedChannels int `json:"linked_channels"`
	UnlinkedCount  int `json:"unlinked_count"`
}

var sourceUpdateCmd = &cobra.Command{
	Use:               "update <name>",
	ValidArgsFunction: sourceNameCompletion,
	Short:             "Re-pick which channels of a watched playlist are pulled",
	Args:              cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		name := args[0]

		src, err := c.SourceService.ByName(ctx, name)
		if err != nil {
			return err
		}

		output, err := c.DownloadService.FetchPlaylist(ctx, src.URL)
		if err != nil {
			return fmt.Errorf("read playlist: %w", err)
		}

		refs, err := (&EnqueueSelector{cmd: cmd, selected: toRefs(src.Channels)}).
			SelectChannels(ctx, output.Channels)
		if err != nil {
			if errors.Is(err, errNoChannelsSelected) {
				return errors.New("no channels selected; the stored selection was left unchanged")
			}

			return err
		}

		channels := make([]library.SourceChannel, 0, len(refs))
		for _, ref := range refs {
			channels = append(channels, library.SourceChannel{ID: ref.ID, Name: ref.Name})
		}

		if err := c.SourceService.SetChannels(ctx, name, channels); err != nil {
			return err
		}

		cmd.Printf("Updated %q: %d channels selected.\n", name, len(channels))

		return nil
	},
}

var sourceRemoveCmd = &cobra.Command{
	Use:               "remove <name>",
	ValidArgsFunction: sourceNameCompletion,
	Short:             "Stop watching a YouTube playlist",
	Args:              cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		if err := c.SourceService.Remove(ctx, args[0]); err != nil {
			return err
		}

		cmd.Printf("Stopped watching %q. Downloaded tracks and playlists were not touched.\n", args[0])

		return nil
	},
}

var sourcePullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Poll every watched playlist and queue its new videos",
	Long: `Poll every watched YouTube playlist and queue what is new.

Videos already in the library are skipped, as are ones already staged or
queued. A video you deleted from the library is skipped too: deletion is
recorded, so a playlist that still offers it does not download it again.

Nothing is removed. This only adds to the download queue, which you can
inspect with yci download queue.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		minWait, maxWait, err := backfillPacing(c.Config.DownloadDelay)
		if err != nil {
			return err
		}

		results, err := c.SourceService.Pull(ctx, library.PullParams{
			MinWait: minWait,
			MaxWait: maxWait,
			Enqueue: func(ctx context.Context, url string, channels []library.SourceChannel) (int, error) {
				res, err := c.DownloadService.Enqueue(ctx, download.EnqueueParams{
					URLs:       []string{url},
					Reporter:   newConsoleReporter(cmd),
					Selector:   savedSelector{channels: toRefs(channels)},
					Retirement: c.TrackJournal,
				})
				if err != nil {
					return 0, err
				}

				return res.Enqueued, nil
			},
		})
		if err != nil {
			return err
		}

		return reportPull(cmd, results)
	},
}

// reportPull prints one line per source, and keeps a failing source visible
// rather than folding it into a total: a playlist that went private is a
// thing to act on, not a number to average away.
func reportPull(cmd *cobra.Command, results []library.PullResult) error {
	if len(results) == 0 {
		cmd.PrintErrln("No watched playlists. Add one with: yci source add <url>")
		return nil
	}

	total := 0
	failed := 0

	for _, r := range results {
		if r.Err != nil {
			failed++
			cmd.Printf("%-30s  %v\n", r.Name, r.Err)
			continue
		}

		total += r.Enqueued
		cmd.Printf("%-30s  %d new\n", r.Name, r.Enqueued)
	}

	cmd.Println()

	switch {
	case failed == len(results):
		return fmt.Errorf("every watched playlist failed")
	case failed > 0:
		cmd.Printf("Queued %d new video(s); %d playlist(s) failed.\n", total, failed)
	default:
		cmd.Printf("Queued %d new video(s) from %d playlist(s).\n", total, len(results))
	}

	return nil
}

// summarizeNames joins names to one cell, counting the rest so a selection of
// thirty channels does not push the URL column off the terminal.
func summarizeNames(names []string, cap int) string {
	if len(names) <= cap {
		return strings.Join(names, ", ")
	}

	return fmt.Sprintf("%s +%d more", strings.Join(names[:cap], ", "), len(names)-cap)
}

func init() {
	sourceCmd.AddCommand(sourceAddCmd, sourceListCmd, sourceUpdateCmd, sourceRemoveCmd, sourcePullCmd)

	sourceAddCmd.Flags().String(flagName, "", "Name for this source (defaults to the playlist title)")
	sourceAddCmd.Flags().String(flagChannels, "", "Pre-select these channels by ID or name (comma-separated), on top of already-linked ones")
	sourceAddCmd.Flags().Bool(flagNoPreselect, false, "Start the picker with nothing selected instead of the already-linked channels")
	sourceListCmd.Flags().Bool(flagJSON, false, "Output as JSON")

}
