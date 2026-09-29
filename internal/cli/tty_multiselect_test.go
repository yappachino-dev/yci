package cli

import (
	"testing"
	"time"

	"github.com/yappachino-dev/yci/internal/library"
)

// TestCandidatePickerPreselectsAndDeselects covers the per-candidate
// exclusion wizard end to end on a real terminal.
//
// The property that matters is pre-selection: every match starts selected,
// and space deselects exactly one. If the pre-selection silently did not
// apply, a user would be asked to select 60 rows by hand and the feature
// would be worse than useless.
func TestCandidatePickerPreselectsAndDeselects(t *testing.T) {
	c := newTestContainer(t)
	ids := seedTracks(t, c,
		"comet / 星街すいせい(official)",
		"another / 星街すいせい(collab)",
		"KICK BACK / other performer",
	)

	matched := []library.Track{
		{ID: ids[0], Title: "comet / 星街すいせい(official)"},
		{ID: ids[1], Title: "another / 星街すいせい(collab)"},
	}

	session := newTTYCommand(t, c)

	var (
		kept     []library.Track
		excludes []library.TrackID
	)
	wait := await(func() error {
		var err error
		kept, excludes, err = pickCandidates(session.cmd, matched, nil)
		return err
	})

	// Wait on the description rather than the title: huh renders the title
	// above the option viewport, and with a fixed height it is clipped from
	// the captured output while the rows and help line are not.
	session.waitFor(t, "Space toggles", 5*time.Second)

	// Accept as-is: both rows were selected by default.
	session.typeKeys(t, keyEnter)

	if err := wait(); err != nil {
		t.Fatalf("candidate picker returned an error: %v\n--- rendered ---\n%s", err, session.output())
	}

	if len(kept) != 2 {
		t.Errorf("accepting the default kept %d track(s), want both: %v", len(kept), kept)
	}
	if len(excludes) != 0 {
		t.Errorf("accepting the default excluded %v", excludes)
	}
}

// TestCandidatePickerDeselectsWithSpace checks that space actually removes
// the focused row, which is the whole point of the picker.
func TestCandidatePickerDeselectsWithSpace(t *testing.T) {
	c := newTestContainer(t)
	ids := seedTracks(t, c,
		"comet / 星街すいせい(official)",
		"another / 星街すいせい(collab)",
		"KICK BACK / other performer",
	)

	matched := []library.Track{
		{ID: ids[0], Title: "comet / 星街すいせい(official)"},
		{ID: ids[1], Title: "another / 星街すいせい(collab)"},
	}

	session := newTTYCommand(t, c)

	var (
		kept     []library.Track
		excludes []library.TrackID
	)
	wait := await(func() error {
		var err error
		kept, excludes, err = pickCandidates(session.cmd, matched, nil)
		return err
	})

	// Wait on the description rather than the title: huh renders the title
	// above the option viewport, and with a fixed height it is clipped from
	// the captured output while the rows and help line are not.
	session.waitFor(t, "Space toggles", 5*time.Second)

	// Move to the second row, toggle it off, and confirm. The arrow escape
	// is used rather than "j" because huh's multiselect keymap binds the
	// arrow keys; vim-style movement is not bound by default.
	session.typeKeys(t, keyDown)
	session.waitForOption(t, "another /", "focused-selected", 5*time.Second)

	session.typeKeys(t, keySpace)
	session.waitForOption(t, "another /", "focused-unselected", 5*time.Second)

	session.typeKeys(t, keyEnter)

	if err := wait(); err != nil {
		t.Fatalf("candidate picker returned an error: %v\n--- rendered ---\n%s", err, session.output())
	}

	if len(kept) != 1 || kept[0].ID != ids[0] {
		t.Errorf("kept = %v, want only the first track", kept)
	}
	if len(excludes) != 1 || excludes[0] != ids[1] {
		t.Errorf("excludes = %v, want [%s]", excludes, ids[1])
	}
}

// TestCandidatePickerHonoursPreExistingExclusions covers passing a
// previously excluded track back in: it must come up already deselected, and
// stay excluded unless the user selects it.
func TestCandidatePickerHonoursPreExistingExclusions(t *testing.T) {
	c := newTestContainer(t)
	ids := seedTracks(t, c,
		"comet / 星街すいせい(official)",
		"another / 星街すいせい(collab)",
		"KICK BACK / other performer",
	)

	matched := []library.Track{
		{ID: ids[0], Title: "comet / 星街すいせい(official)"},
		{ID: ids[1], Title: "another / 星街すいせい(collab)"},
	}

	session := newTTYCommand(t, c)

	var kept []library.Track
	wait := await(func() error {
		var err error
		kept, _, err = pickCandidates(session.cmd, matched, []library.TrackID{ids[1]})
		return err
	})

	// Wait on the description rather than the title: huh renders the title
	// above the option viewport, and with a fixed height it is clipped from
	// the captured output while the rows and help line are not.
	session.waitFor(t, "Space toggles", 5*time.Second)
	session.typeKeys(t, keyEnter)

	if err := wait(); err != nil {
		t.Fatalf("candidate picker returned an error: %v\n--- rendered ---\n%s", err, session.output())
	}

	if len(kept) != 1 || kept[0].ID != ids[0] {
		t.Errorf("kept = %v, want only the non-excluded track", kept)
	}
}
