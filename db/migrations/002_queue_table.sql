-- +goose Up
-- +goose StatementBegin
CREATE TABLE download_queue (
    id INTEGER PRIMARY KEY,
    source_id TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (
        status IN ('pending', 'processing', 'failed')
    ) DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    error_msg TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_download_queue_status ON download_queue (status);
-- +goose StatementEnd

-- +goose Down
DROP TABLE download_queue;
