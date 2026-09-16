-- name: CreatePlaylist :one
INSERT INTO playlists (name)
VALUES (:name)
RETURNING id, name;

-- name: GetPlaylists :many
SELECT
    id,
    name
FROM playlists
ORDER BY name;

-- name: GetPlaylistByID :one
SELECT
    id,
    name
FROM playlists
WHERE id = :id;

-- name: GetPlaylistByName :one
SELECT
    id,
    name
FROM playlists
WHERE name = :name;

-- name: DeletePlaylist :execrows
DELETE FROM playlists
WHERE id = :id;

-- Appending computes the next position inside the statement, so concurrent
-- adds within separate transactions cannot collide on the position PK.
-- The UNIQUE (playlist_id, track_id) constraint rejects duplicates.
-- name: AddTrackToPlaylist :exec
INSERT INTO playlist_tracks (playlist_id, track_id, position)
SELECT
    :playlist_id,
    :track_id,
    COALESCE(MAX(position), 0) + 1
FROM playlist_tracks
WHERE playlist_id = :playlist_id;

-- name: RemoveTrackFromPlaylist :execrows
DELETE FROM playlist_tracks
WHERE playlist_id = :playlist_id AND track_id = :track_id;

-- name: GetPlaylistTracks :many
SELECT
    sqlc.embed(tracks),
    playlist_tracks.position
FROM playlist_tracks
INNER JOIN tracks ON playlist_tracks.track_id = tracks.id
WHERE playlist_tracks.playlist_id = :playlist_id
ORDER BY playlist_tracks.position;
