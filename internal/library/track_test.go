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

// TestTracksAreOrderedForReading pins the ORDER BY that replaced an
// unspecified one. Without it the rows came back in whatever order SQLite
// yielded, which is stable across runs and therefore easy to mistake for a
// deliberate order until you notice it is not alphabetical.
func TestTracksAreOrderedForReading(t *testing.T) {
	dbConn := newLibraryDBConn(t)

	// Foreign keys are enforced, so the artists have to exist first.
	if _, err := dbConn.Exec(
		`INSERT INTO artists (id, display_name) VALUES ('artist-a', 'A'), ('artist-z', 'Z')`,
	); err != nil {
		t.Fatalf("seed artists: %v", err)
	}

	// Inserted out of order, and with an orphan that has no artist at all.
	const insert = `INSERT INTO tracks (id, title, relative_path, artist_id) VALUES
		('id-3', 'Alpha',   'c.opus', 'artist-z'),
		('id-1', 'Beta',    'a.opus', 'artist-a'),
		('id-4', 'Zulu',    'd.opus', NULL),
		('id-2', 'Alpha',   'b.opus', 'artist-a')`

	if _, err := dbConn.Exec(insert); err != nil {
		t.Fatalf("seed tracks: %v", err)
	}

	repo := NewSQLiteTrackRepository(store.New(dbConn))
	tracks, err := repo.ByPathPattern(t.Context(), "%")
	if err != nil {
		t.Fatalf("ByPathPattern: %v", err)
	}

	var got []string
	for _, tr := range tracks {
		got = append(got, string(tr.ID))
	}

	// artist-a first, then artist-z, and the artistless track last: it has no
	// folder to file it under, so leading the list would be misleading.
	want := []string{"id-2", "id-1", "id-3", "id-4"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
