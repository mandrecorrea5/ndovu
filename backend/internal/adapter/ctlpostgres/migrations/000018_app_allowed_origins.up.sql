ALTER TABLE apps
    ADD COLUMN allowed_origins text[] NOT NULL DEFAULT '{}';
