package cli

import (
	"fmt"
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
