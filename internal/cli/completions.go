package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func artistIDCompletion(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c, ok := ContainerOK(cmd.Context())
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	artists, err := c.ArtistService.All(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, a := range artists {
		if strings.HasPrefix(string(a.ID), toComplete) {
			comps = append(comps, fmt.Sprintf("%s\t%s", a.ID, a.Name))
		}
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}

func folderIDCompletion(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c, ok := ContainerOK(cmd.Context())
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	folders, err := c.FolderService.All(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, f := range folders {
		if strings.HasPrefix(string(f.ID), toComplete) {
			comps = append(comps, fmt.Sprintf("%s\t%s", f.ID, f.Name))
		}
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}

func trackIDCompletion(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c, ok := ContainerOK(cmd.Context())
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	tracks, err := c.TrackService.All(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, t := range tracks {
		if strings.HasPrefix(string(t.ID), toComplete) {
			comps = append(comps, fmt.Sprintf("%s\t%s", t.ID, t.Title))
		}
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}

func channelNameCompletion(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c, ok := ContainerOK(cmd.Context())
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	channels, err := c.ChannelService.All(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, c := range channels {
		if strings.HasPrefix(string(c.Name), toComplete) {
			comps = append(comps, fmt.Sprintf("%s\tLinked to: %s", c.Name, c.ArtistID))
		}
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}

func playlistNameCompletion(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c, ok := ContainerOK(cmd.Context())
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	playlists, err := c.PlaylistService.All(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, p := range playlists {
		if strings.HasPrefix(p.Name, toComplete) {
			comps = append(comps, p.Name)
		}
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}

// playlistOrTrackCompletion completes the playlist name first and track IDs
// after it, because `playlist add` takes one of each in that order.
func playlistOrTrackCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return playlistNameCompletion(cmd, args, toComplete)
	}

	return trackIDCompletion(cmd, args, toComplete)
}

func sourceNameCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Only the first positional names an existing source. In
	// `source rename <current> <new>` the second is a name the user is
	// choosing, so completing it would offer the names it cannot take.
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	c, ok := ContainerOK(cmd.Context())
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	sources, err := c.SourceService.List(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, s := range sources {
		if strings.HasPrefix(s.Name, toComplete) {
			comps = append(comps, fmt.Sprintf("%s\t%d channels", s.Name, len(s.Channels)))
		}
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}

// queueIDCompletion offers row IDs with their failure state attached, since
// `retry` takes either and the state is what a user is scanning the queue for.
func queueIDCompletion(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c, ok := ContainerOK(cmd.Context())
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	entries, err := c.DownloadService.All(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var comps []string
	for _, e := range entries {
		id := strconv.Itoa(e.ID)
		if !strings.HasPrefix(id, toComplete) {
			continue
		}

		comps = append(comps, fmt.Sprintf("%s\t%s %s", id, e.Status, e.SourceID))
	}

	return comps, cobra.ShellCompDirectiveNoFileComp
}
