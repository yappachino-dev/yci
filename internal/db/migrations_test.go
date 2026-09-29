package db

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	"testing"

	_ "modernc.org/sqlite"
)

func openMigratedDB(t *testing.T) *sql.DB {
	t.Helper()

	dbConn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	if _, err := dbConn.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if err := SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}

	return dbConn
}

// TestMigrateIsQuietWhenCurrent keeps the common case silent: a command that
// finds nothing to apply must not print anything, or every invocation would
// carry migration noise.
func TestMigrateIsQuietWhenCurrent(t *testing.T) {
	dbConn := openMigratedDB(t)

	if err := RunMigrations(t.Context(), dbConn); err != nil {
		t.Fatalf("initial migration: %v", err)
	}

	var log bytes.Buffer
	applied, err := Migrate(t.Context(), dbConn, &log)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if applied != 0 {
		t.Errorf("applied = %d, want 0 on an up-to-date database", applied)
	}
	if log.Len() != 0 {
		t.Errorf("log = %q, want silence when nothing was applied", log.String())
	}
}

// TestSnapshotCopiesTheDatabase covers the file-backed path. An earlier
// version scanned two columns out of PRAGMA database_list's three, which
// made every command fail the moment a migration was pending.
func TestSnapshotCopiesTheDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	dbConn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer dbConn.Close()

	if err := SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}
	if err := RunMigrations(t.Context(), dbConn); err != nil {
		t.Fatalf("initial migration: %v", err)
	}

	if _, err := dbConn.Exec(`INSERT INTO folders (id, display_name) VALUES ('probe', 'Probe');`); err != nil {
		t.Fatalf("insert row: %v", err)
	}

	snapshotPath, err := Snapshot(dbConn, 0)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	snapshot := snapshotPath
	if snapshot != filepath.Join(dir, SnapshotName) {
		t.Errorf("first snapshot went to %q, want the default name", snapshot)
	}
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("snapshot not written beside the database: %v", err)
	}

	// The copy must be independently readable and complete.
	copyConn, err := sql.Open("sqlite", snapshot)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer copyConn.Close()

	var got string
	if err := copyConn.QueryRow(`SELECT display_name FROM folders WHERE id = 'probe'`).Scan(&got); err != nil {
		t.Fatalf("read from snapshot: %v", err)
	}
	if got != "Probe" {
		t.Errorf("snapshot display_name = %q, want %q", got, "Probe")
	}

	var integrity string
	if err := copyConn.QueryRow("PRAGMA integrity_check;").Scan(&integrity); err != nil {
		t.Fatalf("integrity check: %v", err)
	}
	if integrity != "ok" {
		t.Errorf("snapshot integrity = %q, want ok", integrity)
	}
}

func TestSnapshotSkipsInMemory(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory database: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	if err := SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}
	if err := RunMigrations(t.Context(), dbConn); err != nil {
		t.Fatalf("migrate in-memory database: %v", err)
	}

	// An in-memory database has no file to copy; this must be a no-op
	// rather than an error, since the test suite migrates in memory.
	path, err := Snapshot(dbConn, 0)
	if err != nil {
		t.Errorf("Snapshot on an in-memory database: %v", err)
	}
	if path != "" {
		t.Errorf("in-memory Snapshot reported path %q, want none", path)
	}
}

func TestPendingMigrationsCountsForward(t *testing.T) {
	dbConn := openMigratedDB(t)

	before, err := PendingMigrations(dbConn)
	if err != nil {
		t.Fatalf("PendingMigrations before applying: %v", err)
	}

	if err := RunMigrations(t.Context(), dbConn); err != nil {
		t.Fatalf("initial migration: %v", err)
	}

	after, err := PendingMigrations(dbConn)
	if err != nil {
		t.Fatalf("PendingMigrations after applying: %v", err)
	}

	if len(after) != 0 {
		t.Errorf("pending after migrating = %d, want 0", len(after))
	}
	if len(before) == 0 {
		t.Fatal("pending before migrating = 0, want every embedded migration")
	}

	// Oldest first, so applying them in order cannot invert.
	for i := 1; i < len(before); i++ {
		if before[i].Version <= before[i-1].Version {
			t.Fatalf("migrations out of order: %d follows %d", before[i].Version, before[i-1].Version)
		}
	}
}

// TestSnapshotTwiceDoesNotBlock is the regression test for a failure that
// bricked the CLI: VACUUM INTO refuses to write onto an existing file, and
// that was treated as fatal, so the sequence "migrate once, get interrupted,
// run again" left every later run blocked before it reached the migrations.
// A recovery mechanism that can itself become the outage is worse than none.
func TestSnapshotTwiceDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	dbConn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer dbConn.Close()

	if err := SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}
	if err := RunMigrations(t.Context(), dbConn); err != nil {
		t.Fatalf("initial migration: %v", err)
	}

	first, err := Snapshot(dbConn, 8)
	if err != nil {
		t.Fatalf("first Snapshot: %v", err)
	}

	second, err := Snapshot(dbConn, 10)
	if err != nil {
		t.Fatalf("second Snapshot blocked: %v", err)
	}

	if second == first {
		t.Errorf("second snapshot overwrote the first: both %q", first)
	}

	// Both must survive and be readable: the earlier one is the only record
	// of the state before migrations that have already landed.
	for _, p := range []string{first, second} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("snapshot %q missing: %v", p, err)
		}
		conn, err := sql.Open("sqlite", p)
		if err != nil {
			t.Fatalf("open %q: %v", p, err)
		}
		var check string
		if err := conn.QueryRow("PRAGMA integrity_check;").Scan(&check); err != nil {
			t.Errorf("read %q: %v", p, err)
		} else if check != "ok" {
			t.Errorf("integrity of %q: %s", p, check)
		}
		conn.Close()
	}

	if got := filepath.Base(second); got != "pre-migration-v10.bak.db" {
		t.Errorf("second snapshot named %q, want a version-qualified name", got)
	}
}

// TestMigrateIsNotBlockedByAnExistingSnapshot covers the end-to-end symptom:
// a database with pending migrations must still migrate when a snapshot from
// an earlier run is sitting beside it.
func TestMigrateIsNotBlockedByAnExistingSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	// A leftover snapshot beside a database that still has migrations to
	// apply: exactly the state that bricked the CLI.
	if err := os.WriteFile(filepath.Join(dir, SnapshotName), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbConn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer dbConn.Close()

	if err := SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}

	var out bytes.Buffer
	applied, err := Migrate(t.Context(), dbConn, &out)
	if err != nil {
		t.Fatalf("Migrate blocked by an existing snapshot: %v", err)
	}
	if applied == 0 {
		t.Fatal("Migrate applied nothing despite pending migrations")
	}

	// The pre-existing file must not have been silently clobbered: it is
	// the only record of the state before the migrations that already ran.
	if b, err := os.ReadFile(filepath.Join(dir, SnapshotName)); err != nil {
		t.Error("the pre-existing snapshot was destroyed")
	} else if string(b) != "stale" {
		t.Error("the pre-existing snapshot was overwritten")
	}

	// A new snapshot must have been written alongside it, and the report
	// must name the file actually written rather than the default name.
	versioned := filepath.Join(dir, "pre-migration-v0.bak.db")
	if _, err := os.Stat(versioned); err != nil {
		t.Errorf("no versioned snapshot written: %v", err)
	}

	if msg := out.String(); !strings.Contains(msg, "pre-migration-v") {
		t.Errorf("migration report does not name the snapshot it wrote: %q", msg)
	}
}
