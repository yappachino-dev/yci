-- +goose Up
-- +goose StatementBegin
-- A seed recipe can exclude individual tracks from the derived set.
--
-- Token matching is lexical, so it selects on spelling rather than on work
-- identity: a performer family also picks up collabs, self-covers, and
-- titles that merely share a word. Deselecting those in the wizard is only a
-- real fix if the decision is recorded here — otherwise the next
-- `playlist reseed` re-derives from the tokens alone and puts them straight
-- back.
--
-- Stored as a JSON array of track IDs for the same reason the tokens are:
-- a value can never be corrupted by a separator appearing inside it.
ALTER TABLE playlists ADD COLUMN fragment_excludes TEXT NOT NULL DEFAULT '[]';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE playlists DROP COLUMN fragment_excludes;
-- +goose StatementEnd
