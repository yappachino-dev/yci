package db

import (
	"context"
	"database/sql"
	"fmt"

	"playlist-manager/internal/appctx"
)

type SQLiteTransactor struct {
	db *sql.DB
}

func NewSQLiteTransactor(db *sql.DB) *SQLiteTransactor {
	return &SQLiteTransactor{db: db}
}

func (t *SQLiteTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx failed: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				err = fmt.Errorf("tx err: %v, rollback err: %w", err, rbErr)
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
		return fmt.Errorf("cannot commit transaction: %w", err)
	}

	return nil
}
