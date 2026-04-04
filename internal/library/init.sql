CREATE TABLE IF NOT EXISTS folders (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    parent_id    TEXT,
    FOREIGN KEY (parent_id) REFERENCES folders(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS artists (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    folder_id    TEXT,
    FOREIGN KEY (folder_id) REFERENCES folders(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS channels (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    artist_id    TEXT NOT NULL,
    channel_name TEXT,
    FOREIGN KEY (artist_id) references artists(id) ON DELETE CASCADE,
    UNIQUE(artist_id, channel_name)
);
