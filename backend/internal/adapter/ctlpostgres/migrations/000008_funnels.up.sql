-- Ndovu — control plane: funis de conversão (Sprint C.2).
-- Um funil é uma sequência ordenada de "steps" — cada step é um matcher
-- sobre trace_events (tipo + name/screen/http_url/feature). A execução do
-- funil (contagem por step) é feita on-demand no ClickHouse via windowFunnel.

CREATE TABLE IF NOT EXISTS funnels (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app            text        NOT NULL,
    name           text        NOT NULL,
    window_seconds integer     NOT NULL DEFAULT 1800,  -- 30min por default
    steps          jsonb       NOT NULL,               -- array de {name, match:{...}}
    created_by     uuid REFERENCES users (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (app, name)
);

CREATE INDEX IF NOT EXISTS idx_funnels_app ON funnels (app);
