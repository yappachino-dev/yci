package download

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yappachino-dev/yci/internal/appctx"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/store"
)

type SQLiteQueueRepo struct {
	q *store.Queries
}

func NewSQLiteQueueRepo(q *store.Queries) *SQLiteQueueRepo {
	return &SQLiteQueueRepo{q: q}
}

func (r *SQLiteQueueRepo) All(ctx context.Context) ([]QueueEntry, error) {
	rows, err := r.db(ctx).GetQueue(ctx)
	if err != nil {
		return nil, err
	}

	entries := make([]QueueEntry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, QueueEntry{
			ID:       int(r.ID),
			SourceID: r.SourceID,
			Status:   r.Status,
			Attempts: int(r.Attempts),
			ErrorMsg: db.FromNullString(r.ErrorMsg),
		})
	}

	return entries, nil
}

func (r *SQLiteQueueRepo) Enqueue(ctx context.Context, sourceID string) (bool, error) {
	rows, err := r.db(ctx).EnqueueDownload(ctx, sourceID)
	if err != nil {
		return false, err
	}

	return rows > 0, nil
}

func (r *SQLiteQueueRepo) QueueLength(ctx context.Context) (int, error) {
	length, err := r.db(ctx).GetQueueLength(ctx)
	if err != nil {
		return 0, err
	}

	return int(length), nil
}

func (r *SQLiteQueueRepo) ResetPending(ctx context.Context) error {
	return r.db(ctx).ResetOrphanedDownloads(ctx)
}

func (r *SQLiteQueueRepo) Next(ctx context.Context) (QueueEntry, bool, error) {
	entry, err := r.db(ctx).GetNextActionableDownload(ctx)
	if err == sql.ErrNoRows {
		return QueueEntry{}, false, nil
	}
	if err != nil {
		return QueueEntry{}, false, err
	}

	return QueueEntry{
		ID:       int(entry.ID),
		SourceID: entry.SourceID,
		Status:   entry.Status,
		Attempts: int(entry.Attempts),
		ErrorMsg: db.FromNullString(entry.ErrorMsg),
	}, true, nil
}

func (r *SQLiteQueueRepo) Start(ctx context.Context, id int) error {
	return r.db(ctx).StartDownload(ctx, int64(id))
}

func (r *SQLiteQueueRepo) Complete(ctx context.Context, id int) error {
	return r.db(ctx).CompleteDownload(ctx, int64(id))
}

func (r *SQLiteQueueRepo) Fail(ctx context.Context, id int, err error) error {
	return r.db(ctx).FailDownload(ctx, store.FailDownloadParams{
		ID:       int64(id),
		ErrorMsg: db.ToNullString(err.Error()),
	})
}

// RetryByID re-arms a single failed queue entry; reports whether an entry
// with that ID existed and was failed.
func (r *SQLiteQueueRepo) RetryByID(ctx context.Context, id int) (bool, error) {
	rows, err := r.db(ctx).ResetDownloadByID(ctx, int64(id))
	if err != nil {
		return false, fmt.Errorf("retry queue entry %d: %w", id, err)
	}

	return rows > 0, nil
}

// RetryHardFailed re-arms every entry that exhausted its attempts.
func (r *SQLiteQueueRepo) RetryHardFailed(ctx context.Context) (int64, error) {
	rows, err := r.db(ctx).ResetHardFailedDownloads(ctx)
	if err != nil {
		return 0, fmt.Errorf("retry hard-failed queue entries: %w", err)
	}

	return rows, nil
}

func (r *SQLiteQueueRepo) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}
