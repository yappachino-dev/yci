CREATE TABLE IF NOT EXISTS folders (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    parent_id    TEXT,
    FOREIGN KEY (parent_id) REFERENCES folders(id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS artists (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    folder_id    TEXT,
    FOREIGN KEY (folder_id) REFERENCES folders(id) ON DELETE SET NULL ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS channels (
    id           INTEGER PRIMARY KEY,
    artist_id    TEXT NOT NULL,
    channel_name TEXT,
    FOREIGN KEY (artist_id) REFERENCES artists(id) ON DELETE CASCADE ON UPDATE CASCADE,
    UNIQUE(artist_id, channel_name)
);

CREATE TABLE IF NOT EXISTS tracks (
    id            TEXT PRIMARY KEY,
    title         TEXT NOT NULL,
    album         TEXT,
    artist_id     TEXT,
    duration_sec  INTEGER DEFAULT 0,
    relative_path TEXT NOT NULL UNIQUE,
    source_id     TEXT,
    FOREIGN KEY (artist_id) REFERENCES artists(id) ON DELETE SET NULL ON UPDATE CASCADE
);
