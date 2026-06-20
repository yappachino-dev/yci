-- name: EnqueueDownload :execrows
INSERT OR IGNORE INTO download_queue (source_id, status)
VALUES (:source_id, 'pending');

-- name: GetQueue :many
SELECT id, source_id, status, attempts, error_msg
FROM download_queue;

-- name: GetQueueLength :one
SELECT COUNT(*) FROM download_queue;

-- name: GetNextPendingDownload :one
SELECT id, source_id, status, attempts, error_msg
FROM download_queue
WHERE status = 'pending' AND attempts < 3
ORDER BY created_at ASC
LIMIT 1;

-- name: StartDownload :exec
UPDATE download_queue
SET status = 'processing', attempts = attempts + 1, updated_at = CURRENT_TIMESTAMP
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
