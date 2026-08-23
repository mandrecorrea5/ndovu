-- Ndovu — deteccao de anomalia com baseline movel sazonal (Fase 4 sprint I).
-- Cada regra compara a janela atual com a mesma hora+weekday nas ultimas N
-- semanas. Sensitivity = quantos desvios-padrao alem da media disparam.

CREATE TABLE IF NOT EXISTS anomaly_rules (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name            text        NOT NULL,
    app             text        NOT NULL DEFAULT '', -- '' = todos
    metric          text        NOT NULL             -- 'error_count' | 'event_count'
                    CHECK (metric IN ('error_count', 'event_count', 'error_rate')),
    window_minutes  integer     NOT NULL DEFAULT 15
                    CHECK (window_minutes BETWEEN 1 AND 240),
    baseline_weeks  integer     NOT NULL DEFAULT 4
                    CHECK (baseline_weeks BETWEEN 1 AND 12),
    sensitivity     double precision NOT NULL DEFAULT 3.0
                    CHECK (sensitivity >= 1.0),
    direction       text        NOT NULL DEFAULT 'above'
                    CHECK (direction IN ('above', 'below', 'both')),
    silence_seconds integer     NOT NULL DEFAULT 1800,
    channel         text        NOT NULL CHECK (channel IN ('slack', 'webhook')),
    target_url      text        NOT NULL,
    active          boolean     NOT NULL DEFAULT true,
    created_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Historico de deteccoes: cada linha e um disparo. Usado para dedup
-- (silence window) e para a UI de "quando o Ndovu me alertou".
CREATE TABLE IF NOT EXISTS anomaly_detections (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id         uuid NOT NULL REFERENCES anomaly_rules (id) ON DELETE CASCADE,
    detected_at     timestamptz NOT NULL DEFAULT now(),
    current_value   double precision NOT NULL,
    baseline_avg    double precision NOT NULL,
    baseline_stddev double precision NOT NULL,
    z_score         double precision NOT NULL,
    direction       text        NOT NULL,
    notify_ok       boolean     NOT NULL DEFAULT false,
    notify_detail   text        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_anomaly_detections_rule
    ON anomaly_detections (rule_id, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_anomaly_detections_recent
    ON anomaly_detections (detected_at DESC);
