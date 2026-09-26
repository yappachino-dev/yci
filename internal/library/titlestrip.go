package library

import "strings"

// StripTitle removes decoration and version markers from a raw title and
// does nothing else. It never splits, reorders or infers: a title with no
// recognizable noise comes back unchanged.
//
// This is the only title transformation yci applies automatically. The
// difference is not thoroughness but kind — stripping deletes tokens it
// recognizes as noise, while inference guesses where structure begins and
// can therefore be confidently wrong (it swaps artist and song on the most
// common Western upload format). Stripping cannot invent anything.
func StripTitle(raw string) string {
	remaining := strings.TrimSpace(raw)
	if remaining == "" {
		return ""
	}

	for range stripRounds {
		next, changed := stripOnce(remaining)
		if !changed {
			break
		}
		remaining = next
	}

	stripped := cleanSegment(remaining)
	if stripped == "" {
		// Unrecognizable structure: the original is the safest answer.
		return strings.TrimSpace(raw)
	}

	return stripped
}

// stripRounds bounds the fixed-point loop. One pass removes the outermost
// decoration; peeling it can expose a further marker ("... Cover
// 歌ってみた" hides "Cover" behind "歌ってみた"), so passes repeat until the
// title stops shrinking. The bound keeps a pathological title from looping
// forever; four rounds is far more nesting than real titles contain.
const stripRounds = 4

// stripOnce performs one peeling pass, reporting whether anything changed.
func stripOnce(s string) (string, bool) {
	before := s

	s, _, versions := peelLeading(s, nil)
	s, _, versions = peelTrailing(s, versions)
	s, _ = peelBareVersion(s)

	return s, s != before
}
