package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/yappachino-dev/yci/internal/appctx"
)

// txFromCtx resolves the active transaction exactly like the repositories
// do; executing statements directly on dbConn would bypass the transaction
// via the connection pool.
func txFromCtx(t *testing.T, ctx context.Context) *sql.Tx {
	t.Helper()

	tx, ok := appctx.TxFromContext[*sql.Tx](ctx)
	if !ok {
		t.Fatal("no transaction in context")
	}

	return tx
}

func newTransactorTestDB(t *testing.T) (*sql.DB, *SQLiteTransactor) {
	t.Helper()

	dbConn, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { dbConn.Close() })

	if _, err := dbConn.Exec(`CREATE TABLE kv (k TEXT PRIMARY KEY, v TEXT NOT NULL)`); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return dbConn, NewSQLiteTransactor(dbConn)
}

func mustCount(t *testing.T, dbConn *sql.DB) int {
	t.Helper()

	var n int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM kv`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}

	return n
}

func TestNestedTransactionJoinsOuterUnit(t *testing.T) {
	dbConn, txr := newTransactorTestDB(t)
	ctx := context.Background()

	hooks := 0
	txr.SetPostCommit(func(ctx context.Context) error {
		hooks++
		return nil
	})

	err := txr.WithinTransaction(ctx, func(outerCtx context.Context) error {
		outer := txFromCtx(t, outerCtx)
		if _, err := outer.Exec(`INSERT INTO kv (k, v) VALUES ('outer', '1')`); err != nil {
			return err
		}

		return txr.WithinTransaction(outerCtx, func(innerCtx context.Context) error {
			inner := txFromCtx(t, innerCtx)

			// The outer write must be visible inside the joined
			// transaction.
			var v string
			if err := inner.QueryRow(`SELECT v FROM kv WHERE k = 'outer'`).Scan(&v); err != nil {
				return err
			}

			_, err := inner.Exec(`INSERT INTO kv (k, v) VALUES ('inner', '2')`)
			return err
		})
	})
	if err != nil {
		t.Fatalf("nested transaction: %v", err)
	}

	if got := mustCount(t, dbConn); got != 2 {
		t.Errorf("rows = %d, want 2 (outer and inner writes committed together)", got)
	}
	if hooks != 1 {
		t.Errorf("post-commit hooks fired %d times, want 1 (inner joins, only the outer unit commits)", hooks)
	}
}

func TestInnerFailureRollsBackWholeUnit(t *testing.T) {
	dbConn, txr := newTransactorTestDB(t)
	ctx := context.Background()

	boom := errors.New("inner failure")

	err := txr.WithinTransaction(ctx, func(outerCtx context.Context) error {
		outer := txFromCtx(t, outerCtx)
		if _, err := outer.Exec(`INSERT INTO kv (k, v) VALUES ('outer', '1')`); err != nil {
			return err
		}

		return txr.WithinTransaction(outerCtx, func(innerCtx context.Context) error {
			inner := txFromCtx(t, innerCtx)
			if _, err := inner.Exec(`INSERT INTO kv (k, v) VALUES ('inner', '2')`); err != nil {
				return err
			}

			return boom
		})
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}

	if got := mustCount(t, dbConn); got != 0 {
		t.Errorf("rows = %d, want 0 (inner failure must discard outer writes too)", got)
	}
}

func TestPlainTransactionCommitAndRollback(t *testing.T) {
	dbConn, txr := newTransactorTestDB(t)
	ctx := context.Background()

	if err := txr.WithinTransaction(ctx, func(txCtx context.Context) error {
		tx := txFromCtx(t, txCtx)
		_, err := tx.Exec(`INSERT INTO kv (k, v) VALUES ('a', '1')`)
		return err
	}); err != nil {
		t.Fatalf("committing transaction: %v", err)
	}
	if got := mustCount(t, dbConn); got != 1 {
		t.Errorf("rows = %d, want 1 after commit", got)
	}

	want := errors.New("rollback me")
	if err := txr.WithinTransaction(ctx, func(txCtx context.Context) error {
		tx := txFromCtx(t, txCtx)
		if _, err := tx.Exec(`INSERT INTO kv (k, v) VALUES ('b', '2')`); err != nil {
			return err
		}
		return want
	}); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if got := mustCount(t, dbConn); got != 1 {
		t.Errorf("rows = %d, want 1 after rollback", got)
	}
}
