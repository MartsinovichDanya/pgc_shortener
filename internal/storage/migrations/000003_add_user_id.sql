-- +goose Up
ALTER TABLE service_data.urls ADD COLUMN IF NOT EXISTS user_id VARCHAR(36) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE service_data.urls DROP COLUMN IF EXISTS user_id;
