package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yappachino-dev/yci/internal/appctx"
)

type SQLiteTransactor struct {
	db         *sql.DB
	postCommit func(ctx context.Context) error
}

func NewSQLiteTransactor(db *sql.DB) *SQLiteTransactor {
	return &SQLiteTransactor{db: db}
}

// SetPostCommit registers fn to run after each successful commit.
func (t *SQLiteTransactor) SetPostCommit(fn func(ctx context.Context) error) {
	t.postCommit = fn
}

func (t *SQLiteTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	var commited bool

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		} else if !commited && err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				err = fmt.Errorf("rollback transaction (original error: %v): %w", err, rbErr)
			}
		}
	}()

	txCtx := appctx.WithTx(ctx, tx)

	err = fn(txCtx)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	commited = true

	if t.postCommit != nil {
		if err := t.postCommit(ctx); err != nil {
			return fmt.Errorf("post-commit actions: %w", err)
		}
	}

	return nil
}
