-- name: CreatePlaylist :one
INSERT INTO playlists (name)
VALUES (:name)
RETURNING id, name, seed_track_id, fragment_tokens;

-- The recipe is written immediately after creation, in the same
-- transaction as the seeded membership, so a playlist is never left
-- holding membership it cannot explain.
-- name: SetPlaylistRecipe :exec
UPDATE playlists
SET seed_track_id = :seed_track_id, fragment_tokens = :fragment_tokens
WHERE id = :id;

-- name: GetPlaylists :many
SELECT
    id,
    name,
    seed_track_id,
    fragment_tokens
FROM playlists
ORDER BY name;

-- name: GetPlaylistByID :one
SELECT
    id,
    name,
    seed_track_id,
    fragment_tokens
FROM playlists
WHERE id = :id;

-- name: GetPlaylistByName :one
SELECT
    id,
    name,
    seed_track_id,
    fragment_tokens
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
