-- Folder membership for a track is its relative_path: the folder tree *is*
-- the directory tree, so a separate folder_id on tracks restated a fact the
-- path already carried. It was written at import and maintained by the
-- foreign key's ON UPDATE CASCADE, but no query ever read it and no command
-- ever displayed it, so it invited `WHERE folder_id = ?` — which returns
-- nothing — while adding a second answer to "which folder is this track in".
--
-- Direct assignment at import is unaffected: the chosen folder still decides
-- the destination path, and ByFolder still resolves a folder to its path and
-- matches the prefix. Dropping the column also keeps that path working for
-- artist-less tracks, which `delete artist` produces, because neither step
-- consults artist_id.
--
-- +goose Up
-- +goose StatementBegin
ALTER TABLE tracks DROP COLUMN folder_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE tracks ADD COLUMN folder_id TEXT REFERENCES folders(id) ON DELETE SET NULL ON UPDATE CASCADE;
-- +goose StatementEnd
