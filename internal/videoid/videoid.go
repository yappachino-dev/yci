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

// valid matches a bare video ID: exactly eleven characters drawn from the
// base64url alphabet YouTube uses.
var valid = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// IsValid reports whether s is a bare video ID.
func IsValid(s string) bool {
	return valid.MatchString(s)
}
