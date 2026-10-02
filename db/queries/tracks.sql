-- name: CreateTrack :exec
INSERT INTO tracks (id, title, album, artist_id, duration_sec, relative_path, source_id, source, raw_title, performer, version)
VALUES (:id, :title, :album, :artist_id, :duration_sec, :relative_path, :source_id, :source, :raw_title, :performer, :version);

-- name: UpdateTrackTitle :exec
UPDATE tracks
SET title = :title, raw_title = :raw_title, performer = :performer, version = :version
WHERE id = :id;

-- name: GetTrack :one
SELECT sqlc.embed(tracks) FROM tracks
WHERE id = :id;

-- name: GetTracksByArtist :many
SELECT sqlc.embed(tracks) FROM tracks
WHERE artist_id = :artist_id;

-- name: GetTracksByPath :many
-- Ordered for a person, not for SQLite: without this the rows come back in
-- whatever order the table happens to yield, which is stable but arbitrary.
-- `artist_id IS NULL` sorts orphaned tracks last rather than first, since
-- NULL is the track with no artist to file it under. `id` breaks ties so two
-- tracks sharing a title keep a fixed relative order.
SELECT sqlc.embed(tracks) FROM tracks
WHERE relative_path LIKE :path
ORDER BY artist_id IS NULL, artist_id, title, id;

-- name: CheckTracksBySourceID :one
SELECT EXISTS(
    SELECT 1 FROM tracks
    WHERE source_id = :source_id
);

-- name: UpdateTrackPathsByPrefix :exec
-- Replaces only the leading prefix: SUBSTR keeps the remainder intact, so a
-- repeated folder name deeper in the path survives renames.
UPDATE tracks SET relative_path = :new_prefix || SUBSTR(relative_path, LENGTH(:old_prefix) + 1)
WHERE relative_path LIKE :match_prefix;

-- name: UpdateTrack :exec
UPDATE tracks
SET
    id = :new_id,
    title = :title,
    album = :album,
    artist_id = :artist_id,
    duration_sec = :duration_sec,
    relative_path = :relative_path,
    source_id = :source_id
WHERE id = :old_id;

-- name: DeleteTrack :execrows
DELETE FROM tracks
WHERE id = :id;

-- name: UpdateTrackPath :exec
UPDATE tracks SET relative_path = :relative_path
WHERE id = :id;

-- name: UpdateTrackDuration :exec
UPDATE tracks
SET duration_sec = :duration_sec
WHERE id = :id;

-- name: ListTracksMissingDuration :many
-- NULL is listed alongside 0 so the probe's candidates match what the UI
-- shows as unknown. They are the same fact; the check is defensive, not
-- because both values are expected.
SELECT sqlc.embed(tracks) FROM tracks
WHERE duration_sec IS NULL OR duration_sec = 0
ORDER BY id;
