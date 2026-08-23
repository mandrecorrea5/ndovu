-- Ndovu — sampling adaptativo server-side (Fase 3 sprint G).
-- Regras opcionais por (app, event_type) descartam uma fracao dos eventos
-- antes de gravar no ClickHouse. Erros sempre passam quando keep_errors=true
-- (default) — a ideia e amortizar volume de page_view/action sem perder
-- sinais de falha.
--
-- Precedencia (mais especifica ganha): (app, type) > (app, '') > ('', type) > ('', '').
-- Sem regra que case → sample_rate implicito = 1.0 (mantem tudo).

CREATE TABLE IF NOT EXISTS sampling_rules (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app          text        NOT NULL DEFAULT '', -- '' = todas
    event_type   text        NOT NULL DEFAULT '', -- '' = todos os tipos
    sample_rate  double precision NOT NULL DEFAULT 1.0
                 CHECK (sample_rate >= 0 AND sample_rate <= 1),
    keep_errors  boolean     NOT NULL DEFAULT true,
    active       boolean     NOT NULL DEFAULT true,
    note         text        NOT NULL DEFAULT '',
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    -- Um par (app, event_type) tem uma regra ativa unica. Se o admin
    -- editar, faz UPDATE — nao acumula duplicatas.
    UNIQUE (app, event_type)
);

CREATE INDEX IF NOT EXISTS idx_sampling_active ON sampling_rules (active) WHERE active;
