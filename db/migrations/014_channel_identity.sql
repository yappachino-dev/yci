-- +goose Up
-- +goose StatementBegin
-- Key channel links by YouTube's channel ID instead of its display name.
--
-- A channel_name is what the user typed when linking, and it is also what
-- yt-dlp writes into the artist tag. Both drift: the user can retype it, and
-- the performer can rename the channel on YouTube at any time. When they
-- disagree, import stops resolving the artist and prompts per file.
--
-- The UC... ID is immutable, so it is the identity; the name becomes a label
-- that gets refreshed when it drifts.
ALTER TABLE channels ADD COLUMN channel_id TEXT;

-- One YouTube channel maps to exactly one artist. The existing UNIQUE is on
-- (artist_id, channel_name), which permits the same channel to be linked to
-- two artists under two spellings — the rename failure in a second form.
-- Partial, because rows linked before this migration have no ID yet and NULL
-- would collide otherwise.
CREATE UNIQUE INDEX idx_channels_channel_id ON channels(channel_id)
WHERE channel_id IS NOT NULL;

-- Which channel a downloaded video came from.
--
-- The artist tag in a staged file carries the channel's *name*, which is the
-- thing that changes. This table is written at download time, where yt-dlp
-- has just reported the authoritative ID, and read at import time, so
-- attribution no longer depends on a string the performer can edit.
--
-- Keyed by video_id because that is the staged file's name, so import already
-- holds the join key. Rows are removed once the track is imported; a row that
-- survives an abandoned download is harmless and self-cleaning on re-download.
CREATE TABLE video_channels (
    video_id   TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- channel_id is dropped rather than nulled. A NULL would mean "linked but
-- unidentified", which is the state this migration exists to end: import would
-- fall back to matching on name and the rename problem would return wearing a
-- new column name.
DROP TABLE video_channels;
DROP INDEX idx_channels_channel_id;
ALTER TABLE channels DROP COLUMN channel_id;
-- +goose StatementEnd