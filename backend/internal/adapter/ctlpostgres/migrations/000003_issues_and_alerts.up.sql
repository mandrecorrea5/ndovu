-- Ndovu — control plane: triagem de issues e regras de alerta.
-- Issues são estado mutável (status/assignee); a contagem/first_seen/last_seen
-- vem do ClickHouse. Alertas são regras + histórico de disparos.

CREATE TABLE IF NOT EXISTS issue_states (
    fingerprint text        PRIMARY KEY,
    app         text        NOT NULL,
    status      text        NOT NULL DEFAULT 'open'
                CHECK (status IN ('open', 'resolved', 'ignored')),
    assignee    text        NOT NULL DEFAULT '',
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_issue_states_app ON issue_states (app);
CREATE INDEX IF NOT EXISTS idx_issue_states_status ON issue_states (status);

CREATE TABLE IF NOT EXISTS alert_rules (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text        NOT NULL,
    app            text        NOT NULL DEFAULT '',
    error_code     text        NOT NULL DEFAULT '',
    threshold      integer     NOT NULL CHECK (threshold > 0),
    window_seconds integer     NOT NULL CHECK (window_seconds > 0),
    channel        text        NOT NULL CHECK (channel IN ('slack', 'webhook')),
    target_url     text        NOT NULL,
    silence_seconds integer    NOT NULL DEFAULT 900,
    active         boolean     NOT NULL DEFAULT true,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS alert_deliveries (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id      uuid REFERENCES alert_rules (id) ON DELETE CASCADE,
    delivered_at timestamptz NOT NULL DEFAULT now(),
    count_seen   integer     NOT NULL,
    ok           boolean     NOT NULL,
    detail       text        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_alert_deliveries_rule ON alert_deliveries (rule_id, delivered_at DESC);
