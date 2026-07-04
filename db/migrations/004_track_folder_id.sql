-- +goose Up
-- +goose StatementBegin
ALTER TABLE tracks ADD COLUMN folder_id TEXT 
REFERENCES folders(id) ON DELETE SET NULL ON UPDATE CASCADE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE tracks DROP COLUMN folder_id;
-- +goose StatementEnd
