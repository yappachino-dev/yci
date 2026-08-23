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
FROM download_queue;

-- name: GetNextActionableDownload :one
SELECT
    download_queue.id,
    download_queue.source_id,
    download_queue.status,
    download_queue.attempts,
    download_queue.error_msg
FROM download_queue
WHERE
    download_queue.status IN ('pending', 'failed')
    AND download_queue.attempts < 3
ORDER BY COALESCE(download_queue.updated_at, download_queue.created_at) ASC
LIMIT 1;

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
UPDATE download_queue
SET status = 'failed', error_msg = :error_msg, updated_at = CURRENT_TIMESTAMP
WHERE id = :id;

-- name: ResetOrphanedDownloads :exec
UPDATE download_queue
SET status = 'pending', updated_at = CURRENT_TIMESTAMP
WHERE status = 'processing';

-- name: ResetHardFailedDownloads :execrows
UPDATE download_queue
SET
    status = 'pending',
    attempts = 0,
    error_msg = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE status = 'failed' AND attempts >= 3;

-- name: ResetDownloadByID :execrows
UPDATE download_queue
SET
    status = 'pending',
    attempts = 0,
    error_msg = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = :id AND status = 'failed';
