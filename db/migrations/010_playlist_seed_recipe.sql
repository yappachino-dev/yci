-- +goose Up
-- +goose StatementBegin
-- A playlist can remember the recipe it was seeded from: which track was
-- the seed, and which title tokens were chosen. The recipe is stored so
-- membership can be re-derived on demand and reviewed as a diff, rather
-- than by a rule that silently rewrites membership on import.
--
-- Tokens are stored as a JSON array rather than a delimited string so a
-- token can never be corrupted by a separator appearing inside it.
ALTER TABLE playlists ADD COLUMN seed_track_id TEXT REFERENCES tracks(id) ON DELETE SET NULL;
ALTER TABLE playlists ADD COLUMN fragment_tokens TEXT NOT NULL DEFAULT '[]';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE playlists DROP COLUMN fragment_tokens;
ALTER TABLE playlists DROP COLUMN seed_track_id;
-- +goose StatementEnd
