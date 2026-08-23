-- Ndovu — control plane: source maps por (app, release, filename).
-- Guardamos o conteúdo do .map como texto (JSON com base64 embutido) —
-- source maps em produção raramente passam de alguns MB.

CREATE TABLE IF NOT EXISTS source_maps (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app        text        NOT NULL,
    release    text        NOT NULL,
    filename   text        NOT NULL,
    content    text        NOT NULL,
    size_bytes integer     NOT NULL DEFAULT 0,
    uploaded_by uuid REFERENCES users (id),
    uploaded_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (app, release, filename)
);

CREATE INDEX IF NOT EXISTS idx_source_maps_app_release
    ON source_maps (app, release);
