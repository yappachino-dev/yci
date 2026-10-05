-- name: CreateSource :exec
-- The url is unique so the same playlist cannot be watched twice under two
-- names, which would double every download it offers.
INSERT INTO playlist_sources (name, url, channels)
VALUES (:name, :url, :channels);

-- name: GetSourceByName :one
SELECT
    id,
    name,
    url,
    channels
FROM playlist_sources
WHERE name = :name;

-- name: GetSources :many
SELECT
    id,
    name,
    url,
    channels
FROM playlist_sources
ORDER BY name;

-- name: SetSourceChannels :execrows
-- Rewrites the saved selection in place. The selection is a preference, not a
-- history, so this is a mutation rather than a journal entry; the rows it
-- came from are the playlist's own entries and are refetched each poll.
UPDATE playlist_sources
SET channels = :channels,
    updated_at = CURRENT_TIMESTAMP
WHERE name = :name;

-- name: RenameSource :execrows
-- Only the label moves. The url and the channel selection describe the same
-- watched playlist either way, so a rename must not disturb them.
UPDATE playlist_sources
SET name = :new_name,
    updated_at = CURRENT_TIMESTAMP
WHERE name = :name;

-- name: DeleteSource :execrows
DELETE FROM playlist_sources
WHERE name = :name;

-- name: RecordTrackEvent :exec
-- Append-only. A deletion is a new entry and never an update, so the record
-- of what was removed survives being re-added.
INSERT INTO track_journal (track_id, source_id, op)
VALUES (:track_id, :source_id, :op);

-- name: IsSourceRetired :one
-- True when the video was deleted from the library at some point. Presence of
-- any delete row is the signal, not the most recent one: a track that is
-- removed again after being re-downloaded is a track the user has now deleted
-- twice, and honouring only the newest event would quietly undo that.
SELECT EXISTS (
    SELECT 1
    FROM track_journal
    WHERE source_id = :source_id AND op = 'deleted'
);