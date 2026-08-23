-- Fase 5: adiciona role 'editor' entre viewer e admin.
-- editor pode: triagem de issues, criar/editar/deletar funnels, saved views
-- compartilhadas. Não acessa o bloco de admin.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
  CHECK (role IN ('admin', 'editor', 'viewer'));
