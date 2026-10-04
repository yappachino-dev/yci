package library

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yappachino-dev/yci/internal/appctx"
	"github.com/yappachino-dev/yci/internal/db"
	"github.com/yappachino-dev/yci/internal/store"
)

// TrackEventOp names a lifecycle event in the append-only track journal.
type TrackEventOp string

const (
	// TrackEventImported records a track entering the library.
	TrackEventImported TrackEventOp = "imported"
	// TrackEventDeleted records a track leaving it. The only op read back: it
	// is what keeps a deletion a decision rather than an accident, so a
	// playlist still offering the video does not download it again.
	TrackEventDeleted TrackEventOp = "deleted"
)

type TrackJournal interface {
	Record(ctx context.Context, trackID TrackID, sourceID VideoID, op TrackEventOp) error
	IsRetired(ctx context.Context, sourceID string) (bool, error)
}

// TrackJournalService records and reads the track journal.
type TrackJournalService struct {
	repo TrackJournal
}

func NewTrackJournalService(repo TrackJournal) *TrackJournalService {
	return &TrackJournalService{repo: repo}
}

// Record appends an event. Append-only by design: a delete is a new row and
// never an update, so the history of what was removed is itself the record.
func (s *TrackJournalService) Record(ctx context.Context, trackID TrackID, sourceID VideoID, op TrackEventOp) error {
	if err := s.repo.Record(ctx, trackID, sourceID, op); err != nil {
		return fmt.Errorf("record track event %q: %w", op, err)
	}

	return nil
}

// IsRetired reports whether the video was ever deleted from the library.
//
// A string rather than VideoID because the caller is the download queue's
// retirement seam, which works in plain video IDs the same way the adjacent
// TrackRepository.ExistsBySourceID does.
func (s *TrackJournalService) IsRetired(ctx context.Context, sourceID string) (bool, error) {
	retired, err := s.repo.IsRetired(ctx, sourceID)
	if err != nil {
		return false, fmt.Errorf("check retired source %q: %w", sourceID, err)
	}

	return retired, nil
}

// SQLiteTrackJournal implements TrackJournal over sqlc queries.
type SQLiteTrackJournal struct {
	q *store.Queries
}

func NewSQLiteTrackJournal(q *store.Queries) *SQLiteTrackJournal {
	return &SQLiteTrackJournal{q: q}
}

func (j *SQLiteTrackJournal) Record(ctx context.Context, trackID TrackID, sourceID VideoID, op TrackEventOp) error {
	return j.db(ctx).RecordTrackEvent(ctx, store.RecordTrackEventParams{
		TrackID:  string(trackID),
		SourceID: db.ToNullString(string(sourceID)),
		Op:       string(op),
	})
}

func (j *SQLiteTrackJournal) IsRetired(ctx context.Context, sourceID string) (bool, error) {
	return j.db(ctx).IsSourceRetired(ctx, db.ToNullString(sourceID))
}

func (j *SQLiteTrackJournal) db(ctx context.Context) *store.Queries {
	if tx, ok := appctx.TxFromContext[*sql.Tx](ctx); ok {
		return j.q.WithTx(tx)
	}

	return j.q
}
