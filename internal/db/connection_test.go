package db

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestForeignKeysHoldOnEveryPooledConnection is the regression test for a bug
// that disabled the guarantees the rest of the design assumes.
//
// "PRAGMA foreign_keys = ON" was issued once with db.Exec, which SQLite scopes
// to the connection that ran it. database/sql pools connections, so every
// connection opened afterwards had foreign_keys=0 and enforced nothing —
// silently, with no error. That is how a row violating a foreign key came to
// be accepted, which in this schema means playlist membership not cascading
// and a playlist's seed track dangling instead of going NULL.
func TestForeignKeysHoldOnEveryPooledConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fk.db")

	database, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		CREATE TABLE parent (id TEXT PRIMARY KEY);
		CREATE TABLE child (parent_id TEXT REFERENCES parent(id) ON DELETE CASCADE);
	`); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	// Open several connections concurrently; database/sql keeps them all
	// alive afterwards, so the next query may be served by any of them.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := database.Conn(context.Background())
			if err != nil {
				return
			}
			defer conn.Close()
			if rows, err := conn.QueryContext(context.Background(), "SELECT 1"); err == nil {
				rows.Close()
			}
		}()
	}
	wg.Wait()

	enabled, err := ForeignKeysEnabled(database)
	if err != nil {
		t.Fatalf("ForeignKeysEnabled: %v", err)
	}
	if !enabled {
		t.Error("foreign_keys is off on a pooled connection; constraints are not enforced")
	}

	// The property that actually matters, asserted rather than assumed.
	if _, err := database.Exec(`INSERT INTO child (parent_id) VALUES ('no-such-parent');`); err == nil {
		t.Error("a foreign key violation was accepted")
	}

	if _, err := database.Exec(`INSERT INTO parent (id) VALUES ('p');`); err != nil {
		t.Fatalf("insert parent: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO child (parent_id) VALUES ('p');`); err != nil {
		t.Fatalf("insert child: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM parent WHERE id = 'p';`); err != nil {
		t.Fatalf("delete parent: %v", err)
	}

	var remaining int
	if err := database.QueryRow(`SELECT COUNT(*) FROM child;`).Scan(&remaining); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if remaining != 0 {
		t.Errorf("ON DELETE CASCADE did not fire: %d child row(s) survived", remaining)
	}
}

// TestBusyTimeoutIsSetPerConnection covers the second per-connection setting.
// Without it a writer that meets a lock fails with SQLITE_BUSY instead of
// waiting, which is the failure mode this project already hit once during
// wrapped playlist writes.
func TestBusyTimeoutIsSetPerConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.db")

	database, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := database.Conn(context.Background())
			if err != nil {
				return
			}
			defer conn.Close()
			if rows, err := conn.QueryContext(context.Background(), "SELECT 1"); err == nil {
				rows.Close()
			}
		}()
	}
	wg.Wait()

	var timeout int
	if err := database.QueryRow("PRAGMA busy_timeout;").Scan(&timeout); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if timeout <= 0 {
		t.Errorf("busy_timeout = %d, want a positive wait", timeout)
	}
}

// TestInMemoryDatabaseAlsoEnforcesForeignKeys guards the in-memory path, which
// the migration round-trip tests and every service test depends on.
func TestInMemoryDatabaseAlsoEnforcesForeignKeys(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	defer database.Close()

	enabled, err := ForeignKeysEnabled(database)
	if err != nil {
		t.Fatalf("ForeignKeysEnabled: %v", err)
	}
	if !enabled {
		t.Error("foreign_keys is off for an in-memory database")
	}
}

func TestDSNEscapesFilenamesThatLookLikeQueries(t *testing.T) {
	// A library or config path containing "?" or "#" must stay a path. If
	// it is not escaped, SQLite sees a query string and the database is
	// opened somewhere else entirely — or not at all.
	for _, name := range []string{"a.db", "with space.db", "we?ird.db", "trailing#hash.db"} {
		dir := t.TempDir()
		path := filepath.Join(dir, name)

		database, err := Open(path)
		if err != nil {
			t.Errorf("Open(%q): %v", name, err)
			continue
		}

		if _, err := database.Exec("CREATE TABLE t (id TEXT);"); err != nil {
			t.Errorf("create table in %q: %v", name, err)
			database.Close()
			continue
		}
		database.Close()

		if _, err := os.Stat(path); err != nil {
			t.Errorf("database was not created at %q: %v", path, err)
		}
	}
}
