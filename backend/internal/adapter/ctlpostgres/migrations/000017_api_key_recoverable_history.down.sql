DROP INDEX IF EXISTS idx_api_keys_one_active_per_app;
ALTER TABLE api_keys DROP COLUMN IF EXISTS key_ciphertext;
