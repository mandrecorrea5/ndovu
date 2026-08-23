-- Ndovu — control plane: super admin (Fase 3 sprint E.3, multi-tenancy).
-- Role admin de uma company enxerga só a própria company. Super-admin
-- enxerga tudo (mantém compat: o admin bootstrap vira super por default).

ALTER TABLE users ADD COLUMN IF NOT EXISTS is_super boolean NOT NULL DEFAULT false;

-- O admin criado pelo EnsureBootstrapAdmin (empresa "Padrão") vira super.
-- Novos admins criados via UI ficam com is_super = false por default.
UPDATE users SET is_super = true
WHERE id IN (
    SELECT u.id FROM users u
    JOIN companies c ON c.id = u.company_id
    WHERE c.name = 'Padrão' AND u.role = 'admin'
);
