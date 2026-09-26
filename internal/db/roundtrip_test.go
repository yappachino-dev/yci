package db

import (
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMigrationsRoundTrip applies every embedded migration and then rolls
// each one back.
//
// Nothing else checks the Down sections, and they are the only automated
// check they have: the CLI has no schema-down command, on the grounds that
// rolling a live database back discards the rows written since the migration
// applied. That safety argument only holds if Down is correct, so this test
// is what makes it true rather than assumed. Restoring a database copy is
// the supported way back; see `make sandbox`.
func TestMigrationsRoundTrip(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	if err := SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}
	if err := RunMigrations(t.Context(), dbConn); err != nil {
		t.Fatalf("apply all migrations: %v", err)
	}

	applied, err := GetCurrentVersion(t.Context(), dbConn)
	if err != nil {
		t.Fatalf("read applied version: %v", err)
	}
	if applied == 0 {
		t.Fatal("no migrations were applied; the embed is not wired up")
	}

	all, err := goose.CollectMigrations(".", 0, 1<<63-1)
	if err != nil {
		t.Fatalf("collect migrations: %v", err)
	}
	if len(all) != applied {
		t.Fatalf("collected %d migrations but version is %d", len(all), applied)
	}

	for target := applied - 1; target >= 0; target-- {
		if err := goose.DownToContext(t.Context(), dbConn, ".", int64(target)); err != nil {
			t.Fatalf("roll back to version %d: %v", target, err)
		}

		got, err := GetCurrentVersion(t.Context(), dbConn)
		if err != nil {
			t.Fatalf("read version after rollback: %v", err)
		}
		if got != target {
			t.Fatalf("version after rollback = %d, want %d", got, target)
		}
	}
}

// TestMigrationsReapplyCleanly catches a Down that leaves the schema in a
// state Up cannot build on — a dropped index, or a table recreated with
// different columns.
func TestMigrationsReapplyCleanly(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	if err := SetUpMigrationContext(); err != nil {
		t.Fatalf("set up migrations: %v", err)
	}
	if err := RunMigrations(t.Context(), dbConn); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	applied, err := GetCurrentVersion(t.Context(), dbConn)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}

	if err := goose.DownToContext(t.Context(), dbConn, ".", 0); err != nil {
		t.Fatalf("roll back everything: %v", err)
	}

	if err := RunMigrations(t.Context(), dbConn); err != nil {
		t.Fatalf("re-apply after full rollback: %v", err)
	}

	again, err := GetCurrentVersion(t.Context(), dbConn)
	if err != nil {
		t.Fatalf("read version after re-apply: %v", err)
	}
	if again != applied {
		t.Errorf("version after re-apply = %d, want %d", again, applied)
	}
}
