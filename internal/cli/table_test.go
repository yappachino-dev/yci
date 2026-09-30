package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/library"
)

// TestTableUsesOneDialect is the regression test for three table styles in one
// CLI.
//
// `track list` wrote a row of dashes under its header, `playlist show` wrote
// one dash per column, and `track retitle` and the playlist seed and reseed
// tables wrote no underline at all. Moving between commands meant reading a
// different-looking table each time, and comparing two lists was harder than
// it needed to be.
func TestTableUsesOneDialect(t *testing.T) {
	var out strings.Builder

	table := NewTable(&out, "ID", "ARTIST_ID", "TITLE")
	table.Row("abc", "someone", "A Song")
	table.Row("def", "", "Another")
	table.Flush()

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected a header, a rule and two rows, got %d lines:\n%s", len(lines), out.String())
	}

	// Upper-case headers, tab separated in the source.
	if !strings.HasPrefix(lines[0], "ID") || !strings.Contains(lines[0], "ARTIST_ID") {
		t.Errorf("header = %q", lines[0])
	}

	// A rule of dashes, one per header character.
	rule := strings.Fields(lines[1])
	want := []string{"--", "---------", "-----"}
	if len(rule) != len(want) {
		t.Fatalf("rule = %q, want %d fields", lines[1], len(want))
	}
	for i := range want {
		if rule[i] != want[i] {
			t.Errorf("rule field %d = %q, want %q", i, rule[i], want[i])
		}
	}
	if strings.Trim(rule[0], "-") != "" {
		t.Errorf("rule is not made of dashes: %q", lines[1])
	}

	if !strings.Contains(lines[2], "A Song") {
		t.Errorf("first data row = %q", lines[2])
	}
}

// TestTableWithNoHeadersWritesNothing covers the degenerate case, so a caller
// that forgot its headers gets silence rather than a stray blank table.
func TestTableWithNoHeadersWritesNothing(t *testing.T) {
	var out strings.Builder

	table := NewTable(&out)
	table.Row("orphan")
	table.Flush()

	if out.String() != "" {
		t.Errorf("a headerless table wrote %q", out.String())
	}
}

// TestPluralAgreesWithItsCount covers the wording fix.
//
// The CLI printed "Found 1 missing files." and, elsewhere, hedged with
// "234 track(s)". The first is simply wrong; the second is a programmer
// hedging, and it is noise on the counts a user reads most.
func TestPluralAgreesWithItsCount(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{plural(0, "missing file", "missing files"), "0 missing files"},
		{plural(1, "missing file", "missing files"), "1 missing file"},
		{plural(2, "missing file", "missing files"), "2 missing files"},
		{plural(1, "track", "tracks"), "1 track"},
		{plural(341, "track", "tracks"), "341 tracks"},
		{pluralOf(3, "changes"), "3 changes"},
	}

	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// TestQueueErrorSummaryIsBounded covers the column that made the queue table
// unusable: a full yt-dlp diagnostic is around 120 characters, and one of
// them pushed every other row's columns out of alignment.
func TestQueueErrorSummaryIsBounded(t *testing.T) {
	long := "execute yt-dlp: access forbidden: ERROR: unable to download video data: HTTP Error 403: Forbidden"

	got := queueErrorSummary(long)
	if len([]rune(got)) > cellWidthError+8 {
		t.Errorf("summary is %d runes, want it bounded near %d: %q", len([]rune(got)), cellWidthError, got)
	}

	// A recognised class is named, so a reader knows the category without
	// parsing the message.
	if got != "forbidden" {
		t.Errorf("summary = %q, want the classified kind", got)
	}

	// No failure is a dash, not an empty cell, so the column does not look
	// like a rendering fault.
	if got := queueErrorSummary(""); got != "-" {
		t.Errorf("summary for no error = %q, want -", got)
	}

	// An unrecognised message is still bounded.
	unrecognised := queueErrorSummary("something went wrong in a way nobody predicted " + strings.Repeat("x", 200))
	if len([]rune(unrecognised)) > cellWidthError+8 {
		t.Errorf("unrecognised summary is %d runes: %q", len([]rune(unrecognised)), unrecognised)
	}
}

// TestUnknownDurationIsNotZero is the second half of the duration fix: a
// marker that a real duration could also produce would leave the column just
// as ambiguous as before.
func TestUnknownDurationIsNotZero(t *testing.T) {
	if unknownDuration == "0:00" {
		t.Error("unknown still renders as zero")
	}

	for _, secs := range []int{0, -1} {
		if got := formatDuration(secs); got != unknownDuration {
			t.Errorf("formatDuration(%d) = %q, want the unknown marker", secs, got)
		}
	}

	// Every real duration still renders as a time.
	for _, c := range []struct {
		secs int
		want string
	}{{5, "0:05"}, {60, "1:00"}, {3661, "1:01:01"}} {
		if got := formatDuration(c.secs); got != c.want {
			t.Errorf("formatDuration(%d) = %q, want %q", c.secs, got, c.want)
		}
	}
}

// TestPlanSaysNothingOnce covers the duplication this command had.
//
// `playlist reseed` on an unchanged playlist printed "Nothing to change; the
// recipe already matches the membership." and then immediately "Already up to
// date; nothing to do." Two sentences for one fact, on what is the most
// common outcome of the command, which is the worst place to be verbose.
func TestPlanSaysNothingOnce(t *testing.T) {
	c := newTestContainer(t)
	seedTracks(t, c, "comet / 星街すいせい", "other / 星街すいせい")

	if _, err := c.PlaylistService.CreateFromSeed(t.Context(), "P", "track-00",
		[]string{"星街すいせい"}, library.MatchAll, nil,
		[]library.TrackID{"track-00", "track-01"}); err != nil {
		t.Fatal(err)
	}

	// Driven through the command rather than printReseedPlan directly: the
	// duplication this pins was in the caller, and a test of the callee
	// alone cannot see it.
	var out strings.Builder
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(WithContainer(t.Context(), c))
	cmd.Flags().Bool(flagAccept, false, "")
	cmd.Flags().Bool(flagYes, false, "")

	if err := playlistReseedCmd.RunE(cmd, []string{"P"}); err != nil {
		t.Fatalf("reseed: %v", err)
	}

	got := out.String()

	for _, unwanted := range []string{"Already up to date", "nothing to do"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("plan repeats itself with %q:\n%s", unwanted, got)
		}
	}

	if n := strings.Count(got, "Nothing to change"); n != 1 {
		t.Errorf("the no-change sentence appears %d times, want once:\n%s", n, got)
	}

	// The counts still have to be there, pluralised.
	if !strings.Contains(got, "2 tracks") {
		t.Errorf("the counts are missing or unpluralised:\n%s", got)
	}
}

// TestPluralIsUsedInCountsRatherThanParenthesisedSweeping guards the wording
// convention across the CLI: a count reads as a sentence, not as a template.
func TestPluralIsUsedInCountsRatherThanParenthesised(t *testing.T) {
	// The helpers themselves are covered by TestPluralAgreesWithItsCount;
	// this checks no call site has reintroduced the "(s)" form.
	forbidden := []string{"(s)", "version(s)", "track(s)", "file(s)", "row(s)", "change(s)", "playlist(s)", "download(s)"}

	for _, path := range []string{
		"sync.go", "track.go", "playlist.go", "playlist_reseed.go",
		"playlist_seed.go", "playlist_edit.go", "download.go", "debug.go",
	} {
		raw, err := os.ReadFile(filepath.Join(".", path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, bad := range forbidden {
			if strings.Contains(string(raw), bad) {
				t.Errorf("%s still contains %q", path, bad)
			}
		}
	}
}
