package downloader

import (
	"context"
	"database/sql"

	"playlist-manager/internal/appctx"
	"playlist-manager/internal/db"
	"playlist-manager/internal/store"
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

func (r *SQLiteQueueRepo) NextPending(ctx context.Context) (QueueEntry, bool, error) {
	entry, err := r.db(ctx).GetNextPendingDownload(ctx)
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

func (r *SQLiteQueueRepo) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}
