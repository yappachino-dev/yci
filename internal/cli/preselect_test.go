package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/download"
	"github.com/yappachino-dev/yci/internal/dto"
	"github.com/yappachino-dev/yci/internal/library"
)

func linkedFixture() library.LinkedChannels {
	return library.LinkedChannels{
		IDs:   map[string]struct{}{"UCknown": {}},
		Names: map[string]struct{}{"Legacy Name": {}},
	}
}

// TestLinkStatusCountsWhatDeliversNothing: a selected channel with no linked
// artist is the thing that silently delivers nothing, so it must be counted
// rather than assumed covered.
func TestLinkStatusCountsWhatDeliversNothing(t *testing.T) {
	linked := linkedFixture()

	channels := []library.SourceChannel{
		{ID: "UCknown", Name: "Known"},
		{ID: "UCother", Name: "Unknown"},
		{Name: "Legacy Name"},
	}

	gotLinked, gotUnlinked := dto.LinkStatus(channels, linked)

	if gotLinked != 2 {
		t.Errorf("linked = %d, want 2", gotLinked)
	}

	if gotUnlinked != 1 {
		t.Errorf("unlinked = %d, want 1", gotUnlinked)
	}
}

// TestLinkStatusPrefersTheIDOverTheName: a renamed channel still has an ID,
// so a stale name must not read as unlinked.
func TestLinkStatusPrefersTheIDOverTheName(t *testing.T) {
	_, unlinked := dto.LinkStatus([]library.SourceChannel{{ID: "UCknown", Name: "Renamed"}}, linkedFixture())
	if unlinked != 0 {
		t.Error("a channel whose name drifted but whose ID matched was counted as unlinked")
	}
}

func channelFlags(t *testing.T, channels string, noPreselect bool) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.Flags().String(flagChannels, channels, "")
	cmd.Flags().Bool(flagNoPreselect, noPreselect, "")

	return cmd
}

// TestChannelListMatchesByIDOrName: a user usually has the ID, but a playlist
// prints the name, so both have to resolve.
func TestChannelListMatchesByIDOrName(t *testing.T) {
	list := parseChannelList(channelFlags(t, "UCwanted, By Name ,", false))

	offered := []download.ChannelRef{
		{ID: "UCwanted", Name: "Other"},
		{Name: "By Name"},
		{Name: "Unwanted"},
	}

	matched := 0

	for _, ref := range offered {
		if list.matches(ref) {
			matched++
		}
	}

	if matched != 2 {
		t.Errorf("matched %d of 3, want 2", matched)
	}
}

// TestUnmatchedChannelIsReported: a typo in --channels looks exactly like a
// working entry if it is silently dropped.
func TestUnmatchedChannelIsReported(t *testing.T) {
	offered := []download.ChannelRef{{ID: "UCwanted", Name: "Wanted"}}

	err := parseChannelList(channelFlags(t, "UCtypo", false)).reportUnmatched(offered)
	if err == nil {
		t.Fatal("a --channels entry the playlist does not offer was accepted silently")
	}

	if !strings.Contains(err.Error(), "UCtypo") {
		t.Errorf("error %q does not name the offending entry", err)
	}
}

func TestUnmatchedChannelListNamesEveryEntry(t *testing.T) {
	offered := []download.ChannelRef{{ID: "UCwanted", Name: "Wanted"}}

	err := parseChannelList(channelFlags(t, "one,two", false)).reportUnmatched(offered)
	if err == nil {
		t.Fatal("expected both entries to be reported")
	}

	for _, want := range []string{"one", "two"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q omits %q", err, want)
		}
	}
}

func TestMatchedChannelListReportsNothing(t *testing.T) {
	offered := []download.ChannelRef{{ID: "UCwanted", Name: "Wanted"}}

	if err := parseChannelList(channelFlags(t, "UCwanted", false)).reportUnmatched(offered); err != nil {
		t.Errorf("a present entry was reported as unmatched: %v", err)
	}
}

func TestEmptyChannelListIsNotAMistake(t *testing.T) {
	if got := parseChannelList(channelFlags(t, "  ,  ", false)); len(got) != 0 {
		t.Errorf("parse of separators only = %v, want empty", got)
	}
}
func presetFixture() []download.ChannelRef {
	return []download.ChannelRef{
		{ID: "UCknown", Name: "Known"},
		{ID: "UCfresh", Name: "Fresh"},
		{Name: "Legacy Name"},
		{ID: "UCother", Name: "Unwanted"},
	}
}

// TestPreselectTicksLinkedChannels: a watched playlist is mostly artists the
// user already has, and re-ticking them every poll is the manual work the
// command exists to remove.
func TestPreselectTicksLinkedChannels(t *testing.T) {
	got := preselect(presetFixture(), linkedFixture(), nil, false)

	want := []string{"Known", "Legacy Name"}
	if !sameNames(got, want) {
		t.Errorf("preselected %v, want %v", namesOf(got), want)
	}
}

func TestPreselectKeepsTheChannelID(t *testing.T) {
	for _, ref := range preselect(presetFixture(), linkedFixture(), nil, false) {
		if ref.Name == "Known" && ref.ID != "UCknown" {
			t.Errorf("preselected %+v, want the ID carried through for matching", ref)
		}
	}
}

// TestPreselectAddsExplicitChannels: a channel with no artist yet is the case
// the user most needs to pick, and it is not in the linked set.
func TestPreselectAddsExplicitChannels(t *testing.T) {
	got := preselect(presetFixture(), linkedFixture(), channelList{"UCother"}, false)

	if !containsName(got, "Unwanted") {
		t.Errorf("preselected %v, want the explicitly named channel", namesOf(got))
	}
}

func TestPreselectCombinesBothSources(t *testing.T) {
	got := preselect(presetFixture(), linkedFixture(), channelList{"UCother"}, false)

	for _, want := range []string{"Known", "Legacy Name", "Unwanted"} {
		if !containsName(got, want) {
			t.Errorf("preselected %v, want %q from linked or explicit", namesOf(got), want)
		}
	}

	if containsName(got, "Fresh") {
		t.Errorf("preselected %v, want the unrelated channel left alone", namesOf(got))
	}
}

// TestNoPreselectStartsFromNothing: the escape hatch has to actually escape.
func TestNoPreselectStartsFromNothing(t *testing.T) {
	if got := preselect(presetFixture(), linkedFixture(), nil, true); len(got) != 0 {
		t.Errorf("preselected %v with --no-preselect, want nothing", namesOf(got))
	}
}

// TestNoPreselectStillHonoursExplicitEntries: turning off the automatic
// behaviour must not throw away what the user asked for by name.
func TestNoPreselectStillHonoursExplicitEntries(t *testing.T) {
	got := preselect(presetFixture(), linkedFixture(), channelList{"UCother"}, true)

	if !sameNames(got, []string{"Unwanted"}) {
		t.Errorf("preselected %v, want only the explicit entry", namesOf(got))
	}
}

// TestPreselectIgnoresChannelsTheListingDoesNotOffer: a linked channel that
// is not in this playlist has no checkbox to tick.
func TestPreselectIgnoresChannelsTheListingDoesNotOffer(t *testing.T) {
	offered := []download.ChannelRef{{ID: "UCfresh", Name: "Fresh"}}

	if got := preselect(offered, linkedFixture(), nil, false); len(got) != 0 {
		t.Errorf("preselected %v from a playlist offering only Fresh", namesOf(got))
	}
}

func containsName(refs []download.ChannelRef, name string) bool {
	for _, r := range refs {
		if r.Name == name {
			return true
		}
	}

	return false
}

func sameNames(refs []download.ChannelRef, want []string) bool {
	got := namesOf(refs)
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
