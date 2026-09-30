package cli

import (
	"strconv"

	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/library"
)

var playlistCmd = &cobra.Command{
	Use:   "playlist",
	Short: "Manage playlists and export them as .m3u files",
	Args:  cobra.NoArgs,
}

var playlistCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new playlist, optionally seeded from an existing track",
	Long: `Create an empty playlist, or seed one from a track you already have.

Seeding derives a fragment from the seed's title, shows which tracks it
matches, and adds the accepted set as a static playlist. The fragment is a
token set: it matches a track only when every chosen token is present, so
spacing differences do not matter.

In an interactive session you pick which title tokens to match on, then
deselect any matches that do not belong. Elsewhere, pass --accept-all to take
the derived fragment, or --fragment to state one explicitly.

Exclusions are stored in the recipe, so --exclude survives
"playlist reseed" instead of being undone by the next re-derivation.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		seedInput, _ := cmd.Flags().GetString(flagFromTrack)
		fragmentFlag, _ := cmd.Flags().GetString(flagFragment)
		acceptAll, _ := cmd.Flags().GetBool(flagAcceptAll)
		match, _ := cmd.Flags().GetString(flagMatch)
		exclude, _ := cmd.Flags().GetStringSlice(flagExclude)

		if seedInput != "" {
			return seedPlaylist(cmd, args[0], seedInput, fragmentFlag, acceptAll, library.ParseMatchMode(match), exclude)
		}

		c := Container(cmd.Context())

		p, err := c.PlaylistService.Create(cmd.Context(), args[0])
		if err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrPlaylistAlreadyExists: fmt.Sprintf("playlist %q already exists", args[0]),
			})
		}

		cmd.Printf("Created playlist: %s\n", p.Name)
		return nil
	},
}

func init() {
	playlistCreateCmd.Flags().String(flagFromTrack, "", "Seed the playlist from this track ID (prefix allowed)")
	playlistCreateCmd.Flags().String(flagFragment, "", "Match on these title tokens instead of choosing them")
	playlistCreateCmd.Flags().Bool(flagAcceptAll, false, "Use the derived fragment without prompting (non-interactive)")
	playlistCreateCmd.Flags().String(flagMatch, "all", `How tokens combine: "all" requires every token, "any" requires at least one`)
	playlistCreateCmd.Flags().StringSlice(flagExclude, nil, "Leave these track IDs (prefix allowed) out of the derived set")
	playlistCreateCmd.Flags().Bool(flagYes, false, "Skip confirmation prompt (required in non-interactive sessions)")
}

var playlistListCmd = &cobra.Command{
	Use:   "list",
	Short: "List playlists",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := Container(cmd.Context())

		playlists, err := c.PlaylistService.All(cmd.Context())
		if err != nil {
			return fmt.Errorf("list playlists: %w", err)
		}
		if jsonRequested(cmd) {
			out := make([]jsonPlaylist, 0, len(playlists))
			for _, p := range playlists {
				out = append(out, toJSONPlaylist(p))
			}

			return writeJSON(cmd.OutOrStdout(), out)
		}

		if len(playlists) == 0 {
			cmd.Println("No playlists yet. Create one with: yci playlist create <name>")
			return nil
		}

		table := NewTable(cmd.OutOrStdout(), "NAME")
		for _, p := range playlists {
			table.Row(p.Name)
		}
		table.Flush()

		return nil
	},
}

// formatDuration renders seconds as H:MM:SS or M:SS; 0 (unknown) stays 0:00.
// unknownDuration is what a duration column shows when the length is not
// known.
//
// It is deliberately not "0:00". A track ffprobe could not read is not a
// zero-length track, and the two are different facts about a library: one is
// a gap in what yci knows, the other is a broken file. Rendering both as
// "0:00" made the most common state indistinguishable from a real problem,
// and the rows that most needed attention were the ones that looked least
// wrong.
const unknownDuration = "—"

func formatDuration(secs int) string {
	if secs <= 0 {
		return unknownDuration
	}

	h := secs / 3600
	m := (secs % 3600) / 60
	s := secs % 60

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}

	return fmt.Sprintf("%d:%02d", m, s)
}

var playlistShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Print a playlist's tracks in playback order",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := Container(cmd.Context())

		entries, err := c.PlaylistService.Entries(cmd.Context(), args[0])
		if err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrPlaylistNotFound: fmt.Sprintf("playlist %q does not exist", args[0]),
			})
		}
		if jsonRequested(cmd) {
			out := make([]jsonPlaylistEntry, 0, len(entries))
			for _, e := range entries {
				out = append(out, jsonPlaylistEntry{
					Position: e.Position,
					Track:    string(e.Track.ID),
					Title:    e.Track.Title,
					Duration: e.Duration,
				})
			}

			return writeJSON(cmd.OutOrStdout(), out)
		}

		if len(entries) == 0 {
			cmd.Printf("Playlist %q is empty.\n", args[0])
			return nil
		}

		table := NewTable(cmd.OutOrStdout(), "#", "TRACK_ID", "TITLE", "LENGTH")

		for i, e := range entries {
			// The row number is the human handle for "remove track 4"; the
			// track ID is printed alongside it because renumbering after a
			// removal is exactly why positions should not be the only
			// identifier given.
			table.Row(
				strconv.Itoa(i+1),
				shortTrackID(e.Track.ID),
				truncateCell(e.Track.Title, cellWidthTitleWide),
				formatDuration(e.Duration),
			)
		}

		table.Flush()
		return nil
	},
}

var playlistAddCmd = &cobra.Command{
	Use:   "add <name> <track-id>...",
	Short: "Append one or more tracks to a playlist",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)
		name := args[0]

		for _, input := range args[1:] {
			trackID, err := resolveTrackID(ctx, c, strings.TrimSpace(input))
			if err != nil {
				return err
			}

			if err := c.PlaylistService.AddTrack(ctx, name, trackID); err != nil {
				return mapSentinels(err, map[error]string{
					library.ErrPlaylistNotFound:       fmt.Sprintf("playlist %q does not exist", name),
					library.ErrTrackNotFound:          fmt.Sprintf("track %q does not exist", input),
					library.ErrTrackAlreadyInPlaylist: fmt.Sprintf("track %q is already in %q", trackID, name),
				})
			}

			cmd.Printf("Added to %s: %s\n", name, trackID)
		}

		return nil
	},
}

var playlistRemoveCmd = &cobra.Command{
	Use:   "remove <name> <track-id>...",
	Short: "Remove one or more tracks from a playlist",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)
		name := args[0]

		for _, input := range args[1:] {
			trackID, err := resolveTrackID(ctx, c, strings.TrimSpace(input))
			if err != nil {
				return err
			}

			if err := c.PlaylistService.RemoveTrack(ctx, name, trackID); err != nil {
				return mapSentinels(err, map[error]string{
					library.ErrPlaylistNotFound:   fmt.Sprintf("playlist %q does not exist", name),
					library.ErrTrackNotFound:      fmt.Sprintf("track %q does not exist", input),
					library.ErrTrackNotInPlaylist: fmt.Sprintf("track %q is not in %q", trackID, name),
				})
			}

			cmd.Printf("Removed from %s: %s\n", name, trackID)
		}

		return nil
	},
}

var playlistDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a playlist (exported .m3u files are left on disk)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		confirmed, err := confirmDestructive(cmd, fmt.Sprintf("Delete playlist %q?", args[0]))
		if err != nil {
			return err
		}
		if !confirmed {
			cmd.Println("Aborted. Nothing was deleted.")
			return nil
		}

		if err := c.PlaylistService.Delete(ctx, args[0]); err != nil {
			return mapSentinels(err, map[error]string{
				library.ErrPlaylistNotFound: fmt.Sprintf("playlist %q does not exist", args[0]),
			})
		}

		cmd.Printf("Deleted playlist: %s\n", args[0])
		return nil
	},
}

var playlistExportCmd = &cobra.Command{
	Use:   "export [name]",
	Short: "Render playlists to .m3u files under the library (all playlists when no name given)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		if len(args) == 1 {
			path, err := c.PlaylistService.Export(ctx, args[0])
			if err != nil {
				return mapSentinels(err, map[error]string{
					library.ErrPlaylistNotFound: fmt.Sprintf("playlist %q does not exist", args[0]),
				})
			}

			cmd.Printf("Exported: %s\n", path)
			return nil
		}

		playlists, err := c.PlaylistService.All(ctx)
		if err != nil {
			return fmt.Errorf("list playlists: %w", err)
		}
		if len(playlists) == 0 {
			cmd.Println("No playlists to export.")
			return nil
		}

		// Distinct names can collide after slugification; detect instead
		// of silently overwriting.
		seenSlugs := make(map[string]string, len(playlists))
		exported := 0

		for _, p := range playlists {
			slug := library.SanitizeForExport(p.Name)
			if owner, clash := seenSlugs[slug]; clash {
				cmd.Printf("Skipped %q: export file name clashes with %q (%s.m3u)\n", p.Name, owner, slug)
				continue
			}
			seenSlugs[slug] = p.Name

			if _, err := c.PlaylistService.Export(ctx, p.Name); err != nil {
				return fmt.Errorf("export %q: %w", p.Name, err)
			}

			exported++
		}

		cmd.Printf("Exported %s to the library %s directory.\n",
			plural(exported, "playlist", "playlists"), library.ExportDir)
		return nil
	},
}

func init() {
	playlistCmd.AddCommand(
		playlistCreateCmd,
		playlistReseedCmd,
		playlistEditCmd,
		playlistShowCmd,
		playlistAddCmd,
		playlistRemoveCmd,
		playlistDeleteCmd,
		playlistListCmd,
		playlistExportCmd,
	)

	// Without this, playlist delete could never run non-interactively:
	// confirmDestructive requires --yes, and the flag used to live on the
	// top-level delete group that playlists never sat under.
	playlistDeleteCmd.Flags().Bool(flagYes, false, "Skip confirmation prompt (required in non-interactive sessions)")

	playlistListCmd.Flags().Bool(flagJSON, false, "Emit JSON instead of an aligned table, for scripts")
	playlistShowCmd.Flags().Bool(flagJSON, false, "Emit JSON instead of an aligned table, for scripts")
}
