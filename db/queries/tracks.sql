-- name: CreateTrack :exec
INSERT INTO tracks (id, title, album, artist_id, duration_sec, relative_path, source_id)
VALUES (:id, :title, :album, :artist_id, :duration_sec, :relative_path, :source_id);

-- name: GetTrack :one
SELECT id, title, album, artist_id, duration_sec, relative_path, source_id
FROM tracks
WHERE id = :id;

-- name: GetTracksByArtist :many
SELECT id, title, album, artist_id, duration_sec, relative_path, source_id
FROM tracks
WHERE artist_id = :artist_id;

-- name: GetTracksByPath :many
SELECT id, title, album, artist_id, duration_sec, relative_path, source_id
FROM tracks
WHERE relative_path LIKE :path;

-- name: CheckTracksBySourceID :one
SELECT EXISTS (
    SELECT 1 FROM tracks where source_id = :source_id
);

-- name: UpdateTrackPathsByPrefix :exec
UPDATE tracks
SET relative_path = REPLACE(relative_path, :old_prefix, :new_prefix)
WHERE relative_path LIKE :match_prefix;

-- name: UpdateTrack :exec
UPDATE tracks
SET id = :new_id,
    title = :title,
    album = :album,
    artist_id = :artist_id,
    duration_sec = :duration_sec,
    relative_path = :relative_path,
    source_id = :source_id
WHERE id = :old_id;

-- name: DeleteTrack :exec
DELETE FROM tracks
WHERE id = :id;

-- name: UpdateTrackPath :exec
UPDATE tracks
SET relative_path = :relative_path
WHERE id = :id;
