package library

import (
	"regexp"
	"strings"
	"unicode"
)

// TitleConfidence reports how much of TitleParts the parser actually
// understood: none means nothing was found and the raw title stands.
//
// It is deliberately not used to gate writes. It was measured against
// Western upload titles and reported high confidence while swapping artist
// and song, so it describes parse hygiene rather than correctness. Treat
// the parts it describes as hints, or ignore it.
type TitleConfidence int

const (
	ConfidenceNone TitleConfidence = iota
	ConfidenceLow
	ConfidenceHigh
)

func (c TitleConfidence) String() string {
	switch c {
	case ConfidenceHigh:
		return "high"
	case ConfidenceLow:
		return "low"
	default:
		return "none"
	}
}

// TitleParts is the parser's guess at a raw video title's structure.
//
// Frozen: the rule set behind it is not being extended, and no part of it
// writes to a display title. The values are hints, and the only reason they
// are stored at all is that they were free to compute at import.
type TitleParts struct {
	Raw        string
	Song       string
	Performer  string
	Version    string
	Confidence TitleConfidence
}

// versionLexicon maps recognized version markers to a normalized form.
var versionLexicon = map[string]string{
	"cover":          "cover",
	"cover mv":       "cover",
	"english cover":  "cover",
	"japanese cover": "cover",
	"歌ってみた":          "cover",
	"official":       "official",
	"original":       "original",
	"original song":  "original",
	"live":           "live",
	"acoustic":       "acoustic",
	"instrumental":   "instrumental",
	"remix":          "remix",
	"acapella":       "acapella",
	"mv":             "mv",
	"lyric video":    "lyric video",
}

// versionHint recognizes version markers that carry extra trailing context,
// such as "ENGLISH COVER" or "Original Song : Chaos Zero Nightmare OST".
var versionHint = regexp.MustCompile(`(?i)^(cover\s*(mv)?|mv|english\s*cover|japanese\s*cover|official|original(\s*song)?|live|acoustic|instrumental|remix|acapella|lyric\s*video|歌ってみた)\b`)

// bracketPairs are the decoration delimiters YouTube titles use, in both
// full-width and half-width forms.
var bracketPairs = []struct{ open, close string }{
	{"【", "】"},
	{"『", "』"},
	{"「", "」"},
	{"[", "]"},
	{"（", "）"},
	{"(", ")"},
}

// separators split song from performer. Each requires surrounding
// whitespace or is a full-width solidus, so titles like "POP/STARS" and
// "K/DA" survive intact.
var separators = []string{
	" / ", "／", " - ", " – ", " feat ", " feat. ",
}

// closingBrackets may sit directly against the separator's slash
// (「コワレヤスキ」/ PERFORMER). The bracket closes the song's decoration
// and therefore belongs to the song, not to the separator.
var closingBrackets = []string{"」", "』", "】"}

// ParseTitle splits a raw video title into song, performer, and version.
// It is heuristic by nature: human-typed titles have no fixed grammar, and
// the same trailing parenthesis can mean a version or a performer. Unknown
// structure is reported through Confidence rather than guessed, and Raw is
// always preserved so callers can fall back to it.
func ParseTitle(raw string) TitleParts {
	parts := TitleParts{Raw: raw}

	remaining := strings.TrimSpace(raw)
	if remaining == "" {
		return parts
	}

	// A trailing group names a performer more reliably than a leading
	// decoration does, so the stronger candidate wins.
	remaining, weakHint, versions := peelLeading(remaining, nil)
	remaining, strongHint, versions := peelTrailing(remaining, versions)

	remaining, bare := peelBareVersion(remaining)
	if bare != "" {
		versions = append(versions, bare)
	}

	song, performer, version, consumedReading := splitSongPerformer(remaining)
	versions = append(versions, version)

	if song == "" {
		song = remaining
	}

	// A performer taken from a leading decoration rather than a trailing
	// group or a structural split is a guess worth confirming. A trailing
	// group is itself a structural signal.
	performerFromWeakHint := false
	if performer == "" {
		if strongHint != "" {
			performer = strongHint
		} else {
			performer, performerFromWeakHint = weakHint, weakHint != ""
		}
	}

	parts.Song = cleanSegment(song)
	parts.Performer = cleanPerformer(performer)
	parts.Version = joinVersions(versions)
	parts.Confidence = confidence(parts, performerFromWeakHint, consumedReading)

	return parts
}

type decorationKind int

const (
	decorationOther decorationKind = iota
	decorationVersion
	decorationSeries
)

// peelLeading strips leading decorations, classifying each as a version
// marker, a series tag (ignored), or a weak performer hint.
func peelLeading(s string, versions []string) (string, string, []string) {
	performerHint := ""

	for {
		inner, rest, ok := cutLeadingGroup(s)
		if !ok {
			return s, performerHint, versions
		}

		switch classifyDecoration(inner) {
		case decorationVersion:
			if v, _ := normalizeVersion(inner); v != "" {
				versions = append(versions, v)
			}
		case decorationSeries:
			// Series and event tags carry no song information.
		default:
			// A decoration followed by a separator names the song, not the
			// performer: 『TAKO∞TAKOVER』 - Ninomae Ina'nis. Reattach it so
			// the ordinary split handles it.
			if hasSeparatorPrefix(rest) {
				s = inner + rest
				continue
			}
			if performerHint == "" {
				performerHint = inner
			}
		}

		// Only whitespace and interpunct are dropped: a separator such as
		// " - " is part of the title's structure and must survive for the
		// song/performer split to see it.
		s = strings.TrimLeft(rest, " \t・")
	}
}

// peelTrailing strips trailing groups, classifying them as version markers
// or performers. Anything unrecognized stays part of the title.
func peelTrailing(s string, versions []string) (string, string, []string) {
	performerHint := ""

	for {
		inner, rest, ok := cutTrailingGroup(s)
		if !ok {
			return s, performerHint, versions
		}

		if v, isVersion := normalizeVersion(inner); isVersion {
			versions = append(versions, v)
		} else if isSeriesTag(inner) {
			// Series and event numbers decorate the title without naming
			// anyone.
		} else if looksLikePerformer(inner) {
			if performerHint == "" {
				performerHint = inner
			}
		} else {
			// Unrecognized trailing content belongs to the title.
			return s, performerHint, versions
		}

		s = strings.TrimRight(rest, " \t・-–—~")
	}
}

// peelBareVersion handles an unbracketed trailing version token, as in
// "... ・Ayunda Risu COVER". The caller records the returned marker.
func peelBareVersion(s string) (string, string) {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return s, ""
	}

	last := fields[len(fields)-1]
	if v, ok := normalizeVersion(last); ok {
		return strings.Join(fields[:len(fields)-1], " "), v
	}

	return s, ""
}

// splitSongPerformer splits on the earliest separator and reclassifies the
// right-hand side, which may be a version marker or a reading rather than a
// performer.
func splitSongPerformer(s string) (song, performer, version string, consumedReading bool) {
	idx, sep := firstSeparator(s)
	if idx < 0 {
		return s, "", "", false
	}

	left := strings.TrimSpace(s[:idx])
	right := strings.TrimSpace(s[idx+len(sep):])

	if right == "" {
		return left, "", "", false
	}

	if v, ok := normalizeVersion(right); ok {
		return left, "", v, false
	}

	// "Dilemma / ジレンマ - Nanashi Mumei" puts a kana reading between the
	// song and the performer. Kana-only text next to a Latin-script song is
	// a reading; kanji-bearing text such as 星街すいせい is a name.
	if head, tail, found := strings.Cut(right, " - "); found {
		if isPureKana(head) && containsLatin(left) {
			song = left + " " + sep + " " + head
			if looksLikePerformer(tail) {
				return song, tail, "", true
			}
			return song, "", "", true
		}
	}

	if !looksLikePerformer(right) {
		// Too long or too structured to be a name: the whole remainder is
		// part of the song.
		return s, "", "", false
	}

	return left, right, "", false
}

func hasSeparatorPrefix(s string) bool {
	for _, sep := range separators {
		if strings.HasPrefix(s, sep) {
			return true
		}
	}
	for _, bracket := range closingBrackets {
		if strings.HasPrefix(s, bracket+"/") {
			return true
		}
	}

	return false
}

// firstSeparator locates the earliest separator occurrence, preferring the
// earliest position over the order separators are listed in.
func firstSeparator(s string) (int, string) {
	best, bestSep := -1, ""

	consider := func(idx int, sep string) {
		if idx >= 0 && (best < 0 || idx < best) {
			best, bestSep = idx, sep
		}
	}

	for _, sep := range separators {
		consider(strings.Index(s, sep), sep)
	}

	// The bracket closes the song's decoration and stays on the song's
	// side, so the separator starts at the slash itself.
	for _, bracket := range closingBrackets {
		j := strings.Index(s, bracket+"/")
		if j < 0 {
			continue
		}

		slash := j + len(bracket)
		sep := "/"
		if strings.HasPrefix(s[slash+1:], " ") {
			sep = "/ "
		}

		consider(slash, sep)
	}

	return best, bestSep
}

func classifyDecoration(inner string) decorationKind {
	trimmed := strings.TrimSpace(inner)
	if trimmed == "" {
		return decorationOther
	}
	if isSeriesTag(trimmed) {
		return decorationSeries
	}
	if _, ok := normalizeVersion(trimmed); ok {
		return decorationVersion
	}

	return decorationOther
}

func isSeriesTag(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "#")
}

func cutLeadingGroup(s string) (inner, rest string, ok bool) {
	for _, p := range bracketPairs {
		if !strings.HasPrefix(s, p.open) {
			continue
		}
		if end := strings.Index(s, p.close); end > len(p.open) {
			return s[len(p.open):end], s[end+len(p.close):], true
		}
	}

	return "", s, false
}

func cutTrailingGroup(s string) (inner, rest string, ok bool) {
	// Every pair is tried: a mismatched opener or closer is common in
	// hand-typed titles and must not stop the other pairs from matching.
	for _, p := range bracketPairs {
		if !strings.HasSuffix(s, p.close) {
			continue
		}
		start := strings.LastIndex(s, p.open)
		if start < 0 || start+len(p.open) >= len(s)-len(p.close) {
			continue
		}

		return s[start+len(p.open) : len(s)-len(p.close)], s[:start], true
	}

	return "", s, false
}

// normalizeVersion maps free-form version text onto a canonical marker.
func normalizeVersion(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", false
	}

	lower := strings.ToLower(trimmed)
	if v, ok := versionLexicon[lower]; ok {
		return v, true
	}

	if m := versionHint.FindString(lower); m != "" {
		key := strings.Join(strings.Fields(m), " ")
		if v, ok := versionLexicon[key]; ok {
			return v, true
		}
		return key, true
	}

	return "", false
}

// looksLikePerformer is a conservative guard: only text that plausibly
// names a person or channel may be treated as a performer. Long text or
// text carrying further structure stays part of the title.
func looksLikePerformer(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len([]rune(trimmed)) > 48 {
		return false
	}

	return !strings.ContainsAny(trimmed, "()（）【】「」[]")
}

// isPureKana reports whether every letter in the segment is hiragana or
// katakana, which is what distinguishes a reading from a name.
func isPureKana(s string) bool {
	letters := 0

	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			letters++
		case unicode.IsLetter(r):
			return false
		case unicode.IsDigit(r):
			return false
		}
	}

	return letters >= 2
}

func containsLatin(s string) bool {
	for _, r := range s {
		if r < unicode.MaxASCII && unicode.IsLetter(r) {
			return true
		}
	}

	return false
}

func confidence(parts TitleParts, performerFromWeakHint, consumedReading bool) TitleConfidence {
	if parts.Version == "" && parts.Performer == "" {
		return ConfidenceNone
	}

	// Ambiguity the parser could not resolve: a consumed reading, a
	// leftover kana name, or decoration still embedded in the song.
	if consumedReading || (isPureKana(parts.Performer) && containsLatin(parts.Song)) {
		return ConfidenceLow
	}
	if strings.ContainsAny(parts.Song, "()（）【】「」[]") {
		return ConfidenceLow
	}

	// A performer taken from a leading decoration rather than a trailing
	// group or a structural split is a guess worth confirming.
	if performerFromWeakHint && parts.Performer != "" {
		return ConfidenceLow
	}

	return ConfidenceHigh
}

// joinVersions merges markers in discovery order, dropping the format
// marker "mv" when a real version is also present.
func joinVersions(versions []string) string {
	seen := make(map[string]bool, len(versions))
	out := make([]string, 0, len(versions))

	for _, v := range versions {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}

	if len(out) > 1 && seen["mv"] {
		kept := out[:0]
		for _, v := range out {
			if v != "mv" {
				kept = append(kept, v)
			}
		}
		out = kept
	}

	return strings.Join(out, " ")
}

func cleanSegment(s string) string {
	cleaned := strings.Join(strings.Fields(s), " ")

	return strings.Trim(cleaned, " \t・-–—~,,")
}

func cleanPerformer(s string) string {
	cleaned := cleanSegment(s)

	// Handles are names, not handles.
	cleaned = strings.ReplaceAll(cleaned, "@", "")

	return strings.Trim(cleaned, " \t・-–—~×&,")
}
