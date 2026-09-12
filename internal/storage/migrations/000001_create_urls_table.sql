-- +goose Up
CREATE SCHEMA IF NOT EXISTS service_data;
CREATE TABLE IF NOT EXISTS service_data.urls (
    uuid UUID PRIMARY KEY,
    short_url TEXT NOT NULL UNIQUE,
    original_url TEXT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS service_data.urls;
DROP SCHEMA IF EXISTS service_data;
