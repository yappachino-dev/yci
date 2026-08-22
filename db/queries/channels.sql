-- name: CreateChannel :exec
INSERT INTO channels (artist_id, channel_name)
VALUES (:artist_id, :channel_name);

-- name: GetChannels :many
SELECT
    id,
    artist_id,
    channel_name
FROM channels;

-- name: GetArtistIDByChannel :one
SELECT artist_id
FROM channels
WHERE channel_name = :channel_name;

-- name: DeleteChannel :execrows
DELETE FROM channels
WHERE artist_id = :artist_id AND channel_name = :channel_name;
