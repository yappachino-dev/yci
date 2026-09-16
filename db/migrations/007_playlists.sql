-- +goose Up
CREATE TABLE playlists (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE COLLATE nocase
);

-- Positions are append-only and monotonic per playlist: removals leave
-- gaps, which ordering tolerates. Rewriting positions is a reorder
-- operation's job, not a removal's.
CREATE TABLE playlist_tracks (
    playlist_id INTEGER NOT NULL REFERENCES playlists (id) ON DELETE CASCADE,
    track_id TEXT NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    PRIMARY KEY (playlist_id, position),
    UNIQUE (playlist_id, track_id)
);

-- +goose Down
-- +goose StatementBegin
DROP TABLE playlist_tracks;
DROP TABLE playlists;
-- +goose StatementEnd
