// Package videoid owns the definition of what a YouTube video ID looks like.
//
// It exists as its own package because two places need to agree on it and
// neither should have to import the other: the download queue validates
// user-supplied URLs, and the staging importer validates filenames it is
// about to turn into tracks. When those two rules are written separately they
// drift, and the failure is a staged file being imported with a source ID the
// queue would never have produced.
package videoid

import "regexp"

// Length is the fixed character count of a YouTube video ID.
const Length = 11

// Pattern matches a bare video ID: exactly Length characters drawn from the
// base64url alphabet YouTube uses.
var Pattern = regexp.MustCompile(`^[A-Za-z0-9_-]{` + itoa(Length) + `}$`)

// IsValid reports whether s is a bare video ID.
func IsValid(s string) bool {
	return Pattern.MatchString(s)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}

	return string(digits)
}
