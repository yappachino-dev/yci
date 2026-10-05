package download

import (
	"context"
	"testing"
	"time"
)

// TestOnlyOneWorkerCanClaimARow is the regression this file exists for.
//
// Start used to be `WHERE id = :id`, so two workers that read the same
// pending row both matched it: both incremented attempts and both ran yt-dlp
// on the same video. The claim is the serialisation point, so it has to be
// conditional.
func TestOnlyOneWorkerCanClaimARow(t *testing.T) {
	repo, _ := newQueueRepo(t)
	ctx := context.Background()

	mustEnqueue(t, repo, "contended")
	entry, _ := nextPending(t, repo)

	first, err := repo.Start(ctx, entry.ID)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !first {
		t.Fatal("the first worker could not claim a pending row")
	}

	second, err := repo.Start(ctx, entry.ID)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if second {
		t.Error("a second worker claimed a row that was already processing")
	}

	// The loser must not have spent the winner's retry budget.
	if got := entryBySource(t, repo, "contended").Attempts; got != 1 {
		t.Errorf("attempts = %d, want 1: only the winning claim counts", got)
	}
}

// TestResetPendingLeavesARowBeingDownloadedAlone is the other half.
//
// Reset used to reclaim every 'processing' row, so a routine run starting
// while another was mid-download reset the live row to pending -- and both
// runs then fetched the same video.
func TestResetPendingLeavesARowBeingDownloadedAlone(t *testing.T) {
	repo, conn := newQueueRepo(t)
	ctx := context.Background()

	mustEnqueue(t, repo, "inflight")
	entry, _ := nextPending(t, repo)

	if _, err := repo.Start(ctx, entry.ID); err != nil {
		t.Fatal(err)
	}

	// Fresh: the heartbeat is recent, so this row belongs to a live worker.
	if err := repo.ResetPending(ctx, maxAttempts); err != nil {
		t.Fatal(err)
	}

	after := entryBySource(t, repo, "inflight")
	if after.Status != QueueStatusProcessing {
		t.Errorf("status = %q, want processing: a live download was reclaimed", after.Status)
	}
	if after.Attempts != 1 {
		t.Errorf("attempts = %d, want 1: a live download had its attempt given back", after.Attempts)
	}

	// Aged: no heartbeat, so the worker behind it is gone.
	ageRow(t, conn, entry.ID, time.Hour)

	if err := repo.ResetPending(ctx, maxAttempts); err != nil {
		t.Fatal(err)
	}

	reaped := entryBySource(t, repo, "inflight")
	if reaped.Status != QueueStatusPending {
		t.Errorf("status = %q, want pending: an abandoned row was not reclaimed", reaped.Status)
	}
	if reaped.Attempts != 0 {
		t.Errorf("attempts = %d, want 0: the interrupted attempt was not given back", reaped.Attempts)
	}
}

// TestStalenessBoundComparesRealTimestamps exercises the bound through the
// real query rather than through SQL written beside it, which would only test
// itself.
//
// The trap this covers: the column holds CURRENT_TIMESTAMP text while a bound
// time.Time arrives as Go's Time.String(), which SQLite cannot compare. The
// condition then yields NULL for every row, ResetPending silently reclaims
// nothing, and the only symptom is that crashes are never recovered from.
func TestStalenessBoundComparesRealTimestamps(t *testing.T) {
	repo, conn := newQueueRepo(t)
	ctx := context.Background()

	mustEnqueue(t, repo, "aged")
	entry, _ := nextPending(t, repo)

	if _, err := repo.Start(ctx, entry.ID); err != nil {
		t.Fatal(err)
	}

	// Inside the window: still plausibly live, so it must be left alone.
	ageRow(t, conn, entry.ID, orphanStaleAfter-time.Minute)

	if err := repo.ResetPending(ctx, maxAttempts); err != nil {
		t.Fatal(err)
	}
	if got := entryBySource(t, repo, "aged").Status; got != QueueStatusProcessing {
		t.Fatalf("status = %q, want processing inside the staleness window", got)
	}

	// Past the window: the worker behind it is gone, so it has to come back.
	ageRow(t, conn, entry.ID, orphanStaleAfter+time.Minute)

	if err := repo.ResetPending(ctx, maxAttempts); err != nil {
		t.Fatal(err)
	}
	if got := entryBySource(t, repo, "aged").Status; got != QueueStatusPending {
		t.Errorf("status = %q, want pending past the staleness window", got)
	}
}
