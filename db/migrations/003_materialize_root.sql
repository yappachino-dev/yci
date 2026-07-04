-- +goose Up
-- +goose StatementBegin
INSERT OR IGNORE INTO folders (id, display_name, parent_id)
VALUES ('root', 'Root', NULL);

UPDATE folders
SET parent_id = 'root'
WHERE parent_id IS NULL AND id != 'root';

UPDATE artists
SET folder_id = 'root'
WHERE folder_id IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE artists
SET folder_id = NULL
WHERE folder_id = 'root';

UPDATE folders
SET parent_id = NULL
WHERE parent_id = 'root';

DELETE FROM folders
WHERE id = 'root';
-- +goose StatementEnd
