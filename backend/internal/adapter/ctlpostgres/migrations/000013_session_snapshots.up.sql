-- Ndovu — session replay MVP (Fase 3 sprint H).
-- Snapshots do DOM capturados pelo SDK no momento de um error(). Metadata
-- fica no Postgres; o payload (HTML gzip'ado) vai pro MinIO/S3.
--
-- Um evento tem no maximo 1 snapshot (unique event_id). SDK dedup no envio,
-- backend dedup por seguranca via INSERT ... ON CONFLICT DO NOTHING.

CREATE TABLE IF NOT EXISTS session_snapshots (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id    uuid NOT NULL UNIQUE,
    session_id  text NOT NULL,
    app         text NOT NULL,
    -- caminho do objeto no bucket S3 (ex.: 2026/08/23/uuid.html.gz)
    object_key  text NOT NULL,
    size_bytes  integer NOT NULL DEFAULT 0,
    viewport_w  integer NOT NULL DEFAULT 0,
    viewport_h  integer NOT NULL DEFAULT 0,
    url         text    NOT NULL DEFAULT '',
    taken_at    timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_snapshots_session ON session_snapshots (session_id, taken_at DESC);
CREATE INDEX IF NOT EXISTS idx_snapshots_app     ON session_snapshots (app, taken_at DESC);
