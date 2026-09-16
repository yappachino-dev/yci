-- +goose Up
-- +goose StatementBegin
CREATE TABLE fs_journal (
    id INTEGER PRIMARY KEY,
    op TEXT NOT NULL CHECK (op IN ('move_file', 'move_dir', 'delete_file')),
    src TEXT NOT NULL,
    dst TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'done', 'failed')) DEFAULT 'pending',
    error TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_fs_journal_status ON fs_journal (status);
-- +goose StatementEnd

-- +goose Down
DROP TABLE fs_journal;
