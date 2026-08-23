-- Ndovu — user feedback widget (Fase 4 sprint J).
-- Reclamacoes/sugestoes que o usuario final envia via widget no proprio app.
-- Ficam vinculadas a session_id (obrigatorio) e event_id (opcional — o
-- ultimo evento em memoria quando o widget foi aberto). O drill do dashboard
-- usa isso pra pular direto pro trace/snapshot correspondente.

CREATE TABLE IF NOT EXISTS user_feedbacks (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app          text        NOT NULL,
    session_id   text        NOT NULL,
    event_id     text        NOT NULL DEFAULT '',    -- vazio = feedback fora de erro
    user_id      text        NOT NULL DEFAULT '',
    type         text        NOT NULL DEFAULT 'bug'
                 CHECK (type IN ('bug', 'suggestion', 'praise', 'other')),
    message      text        NOT NULL,
    email        text        NOT NULL DEFAULT '',
    url          text        NOT NULL DEFAULT '',
    viewport_w   integer     NOT NULL DEFAULT 0,
    viewport_h   integer     NOT NULL DEFAULT 0,
    status       text        NOT NULL DEFAULT 'new'
                 CHECK (status IN ('new', 'triaging', 'resolved', 'dismissed')),
    resolved_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    resolved_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_feedbacks_app_status
    ON user_feedbacks (app, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_feedbacks_session
    ON user_feedbacks (session_id);
