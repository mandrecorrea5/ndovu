-- Ndovu — control plane: saved views (Sprint C.1).
-- View é um preset de filtros nomeado por usuário. Escopo: private (só o dono
-- vê) ou shared (todos os usuários do backoffice veem). view_type identifica
-- em qual tela ela se aplica (traces, issues, sessions, ...).

CREATE TABLE IF NOT EXISTS saved_views (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    view_type     text        NOT NULL,
    name          text        NOT NULL,
    filters       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    is_shared     boolean     NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    -- Duas views com o mesmo nome no mesmo tipo só se pertencerem a donos
    -- diferentes — um usuário não repete nome dentro de um tipo.
    UNIQUE (owner_user_id, view_type, name)
);

CREATE INDEX IF NOT EXISTS idx_saved_views_owner
    ON saved_views (owner_user_id, view_type);
CREATE INDEX IF NOT EXISTS idx_saved_views_shared
    ON saved_views (view_type) WHERE is_shared;
