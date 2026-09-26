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
		{0, "0:00"},
		{-1, "0:00"},
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
}

// TestTruncateCellIsRuneSafe guards a real hazard: the library is largely
// Japanese, and slicing bytes would split a multi-byte rune and render
// replacement characters in the retitle table.
func TestTruncateCellIsRuneSafe(t *testing.T) {
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
		{"counts runes, not bytes", strings.Repeat("猫", 30), 10, strings.Repeat("猫", 9) + "…"},
		// The result is exactly `limit` runes, ellipsis included.
		{"mixed script stays intact", "少女A / 猫又おかゆ (cover) 歌ってみた", 12, "少女A / 猫又おかゆ…"},
		{"emoji is not split", strings.Repeat("🎵", 5), 3, "🎵🎵…"},
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

// TestNewTabWriterAlignsColumns documents the shared formatting helper:
// every list command depends on it to line columns up identically.
func TestNewTabWriterAlignsColumns(t *testing.T) {
	var out strings.Builder

	w := newTabWriter(&out)
	w.Write([]byte("ID\tNAME\n"))
	w.Write([]byte("a\tA very long artist name\n"))
	w.Write([]byte("bb\tB\n"))
	w.Flush()

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3:\n%s", len(lines), out.String())
	}

	// Every row must start its second column at the header's offset,
	// otherwise the table renders ragged.
	col2 := strings.Index(lines[0], "NAME")
	if col2 < 0 {
		t.Fatalf("header has no NAME column: %q", lines[0])
	}

	for i, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			t.Fatalf("row %d has fewer than two columns: %q", i, line)
		}
		if got := strings.Index(line, fields[1]); got != col2 {
			t.Errorf("row %d starts column 2 at %d, want %d: %q", i, got, col2, line)
		}
	}
}
