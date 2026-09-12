-- +goose Up
CREATE UNIQUE INDEX IF NOT EXISTS idx_original_url ON service_data.urls (original_url);

-- +goose Down
DROP INDEX IF EXISTS idx_original_url;
