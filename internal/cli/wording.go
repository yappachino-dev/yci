package cli

import "fmt"

// Wording for counts.
//
// The CLI says "1 missing files" and "234 track(s)". The first is simply
// wrong; the second is a programmer hedging, and it makes the output harder to
// read for the case that actually occurs most often — a count of one, or of
// two, where the reader already knows the noun.
//
// A shared pluraliser also means a message and its count cannot drift apart,
// which is what happens when "track(s)" is written out at each call site.

// plural renders a count with the right form of a noun: "1 track",
// "2 tracks". Singular is used for exactly one.
func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}

	return fmt.Sprintf("%d %s", n, pluralForm)
}

// pluralOf renders a count with a noun in its plural form always, for the
// cases where the reader is scanning a list rather than reading a sentence.
func pluralOf(n int, pluralForm string) string {
	return fmt.Sprintf("%d %s", n, pluralForm)
}
