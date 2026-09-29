package library

import (
	"errors"
	"testing"
)

// TestBlankNamesReportOneSentinel pins that the three name validators agree
// on how a blank name is reported.
//
// Each used to build its own anonymous fmt.Errorf, so a caller that had to
// tell "the user typed nothing" apart from a database failure had no way to
// do it. They now share ErrNameEmpty and keep their own prefix so the message
// still says which name was blank.
func TestBlankNamesReportOneSentinel(t *testing.T) {
	cases := []struct {
		kind string
		err  error
	}{
		{"artist", ArtistName("  ").Validate()},
		{"folder", FolderName(" ").Validate()},
		{"channel", ChannelName("\t").Validate()},
	}

	for _, c := range cases {
		if !errors.Is(c.err, ErrNameEmpty) {
			t.Errorf("%s: errors.Is(err, ErrNameEmpty) = false for %v", c.kind, c.err)
		}
		if c.err == nil {
			t.Errorf("%s: expected an error for a blank name", c.kind)
		}
	}

	// Valid names must still pass.
	if err := ArtistName("a").Validate(); err != nil {
		t.Errorf("ArtistName(a).Validate() = %v", err)
	}
	if err := FolderName("a").Validate(); err != nil {
		t.Errorf("FolderName(a).Validate() = %v", err)
	}
	if err := ChannelName("a").Validate(); err != nil {
		t.Errorf("ChannelName(a).Validate() = %v", err)
	}
}
