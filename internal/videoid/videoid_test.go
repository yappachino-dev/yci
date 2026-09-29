package videoid

import "testing"

func TestIsValid(t *testing.T) {
	valid := []string{
		"dQw4w9WgXcQ",
		"abcdefghijk",
		"ABCDEFGHIJK",
		"aBc-123_xYz",
	}
	for _, s := range valid {
		if !IsValid(s) {
			t.Errorf("IsValid(%q) = false, want true", s)
		}
	}

	invalid := []string{
		"",              // empty
		"short",         // too short
		"dQw4w9WgXcQr",  // too long
		"dQw4w9WgXc$",   // character outside the alphabet
		"dQw4w9WgXcQa",  // length is checked, not content
		"  dQw4w9WgXcQ", // whitespace
		"日本語のテストです",     // multibyte: byte length is misleading
	}
	for _, s := range invalid {
		if IsValid(s) {
			t.Errorf("IsValid(%q) = true, want false", s)
		}
	}
}

// TestPatternAndLengthAgree guards the one thing that could silently break:
// the length constant and the regular expression are written separately, and
// the pattern is built from the constant via itoa, so a length that is not
// representable in a decimal literal would make the two disagree.
func TestPatternAndLengthAgree(t *testing.T) {
	if got := Pattern.FindString("abcdefghijk"); got != "abcdefghijk" {
		t.Errorf("pattern matched %q for an ID of exactly %d characters", got, Length)
	}
	if Pattern.MatchString("abcdefghij") {
		t.Error("pattern accepted an ID one character short")
	}
	if Pattern.MatchString("abcdefghijkl") {
		t.Error("pattern accepted an ID one character long")
	}
}
