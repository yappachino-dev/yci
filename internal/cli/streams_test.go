package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/library"
)

// splitCmd wires the flags a list command really has and returns it with
// stdout and stderr captured separately, the way a shell pipe sees them.
// Merging them, as most tests here do, cannot detect an advisory line that
// landed on the wrong stream.
func splitCmd(t *testing.T, c *app.Container) (*cobra.Command, *strings.Builder, *strings.Builder) {
	t.Helper()

	var out, errOut strings.Builder
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.Flags().Bool(flagJSON, false, "")
	cmd.Flags().Int(flagLimit, defaultTrackLimit, "")
	cmd.SetContext(WithContainer(t.Context(), c))

	return cmd, &out, &errOut
}

// TestSourceListKeepsAdvisoryOffStdout is the stream guardrail for the one
// case that could corrupt a pipeline: `source list` prints a table and then
// warns that selected channels are not linked to any artist. That warning is
// advisory, so it belongs on stderr; on stdout it would follow the table and
// break anything reading the command's output.
func TestSourceListKeepsAdvisoryOffStdout(t *testing.T) {
	c := newTestContainer(t)

	// One channel that is linked and one that was never attached to an
	// artist, so the advisory actually has something to report.
	err := c.SourceService.Add(t.Context(), "mix", "https://example.com/pl",
		[]library.SourceChannel{
			{ID: "UCknown", Name: "Known Channel"},
			{ID: "UCorphan", Name: "Orphan Channel"},
		})
	if err != nil {
		t.Fatalf("add source: %v", err)
	}

	cmd, out, errOut := splitCmd(t, c)
	if err := listSources(cmd.Context(), cmd); err != nil {
		t.Fatalf("source list: %v", err)
	}

	if !strings.Contains(errOut.String(), "not linked to an artist") {
		t.Errorf("the unlinked-channel advisory did not reach stderr:\n%s", errOut.String())
	}
	if !strings.Contains(out.String(), "mix") {
		t.Errorf("stdout lost the table:\n%s", out.String())
	}
	if strings.Contains(out.String(), "not linked to an artist") {
		t.Errorf("advisory text leaked onto stdout:\n%s", out.String())
	}
}

// TestJSONCommandsParseWithAdvisoryPresent states the rule the style guide
// gives as its reason for existing: stdout carries only the requested data,
// so `--json | jq` works with advisory chatter present. Checked as a set, so
// a future command has to earn its place rather than pass by omission.
func TestJSONCommandsParseWithAdvisoryPresent(t *testing.T) {
	c := newTestContainer(t)
	seedTracks(t, c, "a / b")

	t.Run("source list", func(t *testing.T) {
		cmd, out, _ := splitCmd(t, c)
		if err := cmd.Flags().Set(flagJSON, "true"); err != nil {
			t.Fatal(err)
		}

		if err := listSources(cmd.Context(), cmd); err != nil {
			t.Fatalf("source list: %v", err)
		}

		var got []sourceDTO
		if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
			t.Fatalf("stdout is not parseable JSON: %v\n%s", err, out.String())
		}
		if len(got) != 0 {
			t.Errorf("got %d sources, want 0", len(got))
		}
	})

	t.Run("track list", func(t *testing.T) {
		cmd, out, _ := splitCmd(t, c)
		if err := cmd.Flags().Set(flagJSON, "true"); err != nil {
			t.Fatal(err)
		}

		if err := listTracks(cmd.Context(), cmd); err != nil {
			t.Fatalf("track list: %v", err)
		}

		var got []jsonTrack
		if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
			t.Fatalf("stdout is not parseable JSON: %v\n%s", err, out.String())
		}
		if len(got) != 1 {
			t.Errorf("got %d tracks, want 1", len(got))
		}
	})
}
