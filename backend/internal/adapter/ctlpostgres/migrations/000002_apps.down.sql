-- Rollback da migração 000002 (apps).
ALTER TABLE api_keys DROP COLUMN IF EXISTS app_id;
DROP TABLE IF EXISTS apps;
