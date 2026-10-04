-- +goose Up
-- +goose StatementBegin

-- A watched YouTube playlist: a remote list whose new videos are pulled into
-- the download queue on demand.
--
-- Deliberately not a playlists row. A user playlist is local membership,
-- which is authoritative and only ever changes by hand or through an explicit
-- reseed. This is a remote source that feeds the queue. Merging the two would
-- turn every poll into the automatic membership change the design forbids.
--
-- channels holds the user's channel selection as a JSON array of
-- {id, name} objects. Both are kept because they answer different questions:
-- the UC... ID is what a selection is matched against, and the name is what
-- the user reads. Matching on the ID is what makes a selection survive the
-- performer renaming their channel, which yt-dlp reports per entry as both
-- channel and channel_id. A listing that omits channel_id (a channel's own
-- uploads list) stores an empty id and matches on name instead.
CREATE TABLE playlist_sources (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    url        TEXT NOT NULL UNIQUE,
    channels   TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

-- Append-only record of track lifecycle events.
--
-- Only the delete decision is consumed today. The queue dedupes against
-- tracks.source_id, so a video that is still in the library is skipped; but a
-- deleted row is gone, and a watched playlist that still offers that video
-- would download it again. This is what makes a deletion a decision rather
-- than an accident.
--
-- Append-only, so a delete is a new entry and never an update: the history of
-- what was removed is itself the record. source_id is indexed because it is
-- the lookup key. It is nullable because a hand-staged file has no YouTube
-- video behind it, and such a track can never be matched against a source.
CREATE TABLE track_journal (
    id         INTEGER PRIMARY KEY,
    track_id   TEXT NOT NULL,
    source_id  TEXT,
    op         TEXT NOT NULL CHECK (op IN ('imported', 'deleted')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_track_journal_source ON track_journal (source_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_track_journal_source;
DROP TABLE track_journal;
DROP TABLE playlist_sources;
-- +goose StatementEnd