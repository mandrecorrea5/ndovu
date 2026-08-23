-- Ndovu — control plane: apps emissores (frontends) com metadados.
-- Cada app tem uma ou mais chaves de API; o cadastro do app gera a primeira
-- chave (exibida uma única vez para enviar ao responsável da integração).

CREATE TABLE IF NOT EXISTS apps (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text        NOT NULL UNIQUE,
    technology  text        NOT NULL DEFAULT '',
    company     text        NOT NULL DEFAULT '',
    responsible text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Backfill: cria um app para cada nome de app já existente nas chaves.
INSERT INTO apps (name)
SELECT DISTINCT app FROM api_keys WHERE app IS NOT NULL AND app <> ''
ON CONFLICT (name) DO NOTHING;

-- Liga cada chave ao app dono (nullable para chaves legadas sem app cadastrado).
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS app_id uuid REFERENCES apps (id);

UPDATE api_keys k
SET app_id = a.id
FROM apps a
WHERE a.name = k.app AND k.app_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_api_keys_app_id ON api_keys (app_id);
