-- +goose Up
-- +goose StatementBegin
-- Fragment matching is a token set, and how the tokens combine is a user
-- choice: AND (every token present) or ANY (at least one). AND stays the
-- default, because it is what the derivation proposes and what keeps
-- unrelated titles out; ANY is a deliberate widening.
--
-- Stored alongside the tokens rather than encoded inside them, so a
-- playlist written before this column existed keeps AND semantics without
-- rewriting its recipe.
ALTER TABLE playlists ADD COLUMN fragment_mode TEXT NOT NULL DEFAULT 'all';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE playlists DROP COLUMN fragment_mode;
-- +goose StatementEnd
