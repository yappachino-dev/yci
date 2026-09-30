package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// TestConsoleReporterOutput pins the download progress lines.
//
// These are the only feedback a long download gives, so what they say and
// what they omit is the whole of the reporting contract: a failure has to be
// visible as a failure, and the index has to show where in the run it
// happened.
func TestConsoleReporterOutput(t *testing.T) {
	cmd, out := cmdWithOutput(t, nil)

	r := newConsoleReporter(cmd)

	r.OnHybridURL("https://x/y", false)
	r.OnHybridURL("https://x/y", true)
	r.OnPlaylistURL("https://x/p")
	r.OnEnqueueSkipped("abc123", "unrecognized URL")
	r.OnQueueStart(10)
	r.OnTrackStart(3, 10, "src-01")
	r.OnTrackSuccess("src-01")
	r.OnTrackFailure("src-02", errors.New("HTTP 403"))

	got := out.String()

	for _, want := range []string{
		"Extracting video from hybrid URL",
		"Extracting every video from hybrid-playlist URL",
		"Extracting every video from playlist URL",
		"Skipped abc123: unrecognized URL",
		"--- [Track 3 / 10] ---",
		"Target: src-01",
		"Success!",
		"Failed: HTTP 403",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reporter output is missing %q:\n%s", want, got)
		}
	}

	// The two hybrid cases must not be indistinguishable, since one takes
	// every video and the other takes one.
	if strings.Count(got, "hybrid") < 2 {
		t.Errorf("hybrid URL reporting does not distinguish the two cases:\n%s", got)
	}
}

// TestChannelPickerRequiresASelection covers the failure a user gets by
// submitting an empty picker, which is a mistake rather than an intent.
func TestChannelPickerRequiresASelection(t *testing.T) {
	session := newTTYCommand(t, nil)
	selector := &EnqueueSelector{cmd: session.cmd}

	wait := await(func() error {
		_, err := selector.SelectChannels(t.Context(), []string{"Alpha", "Beta"})
		return err
	})

	session.waitFor(t, "Alpha", 5*time.Second)

	// Submit with nothing selected.
	session.typeKeys(t, keyEnter)

	err := wait()
	if err == nil {
		t.Fatal("submitting an empty selection returned no error")
	}
	if !errors.Is(err, errNoChannelsSelected) {
		t.Errorf("error = %v, want errNoChannelsSelected", err)
	}
}

// TestChannelPickerReturnsTheSelectedChannels is the useful path: space
// toggles one row on and the chosen subset comes back.
func TestChannelPickerReturnsTheSelectedChannels(t *testing.T) {
	session := newTTYCommand(t, nil)
	selector := &EnqueueSelector{cmd: session.cmd}

	var got []string
	wait := await(func() error {
		var err error
		got, err = selector.SelectChannels(t.Context(), []string{"Alpha", "Beta"})
		return err
	})

	session.waitFor(t, "Alpha", 5*time.Second)

	session.typeKeys(t, keySpace)
	session.waitForOption(t, "Alpha", "focused-selected", 5*time.Second)

	session.typeKeys(t, keyEnter)

	if err := wait(); err != nil {
		t.Fatalf("SelectChannels: %v\n--- rendered ---\n%s", err, session.output())
	}

	if len(got) != 1 || got[0] != "Alpha" {
		t.Errorf("selected %v, want [Alpha]", got)
	}
}

func TestFlagChangedDistinguishesDefaultFromExplicit(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String(flagMatch, "all", "")

	if flagChanged(cmd, flagMatch) {
		t.Error("a flag left at its default reported as explicitly set")
	}

	if err := cmd.Flags().Set(flagMatch, "any"); err != nil {
		t.Fatal(err)
	}
	if !flagChanged(cmd, flagMatch) {
		t.Error("a flag set explicitly reported as unchanged")
	}
}
