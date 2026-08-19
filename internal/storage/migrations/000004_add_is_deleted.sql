-- +goose Up
ALTER TABLE service_data.urls ADD COLUMN is_deleted BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE service_data.urls DROP COLUMN is_deleted;
