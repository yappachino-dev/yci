-- name: EnqueueDownload :execrows
INSERT OR IGNORE INTO download_queue (source_id, status)
VALUES (:source_id, 'pending');

-- name: GetQueue :many
SELECT
    id,
    source_id,
    status,
    attempts,
    error_msg
FROM download_queue
ORDER BY COALESCE(updated_at, created_at) ASC;

-- name: StartDownload :execrows
-- Conditional on 'pending', because this claim is what serialises two
-- workers. Both can read the same pending row, and an unconditional update
-- would match for both: each would increment attempts, and both would run
-- yt-dlp on the same video. Zero rows means someone else won it.
UPDATE download_queue
SET
    status = 'processing',
    attempts = attempts + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = :id
  AND status = 'pending';

-- name: ReleaseDownload :exec
-- Re-arms a row this process is giving up on, giving back the attempt it
-- never spent. Used when the run is cancelled: the process knows it has
-- stopped, so it does not have to wait out the staleness window that a
-- crash has to.
UPDATE download_queue
SET
    attempts = MAX(attempts - 1, 0),
    status = 'pending',
    updated_at = CURRENT_TIMESTAMP
WHERE id = :id;

-- name: TouchDownload :exec
-- Refreshes the heartbeat of a row being downloaded. Without it a long
-- download is indistinguishable from a run that died holding it, and the
-- staleness bound below has to be a guess wide enough to be safe.
UPDATE download_queue
SET updated_at = CURRENT_TIMESTAMP
WHERE id = :id;

-- name: CompleteDownload :exec
DELETE FROM download_queue
WHERE id = :id;

-- name: FailDownload :exec
UPDATE download_queue SET
    status = 'failed',
    error_msg = :error_msg,
    updated_at = CURRENT_TIMESTAMP
WHERE id = :id;

-- name: ResetOrphanedDownloads :exec
-- An orphaned row is one left 'processing' by a run that was interrupted
-- (Ctrl-C, crash, SIGTERM) rather than one that failed: a failure goes
-- through Fail and lands in 'failed'. Interrupting is not an attempt the
-- track spent, so attempts is given back. Without this, three interrupts
-- exhaust the retry budget and fail a track that never errored.
--
-- The CASE reads the pre-update attempts, so the status decision and the
-- decremented counter stay consistent.
UPDATE download_queue
SET
    attempts = MAX(attempts - 1, 0),
    status = CASE
        WHEN attempts - 1 >= :max_attempts THEN 'failed'
        ELSE 'pending'
    END,
    error_msg = CASE
        WHEN attempts - 1 >= :max_attempts THEN 'interrupted while processing (attempts exhausted)'
        ELSE error_msg
    END,
    updated_at = CURRENT_TIMESTAMP
WHERE status = 'processing'
  -- Age is the only evidence that the process holding this row is gone.
  -- Without the bound a routine run reclaims rows another run is actively
  -- downloading, which is the double-download this guards against.
  --
  -- :stale_before is bound as a preformatted string, not a time.Time. The
  -- driver renders a time.Time as Go's Time.String() ("2026-10-05 11:26:55
  -- +0300 EEST m=-3599"), which SQLite cannot parse or compare against the
  -- column's CURRENT_TIMESTAMP text. Passing one silently makes this
  -- comparison NULL, i.e. nothing is ever reclaimed again.
  AND updated_at < CAST(sqlc.arg(stale_before) AS TEXT);

-- name: ResetHardFailedDownloads :execrows
UPDATE download_queue
SET
    status = 'pending',
    attempts = 0,
    error_msg = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE status = 'failed' AND attempts >= :max_attempts;

-- name: ResetDownloadByID :execrows
UPDATE download_queue
SET
    status = 'pending',
    attempts = 0,
    error_msg = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = :id;

-- name: UpdateDownloadStatus :exec
UPDATE download_queue
SET
    status = :status,
    error_msg = :error_msg,
    updated_at = CURRENT_TIMESTAMP
WHERE id = :id;
