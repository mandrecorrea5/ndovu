-- Ndovu — control plane: triagem colaborativa de issues (Fase 2 sprint B).
-- 1) Comentários no issue (author, body, created_at).
-- 2) Novo status 'investigating' (entre 'open' e 'resolved').
-- 3) assignee_user_id: FK para users (mantém o campo texto "assignee" legado
--    por enquanto — o UI usa o FK; migração futura pode dropar o texto).

ALTER TABLE issue_states DROP CONSTRAINT IF EXISTS issue_states_status_check;
ALTER TABLE issue_states ADD CONSTRAINT issue_states_status_check
    CHECK (status IN ('open', 'investigating', 'resolved', 'ignored'));

ALTER TABLE issue_states ADD COLUMN IF NOT EXISTS assignee_user_id uuid REFERENCES users (id);
CREATE INDEX IF NOT EXISTS idx_issue_states_assignee ON issue_states (assignee_user_id);

CREATE TABLE IF NOT EXISTS issue_comments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    fingerprint text        NOT NULL,
    author_id   uuid REFERENCES users (id),
    body        text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_issue_comments_fp
    ON issue_comments (fingerprint, created_at DESC);
