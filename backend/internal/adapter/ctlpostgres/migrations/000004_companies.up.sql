-- Ndovu — control plane: empresas (multi-tenant leve).
-- Todo usuário passa a estar atrelado a uma empresa (NOT NULL); apps também
-- podem estar (FK opcional). Preservamos o campo texto legado `apps.company`
-- durante a transição — o novo cadastro usa a FK.

CREATE TABLE IF NOT EXISTS companies (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL UNIQUE,
    document   text        NOT NULL DEFAULT '',
    active     boolean     NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Empresa "Padrão" recebe todos os registros pré-existentes; assim conseguimos
-- setar users.company_id NOT NULL sem perder o admin do bootstrap.
INSERT INTO companies (name)
VALUES ('Padrão')
ON CONFLICT (name) DO NOTHING;

-- Backfill de apps.company (texto livre) → companies.name.
INSERT INTO companies (name)
SELECT DISTINCT company FROM apps
WHERE company <> ''
  AND NOT EXISTS (SELECT 1 FROM companies c WHERE c.name = apps.company);

-- Users: adiciona FK, backfill para "Padrão", trava NOT NULL.
ALTER TABLE users ADD COLUMN IF NOT EXISTS company_id uuid REFERENCES companies (id);
UPDATE users
SET company_id = (SELECT id FROM companies WHERE name = 'Padrão')
WHERE company_id IS NULL;
ALTER TABLE users ALTER COLUMN company_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_company ON users (company_id);

-- Apps: FK opcional para não quebrar apps já cadastrados sem empresa.
ALTER TABLE apps ADD COLUMN IF NOT EXISTS company_id uuid REFERENCES companies (id);
UPDATE apps a
SET company_id = c.id
FROM companies c
WHERE a.company_id IS NULL AND a.company <> '' AND c.name = a.company;
CREATE INDEX IF NOT EXISTS idx_apps_company ON apps (company_id);
