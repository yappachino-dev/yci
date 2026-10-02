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
UPDATE download_queue
SET
    status = CASE
        WHEN attempts >= :max_attempts THEN 'failed'
        ELSE 'pending'
    END,
    error_msg = CASE
        WHEN attempts >= :max_attempts THEN 'interruped while processing (attempts exhausted)'
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
