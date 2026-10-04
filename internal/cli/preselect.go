package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/download"
	"github.com/yappachino-dev/yci/internal/library"
)

const (
	flagChannels    = "channels"
	flagNoPreselect = "no-preselect"
	presetSeparator = ","
)

// preselectFor builds the starting selection for the channel picker.
//
// Channels already linked to an artist come first: a playlist you watch is
// mostly artists you have, and making the user re-tick them on every poll is
// the manual work the command exists to remove. Anything named by --channels
// is added on top, so a channel that is not linked yet can still be picked
// without hunting for it in the list.
//
// The result only contains channels the listing actually offers. A --channels
// entry that matches nothing is reported rather than silently dropped,
// because a typo there looks identical to "already selected".
func preselectFor(ctx context.Context, c *app.Container, cmd *cobra.Command, offered []download.ChannelRef) ([]download.ChannelRef, error) {
	linked, err := c.ChannelService.LinkedChannels(ctx)
	if err != nil {
		return nil, err
	}

	noPreselect, _ := cmd.Flags().GetBool(flagNoPreselect)
	explicit := parseChannelList(cmd)

	if err := explicit.reportUnmatched(offered); err != nil {
		return nil, err
	}

	return preselect(offered, linked, explicit, noPreselect), nil
}

// preselect is the decision, with the storage and flag reading left behind,
// so what actually gets ticked can be asserted without a container.
func preselect(offered []download.ChannelRef, linked library.LinkedChannels, explicit channelList, noPreselect bool) []download.ChannelRef {
	preset := make([]download.ChannelRef, 0, len(offered))

	for _, ref := range offered {
		if !noPreselect && linked.Contains(ref.ID, ref.Name) {
			preset = append(preset, ref)
			continue
		}

		if explicit.matches(ref) {
			preset = append(preset, ref)
		}
	}

	return preset
}

// channelList is the parsed value of --channels. Each entry may be a channel
// ID or a display name, because the ID is what a user usually has and the
// name is what a playlist prints.
type channelList []string

func parseChannelList(cmd *cobra.Command) channelList {
	raw, _ := cmd.Flags().GetString(flagChannels)
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, presetSeparator)
	out := make(channelList, 0, len(parts))

	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}

func (c channelList) matches(ref download.ChannelRef) bool {
	for _, want := range c {
		if want == ref.Name || (ref.ID != "" && want == ref.ID) {
			return true
		}
	}

	return false
}

// reportUnmatched catches a --channels entry the listing does not offer.
// Silently ignoring it produces exactly the same result as the entry working,
// which is the worst possible failure for a typo.
func (c channelList) reportUnmatched(offered []download.ChannelRef) error {
	var unmatched []string

	for _, want := range c {
		found := false

		for _, ref := range offered {
			if want == ref.Name || (ref.ID != "" && want == ref.ID) {
				found = true
				break
			}
		}

		if !found {
			unmatched = append(unmatched, want)
		}
	}

	if len(unmatched) == 0 {
		return nil
	}

	if len(unmatched) == 1 {
		return fmt.Errorf("--channels %q is not in this playlist", unmatched[0])
	}

	return fmt.Errorf("--channels entries not in this playlist: %s", strings.Join(unmatched, ", "))
}

// linkStatus counts a source's selection against the linked channels, so a
// channel that was picked but never attached to an artist is visible instead
// of hanging silently.
func linkStatus(channels []library.SourceChannel, linked library.LinkedChannels) (int, int) {
	linkedCount := 0

	for _, ch := range channels {
		if linked.Contains(ch.ID, ch.Name) {
			linkedCount++
		}
	}

	return linkedCount, len(channels) - linkedCount
}
