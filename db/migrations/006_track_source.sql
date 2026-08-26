-- +goose Up
-- +goose StatementBegin
ALTER TABLE tracks ADD COLUMN source TEXT NOT NULL DEFAULT 'youtube';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE tracks DROP COLUMN source;
-- +goose StatementEnd
