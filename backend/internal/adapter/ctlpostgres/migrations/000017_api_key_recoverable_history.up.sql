-- Guarda uma cópia cifrada da chave para consulta administrativa futura.
-- O hash continua sendo usado exclusivamente para autenticar ingestão.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS key_ciphertext text NOT NULL DEFAULT '';

-- A partir desta migração, cada app tem no máximo uma chave ativa.
WITH ranked AS (
    SELECT id, row_number() OVER (PARTITION BY app_id ORDER BY created_at DESC, id DESC) AS position
    FROM api_keys
    WHERE app_id IS NOT NULL AND active
)
UPDATE api_keys k
SET active = false, revoked_at = COALESCE(k.revoked_at, now())
FROM ranked r
WHERE k.id = r.id AND r.position > 1;

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_one_active_per_app
    ON api_keys (app_id)
    WHERE active AND app_id IS NOT NULL;
