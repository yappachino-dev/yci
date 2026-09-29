package download

import (
	"errors"
	"testing"
)

// TestParseErrorClassification pins the yt-dlp diagnostic wording this
// package depends on. A yt-dlp upgrade that rewords these strings is a
// behaviour change, not a test failure to be papered over: if this test
// needs updating, ErrForbidden is no longer being detected and the user has
// lost the cookies/sign-in hint.
func TestParseErrorClassification(t *testing.T) {
	forbidden := []string{
		"ERROR: unable to download video data: HTTP Error 403: Forbidden",
		"ERROR: [youtube] abc: Sign in to confirm you're not a bot. Use --cookies",
		"HTTP Error 403 Forbidden",
		"got status code 403",
		"error 403 while downloading",
	}
	for _, stderr := range forbidden {
		if err := parseError(stderr); !errors.Is(err, ErrForbidden) {
			t.Errorf("parseError(%q) = %v, want ErrForbidden", stderr, err)
		}
	}

	execution := []string{
		"ERROR: unable to download video data: HTTP Error 404: Not Found",
		"ERROR: [youtube] abc: Video unavailable",
		"error: unable to write data",
		"",
	}
	for _, stderr := range execution {
		if err := parseError(stderr); !errors.Is(err, ErrExecution) {
			t.Errorf("parseError(%q) = %v, want ErrExecution", stderr, err)
		}
	}
}
