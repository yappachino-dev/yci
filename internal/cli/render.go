package cli

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/yappachino-dev/yci/internal/library"
)

// Cell display limits, in terminal cells. They exist so a long title cannot
// push the ID column off the screen, and are grouped because the point at
// which a title stops being readable is the same across commands.
//
// The unit is terminal cells, not runes and not bytes. Those are three
// different numbers for any title with Japanese text in it, and only one of
// them is what the reader sees.
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
	// cellWidthError bounds the error column in the download queue. A full
	// yt-dlp diagnostic is around 120 characters, which is enough to push
	// every other column off a normal terminal.
	cellWidthError = 40
)

// changePreviewLimit caps how many added/removed rows a diff-style command
// prints before summarising the rest. Reseed and retitle are the two commands
// that show a change list, and a cap on one but not the other would make the
// same kind of output behave two ways.
const changePreviewLimit = 20

// shortTrackID abbreviates a track ID to a fixed prefix, which is long enough
// to be unambiguous in practice and short enough for a column.
//
// Unlike truncateCell this is a prefix, not a truncation, so it never grows
// an ellipsis: an ID is an identifier and decorating it would break the one
// thing it is for.
func shortTrackID(id library.TrackID) string {
	s := string(id)
	if displayWidth(s) <= cellWidthID {
		return s
	}

	return string([]rune(s)[:cellWidthID])
}

// displayWidth is how many terminal cells s occupies, which is the only
// column-width measure that matches what the reader sees.
//
// Runes and bytes are both wrong here, in opposite directions. A byte count
// triples a CJK character and a rune count calls it one, so a library of
// Japanese tracks is either ragged or twice as wide as intended. The CLI
// exists to display a library that is largely Japanese, so this is not an edge
// case: it is the normal case.
//
// runewidth is already linked into this binary through huh, so using it here
// costs no new dependency and gets the maintained East Asian Width tables
// rather than a hand-rolled subset that would be wrong for every character
// outside the block somebody thought of.
func displayWidth(s string) int {
	return runewidth.StringWidth(s)
}

// truncateCell collapses whitespace and clips s to limit terminal cells,
// appending an ellipsis when anything was dropped.
//
// The limit is a visual budget, so a title of Japanese text is clipped at half
// the character count of an ASCII one and occupies the same width on screen.
// Clipping by rune count instead would hand a CJK title twice the column it
// was meant to have, which is how a table that looked correct with
// placeholder data came out ragged against a real library.
func truncateCell(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")

	if displayWidth(s) <= limit {
		return s
	}
	// A limit below 2 would leave no room for content and an ellipsis;
	// clamping keeps the helper total rather than producing a broken cell on
	// a caller's typo.
	if limit < 2 {
		return "…"
	}

	// Budget for the ellipsis, then take whole characters until the budget
	// is spent. The one-rune overshoot is corrected by the check inside the
	// loop rather than by a separate pass.
	budget := limit - displayWidth("…")
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if used+w > budget {
			break
		}
		b.WriteRune(r)
		used += w
	}

	return b.String() + "…"
}
