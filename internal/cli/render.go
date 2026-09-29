package cli

import (
	"io"
	"strings"
	"text/tabwriter"

	"github.com/yappachino-dev/yci/internal/library"
)

// Column widths for table output. tabwriter takes these as (padding, tabwidth)
// and derives alignment from the data, so the numbers below only have to be
// consistent, not tuned per table.
const (
	tabwriterPadding = 3
	tabwriterTabstop = 0
)

// Cell display limits, in runes. They exist so a long title cannot push the
// ID column off the screen, and are grouped because the point at which a
// title stops being readable is the same across commands.
const (
	// cellWidthID is the most a short display ID is ever shown as. IDs are
	// UUIDs, so anything longer is noise the user cannot act on.
	cellWidthID = 8
	// cellWidthTitle is the usual title budget in list output.
	cellWidthTitle = 50
	// cellWidthTitleWide is for detail output where the title is the point
	// of the row.
	cellWidthTitleWide = 60
	// cellWidthDiff is for before/after title comparisons, which must line
	// up in two columns.
	cellWidthDiff = 40
	// cellWidthSeed is for the seed line in a recipe report.
	cellWidthSeed = 50
)

// changePreviewLimit caps how many added/removed rows a diff-style command
// prints before summarising the rest. Reseed and retitle are the two commands
// that show a change list, and a cap on one but not the other would make the
// same kind of output behave two ways.
const changePreviewLimit = 20

// newTabWriter returns the aligned column writer shared by every list
// command, so table output looks the same across entity groups.
func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, tabwriterPadding, ' ', tabwriterTabstop)
}

// shortTrackID abbreviates a track ID to a fixed prefix, which is long enough
// to be unambiguous in practice and short enough for a column.
func shortTrackID(id library.TrackID) string {
	s := string(id)
	if len(s) <= cellWidthID {
		return s
	}

	return s[:cellWidthID]
}

// truncateCell collapses whitespace and clips s to limit runes, appending an
// ellipsis when anything was dropped. It counts runes rather than bytes so a
// CJK or emoji title is not cut in half.
func truncateCell(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")

	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	// A limit below 2 would slice a negative length; clamping keeps the
	// helper total rather than panicking on a caller's typo.
	if limit < 2 {
		return "…"
	}

	return string(runes[:limit-1]) + "…"
}
