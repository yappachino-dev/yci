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
SELECT sqlc.embed(tracks) FROM tracks
WHERE relative_path LIKE :path;

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
