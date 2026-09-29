package library

import (
	"strconv"
	"strings"
	"unicode"
)

// Fragment extraction turns one seed track into a set of tokens that select
// its family, so `playlist create --from-track` can propose membership the
// user confirms once. The strategy was measured against the 341-title corpus:
// take the right-hand side of a title separator, keep it only if it recurs,
// otherwise fall back to the most recurrent token. On labelled performer
// seeds that scored 1.00 precision and 1.00 recall.
//
// Selection is a token set rather than a joined substring, so "星街 すいせい"
// still matches "星街すいせい" and "Mori Calliope" still matches
// "MoriCalliope". How the tokens combine is a mode: AND (every token present)
// is the default and is what the derivation proposes, because it is what
// keeps unrelated titles out. ANY (at least one token present) is available
// for deliberately widening a selection.

// foldFragment normalises a title for comparison: ASCII and fullwidth letters
// fold to one case, so "Mori Calliope" and "calliope" share a family.
func foldFragment(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		switch {
		case r >= 'Ａ' && r <= 'Ｚ':
			b.WriteRune(r - 'Ａ' + 'a')
		case r >= 'ａ' && r <= 'ｚ':
			b.WriteRune(r - 'ａ' + 'a')
		case r >= '０' && r <= '９':
			b.WriteRune(r - '０' + '0')
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}

	return b.String()
}

// fragmentStopwords are the words that recur across unrelated tracks, so
// selecting one produces a set that looks meaningful and is not. Measured
// against the corpus: "it" matches 16 tracks, "the" 11.
var fragmentStopwords = map[string]bool{
	// English function words and filler.
	"the": true, "and": true, "but": true, "from": true,
	"with": true,
	"this": true, "that": true, "there": true, "here": true, "you": true,
	"your": true, "our": true, "out": true, "for": true, "not": true,
	"all": true, "can": true, "will": true, "was": true, "are": true,
	"has": true, "had": true, "its": true, "it's": true, "just": true,
	"like": true, "into": true, "over": true, "under": true, "get": true,
	"let": true, "now": true, "one": true, "two": true, "new": true,
	"full": true, "edit": true, "mix": true, "song": true, "music": true,
	"video": true, "audio": true, "official": true, "lyrics": true,
	"lyric": true, "visualizer": true, "teaser": true, "trailer": true,
	// Japanese connective and title particles.
	"の": true, "と": true, "は": true, "が": true, "を": true, "に": true,
	"で": true, "も": true, "や": true, "へ": true,
	// Romanised filler.
	"feat": true, "featuring": true, "ft": true, "x": true,
}

// fragmentMinLen rejects fragments too short to identify anything. "32"
// matches one track and means nothing.
const fragmentMinLen = 3

// fragmentDecorations are stripped from a token before it is offered.
var fragmentDecorations = "「」『』【】〔〕()（）[]"

// Tokenize splits a title into selectable units. Runs of letters and digits
// are kept whole, so a Japanese performer name stays one token even though
// Japanese does not use spaces.
func Tokenize(title string) []string {
	fields := strings.FieldsFunc(title, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.Trim(f, fragmentDecorations)
		if f != "" {
			out = append(out, f)
		}
	}

	return out
}

// Fragment is a proposed token selection plus how it was arrived at.
type Fragment struct {
	// Tokens is the selected subset, in title order.
	Tokens []string
	// Recurrence is how many corpus tracks the selection matches.
	Recurrence int
	// Certain reports whether the selection came from a validated
	// separator or a fallback. Callers should confirm with the user when it
	// is false rather than presenting the set as authoritative.
	Certain bool
}

// FragmentText renders the selection for display. It is for humans only and
// must never be used for matching, because the corpus may spell the same
// name with different spacing.
func (f Fragment) FragmentText() string {
	return strings.Join(f.Tokens, " ")
}

// DeriveFragment proposes a token selection for a seed title. corpus holds
// the raw titles available to match against, and must not be empty.
//
// The right-hand side of a title separator is the performer for the corpus's
// dominant "Song / Performer" shape, but for "Artist - Song" it is the song,
// which matches one track. Validating it by recurrence before accepting it is
// what separates the two, and the fallback is what rescues the Western shape.
func DeriveFragment(seedTitle string, corpus []string) Fragment {
	stripped := StripTitle(seedTitle)
	tokens := Tokenize(stripped)

	best := func() Fragment {
		f := Fragment{Recurrence: 0}
		for _, t := range tokens {
			if fragmentStopwords[foldFragment(t)] || len([]rune(t)) < fragmentMinLen {
				continue
			}
			if n := countMatching(corpus, []string{t}); n > f.Recurrence {
				f = Fragment{Tokens: []string{t}, Recurrence: n, Certain: n >= 2}
			}
		}
		if len(f.Tokens) == 0 {
			// Nothing recurred. The whole title is the best available
			// guess, and it matches at least the seed itself.
			return Fragment{Tokens: tokens, Recurrence: countMatching(corpus, tokens)}
		}
		return f
	}

	for _, sep := range []string{" / ", " - ", " – ", " × ", "「"} {
		i := strings.Index(stripped, sep)
		if i < 0 {
			continue
		}

		right := Tokenize(strings.TrimSpace(stripped[i+len(sep):]))
		if len(right) == 0 {
			continue
		}

		if n := countMatching(corpus, right); n >= 2 {
			return Fragment{Tokens: right, Recurrence: n, Certain: true}
		}
	}

	return best()
}

// MatchesFragment reports whether a title satisfies every token in the
// selection. An empty selection matches nothing rather than everything.
// MatchMode decides how a token set combines.
type MatchMode string

const (
	// MatchAll requires every token to be present. This is the default: it
	// is what the derivation proposes, and it is why a seed like
	// "Cry Baby / 星街すいせい" does not also select every 星街すいせい
	// collab.
	MatchAll MatchMode = "all"
	// MatchAny requires at least one token. Deliberately wider, so it will
	// pull in titles that share only one word; useful when the goal is
	// "anything mentioning this at all", never as a default.
	MatchAny MatchMode = "any"
)

// ParseMatchMode maps stored and user-facing text to a mode, defaulting to
// MatchAll so an absent or unrecognized value keeps the conservative
// behaviour rather than silently widening a selection.
func ParseMatchMode(s string) MatchMode {
	if MatchMode(strings.ToLower(strings.TrimSpace(s))) == MatchAny {
		return MatchAny
	}
	return MatchAll
}

func (m MatchMode) String() string {
	if m == MatchAny {
		return string(MatchAny)
	}
	return string(MatchAll)
}

// MatchesFragment reports whether a title satisfies an AND token set.
func MatchesFragment(corpusTitle string, tokens []string) bool {
	return MatchesTokens(corpusTitle, tokens, MatchAll)
}

// MatchesTokens reports whether a title satisfies a token set under mode.
// An empty token set never matches: a playlist with no criteria must not
// silently select the whole library, which is the one outcome a user would
// never notice and never want.
//
// A token matches a title when the folded token is one of the title's
// tokens, or when the whole selection concatenated matches one of them as a
// run-together spelling. The second clause is what lets ["Mori","Calliope"]
// reach "MoriCalliope" without falling back to substring search, which would
// also let "Mori" reach "Morisette" and "body" reach "Nobody".
func MatchesTokens(corpusTitle string, tokens []string, mode MatchMode) bool {
	if len(tokens) == 0 {
		return false
	}

	haystack := foldedTokenSet(StripTitle(corpusTitle))

	// A title that spells the selection without separators, e.g. a run
	// together upload name.
	if _, ok := haystack[foldFragment(strings.Join(tokens, ""))]; ok {
		return true
	}

	for _, t := range tokens {
		_, found := haystack[foldFragment(t)]
		if mode == MatchAny && found {
			return true
		}
		if mode == MatchAll && !found {
			return false
		}
	}

	return mode == MatchAll
}

// foldedTokenSet returns the folded tokens of a title, for membership tests.
func foldedTokenSet(title string) map[string]struct{} {
	raw := Tokenize(title)

	set := make(map[string]struct{}, len(raw))
	for _, t := range raw {
		set[foldFragment(t)] = struct{}{}
	}

	return set
}

func countMatching(corpus []string, tokens []string) int {
	n := 0
	for _, t := range corpus {
		if MatchesFragment(t, tokens) {
			n++
		}
	}
	return n
}

// FormatTokens renders a token set for display.
//
// Quoting each token makes the members and the count visible, and the
// joining operator is taken from the mode so the rendered recipe cannot
// contradict how it is actually evaluated.
func FormatTokens(tokens []string, mode MatchMode) string {
	if len(tokens) == 0 {
		return "(none)"
	}

	quoted := make([]string, 0, len(tokens))
	for _, t := range tokens {
		quoted = append(quoted, strconv.Quote(t))
	}

	// The operator has to follow the mode, or an ANY recipe is displayed
	// as though its tokens all had to be present.
	joiner := " AND "
	if mode == MatchAny {
		joiner = " OR "
	}

	return strings.Join(quoted, joiner)
}
