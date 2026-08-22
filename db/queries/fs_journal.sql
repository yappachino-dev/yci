-- name: RecordFileAction :execrows
INSERT INTO fs_journal (op, src, dst)
VALUES (?1, ?2, ?3);

-- name: GetPendingFileActions :many
SELECT
    id,
    op,
    src,
    dst
FROM fs_journal
WHERE status = 'pending'
ORDER BY id ASC;

-- name: GetFailedFileActions :many
SELECT
    id,
    op,
    src,
    dst,
    error
FROM fs_journal
WHERE status = 'failed'
ORDER BY id ASC;

-- name: CompleteFileAction :exec
UPDATE fs_journal
SET status = 'done', error = NULL
WHERE id = ?1;

-- name: FailFileAction :exec
UPDATE fs_journal
SET status = 'failed', error = ?1
WHERE id = ?2;

-- name: ResetFailedFileActions :exec
UPDATE fs_journal
SET status = 'pending', error = NULL
WHERE status = 'failed';

-- name: PurgeCompletedFileActions :exec
DELETE FROM fs_journal
WHERE status = 'done';
