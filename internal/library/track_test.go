package library

import (
	"testing"

	"github.com/yappachino-dev/yci/internal/store"
)

// TestMissingDurationIncludesNullRows pins the query against the display.
//
// A NULL duration renders as unknown exactly like a 0 does, so a row the UI
// shows as unknown but the probe skips is a track that can never be filled in.
// The check is defensive — no write path produces NULL today — but the
// invariant belongs in the query rather than in a one-time data cleanup.
func TestMissingDurationIncludesNullRows(t *testing.T) {
	dbConn := newLibraryDBConn(t)

	// Written straight to SQL: the Go insert path always binds a valid
	// integer, so this is the only way to produce the state being tested.
	const insert = `INSERT INTO tracks (id, title, relative_path, duration_sec)
		VALUES ('null-id', 'NULL', 'a.opus', NULL),
		       ('zero-id', 'Zero', 'b.opus', 0),
		       ('known-id', 'Known', 'c.opus', 90)`

	if _, err := dbConn.Exec(insert); err != nil {
		t.Fatalf("seed tracks: %v", err)
	}

	repo := NewSQLiteTrackRepository(store.New(dbConn))
	missing, err := repo.MissingDuration(t.Context())
	if err != nil {
		t.Fatalf("MissingDuration: %v", err)
	}

	got := make(map[string]bool, len(missing))
	for _, tr := range missing {
		got[string(tr.ID)] = true
	}

	for _, id := range []string{"null-id", "zero-id"} {
		if !got[id] {
			t.Errorf("track %q missing from MissingDuration, want it probed", id)
		}
	}
	if got["known-id"] {
		t.Error("track with a known duration was offered for probing")
	}
}
