-- Ndovu — control plane: RBAC granular por app (Fase 3 sprint E.4).
-- Semântica:
--   * super-admin (users.is_super = true): vê tudo, ignora permissions.
--   * admin de company (users.role = 'admin', is_super = false): vê todos os
--     apps da própria company automaticamente. Não precisa de permission.
--   * viewer (users.role = 'viewer'): vê SOMENTE os apps com linha nessa
--     tabela. Sem linha = sem acesso.
--
-- A checagem "user e app pertencem à mesma company" acontece na aplicação
-- (não no DB) para permitir mensagens de erro mais claras.

CREATE TABLE IF NOT EXISTS user_app_permissions (
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    app_id      uuid NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    role        text NOT NULL DEFAULT 'viewer'
                CHECK (role IN ('viewer', 'editor')),
    granted_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    granted_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, app_id)
);

CREATE INDEX IF NOT EXISTS idx_uap_user ON user_app_permissions (user_id);
CREATE INDEX IF NOT EXISTS idx_uap_app  ON user_app_permissions (app_id);
