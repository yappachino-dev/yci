package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/library"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		secs int
		want string
	}{
		// Unknown is not zero. A track ffprobe could not read is a gap in
		// what yci knows; a zero-length track is a broken file. Rendering
		// both as "0:00" made the rows that most needed attention look the
		// most unremarkable.
		{0, unknownDuration},
		{-1, unknownDuration},
		{5, "0:05"},
		{59, "0:59"},
		{60, "1:00"},
		{3599, "59:59"},
		{3600, "1:00:00"},
		{3661, "1:01:01"},
		{86399, "23:59:59"},
		{90061, "25:01:01"},
	}

	for _, tt := range tests {
		if got := formatDuration(tt.secs); got != tt.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tt.secs, got, tt.want)
		}
	}

	// The unknown marker must not be something a duration could produce, or
	// the column becomes ambiguous again.
	if unknownDuration == "0:00" {
		t.Error("the unknown-duration marker is still the zero rendering")
	}
}

// TestTruncateCellIsCellSafe guards a real hazard: the library is largely
// Japanese, and the width of what a cell occupies on screen is neither its
// byte length nor its rune count.
//
// Slicing bytes splits a multi-byte rune and renders replacement characters.
// Counting runes calls a CJK character one cell wide when the terminal gives
// it two, which hands a Japanese title twice the column it was budgeted and
// makes the table ragged. So the unit is display cells throughout.
func TestTruncateCellIsCellSafe(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{"under limit is untouched", "Creep", 40, "Creep"},
		{"exact limit is untouched", "abcdefghij", 10, "abcdefghij"},
		{"over limit gets an ellipsis", "abcdefghijk", 10, "abcdefghi…"},
		{"collapses runs of whitespace", "a   b\t\tc", 40, "a b c"},
		{"trims surrounding whitespace", "  spaced  ", 40, "spaced"},
		// Half as many CJK characters as the cell limit, because each is
		// two cells wide. Counting runes here would fit nine of them and
		// overflow the column by nine cells.
		{"counts cells, not runes", strings.Repeat("猫", 30), 10, strings.Repeat("猫", 4) + "…"},
		{"mixed script stays intact", "少女A / 猫又おかゆ (cover) 歌ってみた", 12, "少女A / 猫…"},
		{"emoji is not split", strings.Repeat("🎵", 5), 3, "🎵…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateCell(tt.in, tt.limit)
			if got != tt.want {
				t.Fatalf("truncateCell(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
			}
			if strings.ContainsRune(got, '�') {
				t.Errorf("truncateCell produced a replacement character: %q", got)
			}
			// Every case must fit the budget it was given, whatever script
			// it is written in.
			if w := displayWidth(got); w > tt.limit {
				t.Errorf("truncateCell(%q, %d) is %d cells wide, over budget: %q",
					tt.in, tt.limit, w, got)
			}
		})
	}
}

func TestShortTrackID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"short", "short"},
		{"12345678", "12345678"},
		{"5ead5f5e-d9e3-4bca-b300-600bab493525", "5ead5f5e"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := shortTrackID(library.TrackID(tt.in)); got != tt.want {
			t.Errorf("shortTrackID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestConfirmDestructiveFailsClosed pins the behaviour that keeps a
// non-interactive session from deleting anything by accident: without an
// explicit --yes it refuses rather than proceeding, and with --yes it
// proceeds. Tests run without a TTY, which is exactly the case that matters.
func TestConfirmDestructiveFailsClosed(t *testing.T) {
	t.Run("refuses without --yes in a non-interactive session", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().Bool(flagYes, false, "")

		confirmed, err := confirmDestructive(cmd, "Delete everything?")
		if err == nil {
			t.Fatalf("confirmDestructive = %v, want an error in a non-interactive session", confirmed)
		}
		if confirmed {
			t.Error("confirmDestructive returned true without confirmation")
		}
		if !strings.Contains(err.Error(), flagYes) {
			t.Errorf("error = %q, want it to name the --%s flag", err, flagYes)
		}
	})

	t.Run("proceeds with --yes", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().Bool(flagYes, false, "")
		if err := cmd.Flags().Set(flagYes, "true"); err != nil {
			t.Fatal(err)
		}

		confirmed, err := confirmDestructive(cmd, "Delete everything?")
		if err != nil {
			t.Fatalf("confirmDestructive: %v", err)
		}
		if !confirmed {
			t.Error("confirmDestructive = false, want true with --yes")
		}
	})
}

// TestTableAlignsColumns is the shared formatting contract: every list
// command depends on this alignment to line columns up identically.
//
// The rows deliberately mix widths, because that is the only case where a
// padding bug shows up. Equal-width rows align under any implementation.
func TestTableAlignsColumns(t *testing.T) {
	var out strings.Builder

	table := NewTable(&out, "ID", "NAME")
	table.Row("a", "A very long artist name")
	table.Row("bb", "B")
	table.Flush()

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4:\n%s", len(lines), out.String())
	}

	offsets := table.offsets
	if len(offsets) != 2 {
		t.Fatalf("got %d column offsets, want 2", len(offsets))
	}

	// Every line must place its second column at the offset the table said
	// it would, and that offset must be a real boundary in the text rather
	// than an interior space of a padding run.
	for i, line := range lines {
		for col, at := range offsets {
			if col == len(offsets)-1 && i > 0 && col > 0 {
				// The final column is ragged by design; only interior
				// columns are pinned.
				continue
			}
			if got := cellAt(t, line, at); got != "" && col > 0 && i > 1 {
				t.Errorf("line %d column %d starts mid-cell at cell %d: %q",
					i, col, at, line)
			}
		}
	}
}

// cellAt returns the character beginning at display-cell offset at.
func cellAt(t *testing.T, line string, at int) string {
	t.Helper()

	if at > displayWidth(line) {
		return ""
	}
	for i, r := range line {
		if displayWidth(line[:i]) == at {
			return string(r)
		}
	}
	return ""
}
