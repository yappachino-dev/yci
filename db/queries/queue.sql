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

-- name: StartDownload :exec
UPDATE download_queue
SET
    status = 'processing',
    attempts = attempts + 1,
    updated_at = CURRENT_TIMESTAMP
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
WHERE status = 'processing';

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
