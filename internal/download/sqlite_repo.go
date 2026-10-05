package download

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
			Status:   QueueStatus(r.Status),
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

// orphanStaleAfter is how long a row may sit in 'processing' before another
// run treats it as abandoned. It has to exceed any single download, because
// resetting a row that is actively downloading causes both runs to fetch the
// same video; TouchDownload keeps the row's age fresh so this stays a
// bound on a dead process rather than on a slow one.
const orphanStaleAfter = 5 * time.Minute

// sqliteTimestampLayout is the format SQLite's CURRENT_TIMESTAMP writes.
// A lexicographic comparison is only correct when both sides use it.
const sqliteTimestampLayout = "2006-01-02 15:04:05"

// ResetPending reclaims rows left 'processing' by an interrupted run.
//
// Only rows stale past orphanStaleAfter: age is the sole evidence that the
// process holding one is gone, so a run that started a moment ago must not
// reclaim rows a slower run is still downloading.
func (r *SQLiteQueueRepo) ResetPending(ctx context.Context, maxAttempts int) error {
	// Formatted to match CURRENT_TIMESTAMP, which is what the column holds.
	// See the query comment: binding a time.Time here is not merely ugly, it
	// silently disables the reclaim.
	staleBefore := time.Now().Add(-orphanStaleAfter).UTC().Format(sqliteTimestampLayout)

	return r.db(ctx).ResetOrphanedDownloads(ctx, store.ResetOrphanedDownloadsParams{
		MaxAttempts: int64(maxAttempts),
		StaleBefore: staleBefore,
	})
}

// Start claims a row for this worker, reporting whether it actually won.
//
// A false return is not an error: it means another worker claimed the row
// between this run reading the queue and this run reaching it.
func (r *SQLiteQueueRepo) Start(ctx context.Context, id int) (bool, error) {
	rows, err := r.db(ctx).StartDownload(ctx, int64(id))
	if err != nil {
		return false, err
	}

	return rows > 0, nil
}

// Release re-arms a row the caller is knowingly abandoning.
func (r *SQLiteQueueRepo) Release(ctx context.Context, id int) error {
	return r.db(ctx).ReleaseDownload(ctx, int64(id))
}

// Touch refreshes the heartbeat of a row this worker is downloading.
func (r *SQLiteQueueRepo) Touch(ctx context.Context, id int) error {
	return r.db(ctx).TouchDownload(ctx, int64(id))
}

// Complete removes a finished entry from the queue.
//
// It deletes rather than marking it done: the schema allows only pending,
// processing and failed, so there is nowhere to record a completed state. The
// trade is that a finished download leaves no trace, which also means
// `download retry <id>` cannot refer to one — a retry is for failures only.
func (r *SQLiteQueueRepo) Complete(ctx context.Context, id int) error {
	return r.db(ctx).CompleteDownload(ctx, int64(id))
}

func (r *SQLiteQueueRepo) Fail(ctx context.Context, id int, status string, err error) error {
	return r.db(ctx).UpdateDownloadStatus(ctx, store.UpdateDownloadStatusParams{
		ID:       int64(id),
		Status:   status,
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
func (r *SQLiteQueueRepo) RetryHardFailed(ctx context.Context, maxAttempts int) (int64, error) {
	rows, err := r.db(ctx).ResetHardFailedDownloads(ctx, int64(maxAttempts))
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
