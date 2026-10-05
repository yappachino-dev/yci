package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/download"
	"github.com/yappachino-dev/yci/internal/library"
)

func TestSourceCommandIsRegisteredAsAGroup(t *testing.T) {
	if sourceCmd.RunE == nil {
		t.Error("the source group is not runnable, so `source` alone would error")
	}

	want := []string{"add", "list", "update", "remove", "pull"}
	for _, sub := range want {
		found := false

		for _, c := range sourceCmd.Commands() {
			if c.Name() == sub {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("source has no %q subcommand", sub)
		}
	}
}

// TestToRefsCarriesTheChannelID: the form hands back names, so this is where
// the immutable ID is either recovered or silently lost.
func TestToRefsCarriesTheChannelID(t *testing.T) {
	refs := toRefs([]library.SourceChannel{
		{ID: "UCa", Name: "Alpha"},
		{ID: "UCb", Name: "Beta"},
	})

	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2", len(refs))
	}

	if refs[0].ID != "UCa" || refs[0].Name != "Alpha" {
		t.Errorf("ref 0 = %+v, want the stored ID and name", refs[0])
	}
}

// TestSavedSelectorReplaysTheStoredSelection: polling must not re-prompt.
func TestSavedSelectorReplaysTheStoredSelection(t *testing.T) {
	stored := toRefs([]library.SourceChannel{{ID: "UCa", Name: "Alpha"}})

	got, err := savedSelector{channels: stored}.
		SelectChannels(t.Context(), []download.ChannelRef{{ID: "UCzzz", Name: "Whatever"}})
	if err != nil {
		t.Fatalf("SelectChannels: %v", err)
	}

	if len(got) != 1 || got[0].ID != "UCa" {
		t.Errorf("returned %+v, want the stored selection regardless of what was offered", got)
	}
}

// TestDefaultSourceNamePrefersThePlaylistTitle: naming a source after the
// URL is worse than naming it after the playlist, when YouTube told us.
func TestDefaultSourceNamePrefersThePlaylistTitle(t *testing.T) {
	output := download.PlaylistMetadata{Title: "僕の歌ってみた"}

	got := defaultSourceName(output, "https://www.youtube.com/playlist?list=PL3y")
	if got != "僕の歌ってみた" {
		t.Errorf("name = %q, want the playlist title", got)
	}
}

func TestDefaultSourceNameFallsBackToThePlaylistID(t *testing.T) {
	got := defaultSourceName(download.PlaylistMetadata{}, "https://www.youtube.com/playlist?list=PL3y")
	if got != "PL3y" {
		t.Errorf("name = %q, want the playlist ID", got)
	}
}

// TestSummarizeNamesCountsWhatIsHidden: a selection of thirty channels must
// not push the URL column off the terminal, but the count must be honest.
func TestSummarizeNamesCountsWhatIsHidden(t *testing.T) {
	names := []string{"a", "b", "c", "d"}

	got := summarizeNames(names, 3)
	if !strings.Contains(got, "+1 more") {
		t.Errorf("summary = %q, want the hidden count", got)
	}

	if strings.Contains(summarizeNames([]string{"a", "b"}, 3), "more") {
		t.Error("a short selection was truncated")
	}
}

// TestReportPullKeepsFailuresVisible: one playlist going private is a thing
// to act on, not a number to fold into a total.
func TestReportPullKeepsFailuresVisible(t *testing.T) {
	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetOut(out)

	err := reportPull(cmd, []library.PullResult{
		{Name: "good", Enqueued: 3},
		{Name: "broken", Err: errString("playlist is private")},
	})
	if err != nil {
		t.Fatalf("reportPull: %v", err)
	}

	got := out.String()

	if !strings.Contains(got, "playlist is private") {
		t.Errorf("output hides the failure:\n%s", got)
	}

	if !strings.Contains(got, "1 playlist(s) failed") {
		t.Errorf("output does not report the partial failure:\n%s", got)
	}

	if !strings.Contains(got, "Queued 3 new video(s)") {
		t.Errorf("output omits what did succeed:\n%s", got)
	}
}

func TestReportPullFailsWhenEverySourceFails(t *testing.T) {
	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetOut(out)

	err := reportPull(cmd, []library.PullResult{{Name: "a", Err: errString("gone")}})
	if err == nil {
		t.Error("a run where every source failed reported success")
	}
}

func TestReportPullWithNoSourcesPointsAtAdd(t *testing.T) {
	cmd := &cobra.Command{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(errOut)

	if err := reportPull(cmd, nil); err != nil {
		t.Fatalf("reportPull: %v", err)
	}

	// Advisory, so stderr: stdout stays empty for anything piping the run.
	if !strings.Contains(errOut.String(), "source add") {
		t.Errorf("empty run does not say how to start: %q", errOut.String())
	}
}

type errString string

func (e errString) Error() string { return string(e) }
