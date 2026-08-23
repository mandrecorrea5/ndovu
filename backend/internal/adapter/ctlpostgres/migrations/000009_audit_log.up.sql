-- Ndovu — control plane: audit log de ações admin (Fase 3 sprint E.1).
-- Todo mutation admin (criar/editar/excluir/revogar) grava uma linha aqui.
-- resource_type + resource_id ficam livres (string) — a UI resolve o nome.
-- details é JSON com "before"/"after" quando aplicável.

CREATE TABLE IF NOT EXISTS audit_log (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id  uuid REFERENCES users (id) ON DELETE SET NULL,
    actor_email    text        NOT NULL DEFAULT '',
    action         text        NOT NULL,
    resource_type  text        NOT NULL DEFAULT '',
    resource_id    text        NOT NULL DEFAULT '',
    details        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    ip             text        NOT NULL DEFAULT '',
    user_agent     text        NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_actor   ON audit_log (actor_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_action  ON audit_log (action, created_at DESC);
