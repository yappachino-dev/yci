# Download Queue

The download queue is the one place where two yci processes can genuinely
collide. This is the contract that keeps that safe, and it is load-bearing:
a `serve` worker and a manual `download resume` are both ordinary ways to run
it, and they can overlap.

## The claim is the serialisation point

`StartDownload` is conditional:

```sql
UPDATE download_queue
SET status = 'processing', attempts = attempts + 1, updated_at = CURRENT_TIMESTAMP
WHERE id = :id AND status = 'pending';
```

It returns the affected row count, and a worker that gets zero rows **skips
the track**. It is not an error: another worker won the row between this run
reading the queue and this run reaching it.

Without the `status = 'pending'` guard, two workers that read the same pending
row both match it. Both increment `attempts`, and both run yt-dlp on the same
video — a duplicate download that also spends two retry attempts for one
success. SQLite serialising the write is not enough on its own; the condition
is what produces a single winner.

## Reclaiming a row is harder than claiming one

Nothing records which process owns a `processing` row, so the only evidence
that its owner is gone is age. `ResetOrphanedDownloads` therefore reclaims
only rows whose heartbeat is older than `orphanStaleAfter` (5 minutes).

Reclaiming every `processing` row unconditionally would be worse than
useless: a routine run starting while another is mid-download resets the live
row to `pending`, and both runs then fetch the same video. That is the same
duplicate as above, reached by a different route.

### The timestamp format is load-bearing

The column holds `CURRENT_TIMESTAMP` text (`2026-10-05 09:23:40`). A bound
`time.Time` arrives as Go's `Time.String()` (`2026-10-05 09:23:55.646901146
+0300 EEST m=-3599`), and SQLite compares those two lexically rather than
chronologically. Every same-day row then reads as stale and the guard inverts
into the bug it exists to prevent.

`stale_before` is therefore bound as a preformatted string built with
`sqliteTimestampLayout`, and the query casts it to text. `TestStalenessBound
ComparesRealTimestamps` pins this through the real query, because the failure
is silent: the code compiles, the tests pass, and crashes are simply never
recovered from.

### Heartbeat

A claimed row's `updated_at` is refreshed every 30 seconds while it downloads.
Without it the staleness window has to be wide enough to cover the slowest
conceivable download, which turns every crash into a queue that stays stuck
for that long. A dropped refresh is not fatal — it only makes the row look
older.

### Cancellation does not wait

A run that is cancelled knows it has stopped, so it re-arms its own row
immediately (`ReleaseDownload`) instead of leaving it to the next run's sweep.
Ctrl-C therefore does not make the track unavailable for five minutes. The
write uses `context.WithoutCancel`, because the cancelled context cannot be
used to write the release.

A crash has no such certainty, which is exactly why the window exists.

## What is deliberately absent

No advisory lock, no lease table, no `claimed_by` column. The single-user
case does not need one, and each would add a migration plus a failure mode to
a subsystem whose failure mode is a wasted download rather than data loss.
Revisit when a second worker becomes normal rather than incidental — a worker
per host, or several servers against a shared queue.

## Related

- `db/queries/queue.sql` — the queries and their reasoning, inline
- `internal/download/service.go` — `ProcessQueue`, `keepAlive`
- `docs/sources.md` — what feeds the queue