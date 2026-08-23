-- Ndovu — control plane (Postgres): configuração da própria ferramenta.
-- Traces NÃO vivem aqui (ficam no ClickHouse) — aqui é o que é transacional
-- e mutável: usuários do backoffice e chaves de API dos apps emissores.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text        NOT NULL UNIQUE,
    name          text        NOT NULL,
    password_hash text        NOT NULL,
    role          text        NOT NULL CHECK (role IN ('admin', 'viewer')),
    active        boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS api_keys (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app        text        NOT NULL,
    label      text        NOT NULL DEFAULT '',
    -- nunca guardamos a chave em claro: só o SHA-256 e um prefixo para exibição
    key_hash   text        NOT NULL UNIQUE,
    key_prefix text        NOT NULL,
    active     boolean     NOT NULL DEFAULT true,
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);

CREATE INDEX IF NOT EXISTS idx_api_keys_app ON api_keys (app);
