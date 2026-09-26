-- +goose Up
-- +goose StatementBegin
-- Provenance: the title as it came from the file's tags, before any parsing.
-- NULL means the row predates title parsing and has never been re-read.
ALTER TABLE tracks ADD COLUMN raw_title TEXT;

-- Parsed hints from the raw title. These are observations, not authority:
-- artist_id remains the curated link the user confirmed at import.
ALTER TABLE tracks ADD COLUMN performer TEXT NOT NULL DEFAULT '';
ALTER TABLE tracks ADD COLUMN version TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE tracks DROP COLUMN version;
ALTER TABLE tracks DROP COLUMN performer;
ALTER TABLE tracks DROP COLUMN raw_title;
-- +goose StatementEnd
