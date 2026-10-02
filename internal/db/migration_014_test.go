package db

import (
	"testing"
)

// TestMigration014CreatesChannelIdentity checks the new schema exists and that
// the uniqueness constraint on channel_id actually holds.
func TestMigration014CreatesChannelIdentity(t *testing.T) {
	dbConn := openMigratedDB(t)
	if err := RunMigrations(t.Context(), dbConn); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := dbConn.Exec(
		`INSERT INTO artists (id, display_name) VALUES ('a', 'A'), ('b', 'B'), ('c', 'C'), ('d', 'D')`,
	); err != nil {
		t.Fatalf("seed artists: %v", err)
	}

	if _, err := dbConn.Exec(
		`INSERT INTO channels (artist_id, channel_name, channel_id) VALUES ('a', 'A', 'UC1')`,
	); err != nil {
		t.Fatalf("insert channel with id: %v", err)
	}

	// The same YouTube channel must not attach to a second artist. This is
	// the constraint that makes "same channel, two spellings" unrepresentable.
	_, err := dbConn.Exec(
		`INSERT INTO channels (artist_id, channel_name, channel_id) VALUES ('b', 'A renamed', 'UC1')`,
	)
	if err == nil {
		t.Error("inserting the same channel_id for a second artist succeeded, want a uniqueness failure")
	}

	// Distinct channels are fine, and NULL ids coexist (they are how a
	// hand-made link looks before its ID is known).
	if _, err := dbConn.Exec(
		`INSERT INTO channels (artist_id, channel_name, channel_id) VALUES ('b', 'B', 'UC2'), ('c', 'C', NULL), ('d', 'D', NULL)`,
	); err != nil {
		t.Errorf("distinct/null channel ids rejected: %v", err)
	}

	// video_channels round-trips and re-set overwrites.
	if _, err := dbConn.Exec(
		`INSERT INTO video_channels (video_id, channel_id) VALUES ('vid1', 'UC1')`,
	); err != nil {
		t.Fatalf("insert video channel: %v", err)
	}
	var got string
	if err := dbConn.QueryRow(`SELECT channel_id FROM video_channels WHERE video_id='vid1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "UC1" {
		t.Errorf("channel_id = %q, want UC1", got)
	}
	if _, err := dbConn.Exec(
		`INSERT INTO video_channels (video_id, channel_id) VALUES ('vid1', 'UC2') ON CONFLICT (video_id) DO UPDATE SET channel_id = excluded.channel_id`,
	); err != nil {
		t.Errorf("re-set video channel: %v", err)
	}
	if err := dbConn.QueryRow(`SELECT channel_id FROM video_channels WHERE video_id='vid1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "UC2" {
		t.Errorf("after re-set channel_id = %q, want UC2 (retry must overwrite)", got)
	}
}
