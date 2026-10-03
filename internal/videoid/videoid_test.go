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
