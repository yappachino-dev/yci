-- name: CreateChannel :exec
-- channel_id may be null for a link made by hand, before yt-dlp has been
-- asked. Such a row resolves by name only until a download fills it in.
INSERT INTO channels (artist_id, channel_name, channel_id)
VALUES (:artist_id, :channel_name, :channel_id);

-- name: GetChannels :many
SELECT
    id,
    artist_id,
    channel_name,
    channel_id
FROM channels
ORDER BY artist_id, channel_name;

-- name: GetArtistIDByChannel :many
-- Every match, not just the first. The name is not unique across artists,
-- because the uniqueness constraint is on the pair, so returning one row
-- would pick an artist arbitrarily and call it an answer. The caller decides
-- what an ambiguous name means.
SELECT artist_id
FROM channels
WHERE channel_name = :channel_name
ORDER BY artist_id;

-- name: GetArtistIDByChannelID :one
-- The authoritative lookup. A YouTube channel ID does not change when the
-- channel is renamed.
SELECT artist_id
FROM channels
WHERE channel_id = :channel_id;

-- name: GetChannelNameByChannelID :one
-- Read before writing, so a rename updates the stored label instead of
-- inserting a second link for the same channel.
SELECT channel_name
FROM channels
WHERE channel_id = :channel_id;

-- name: SetChannelNameByID :execrows
-- Refreshes the display name after a rename. Scoped to the artist so it
-- cannot move a channel between artists by accident.
UPDATE channels
SET channel_name = :channel_name
WHERE channel_id = :channel_id AND artist_id = :artist_id;

-- name: SetChannelID :execrows
-- Attaches the ID to a link that was created by name only.
UPDATE channels
SET channel_id = :channel_id
WHERE artist_id = :artist_id AND channel_name = :channel_name
  AND channel_id IS NULL;

-- name: DeleteChannel :execrows
DELETE FROM channels
WHERE artist_id = :artist_id AND channel_name = :channel_name;

-- name: SetVideoChannel :exec
-- Recorded at download time, where yt-dlp has just reported the channel the
-- video actually belongs to. A retry overwrites rather than failing, because
-- a video cannot belong to two channels and the later answer is the fresher.
INSERT INTO video_channels (video_id, channel_id)
VALUES (:video_id, :channel_id)
ON CONFLICT (video_id) DO UPDATE SET channel_id = excluded.channel_id;

-- name: GetVideoChannel :one
-- The import-time lookup, keyed by the staged file's name because that is the
-- video ID. Attribution then does not depend on the artist tag.
SELECT channel_id
FROM video_channels
WHERE video_id = :video_id;

-- name: DeleteVideoChannel :exec
-- Once the track exists, the mapping has done its job.
DELETE FROM video_channels
WHERE video_id = :video_id;

-- name: ListVideoChannels :many
-- Backs the backfill report: downloads with no channel recorded.
SELECT
    video_id,
    channel_id
FROM video_channels
ORDER BY video_id;