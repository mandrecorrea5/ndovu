
## 2026-09-30 [git, repo-structure, gotcha, docs]
O push para origin/main pode ser rejeitado com um commit "sync" que traz um espelho do repo inteiro DENTRO das pastas já existentes (backend/docs/, backend/dev/, backend/.github/, dashboard/.gitignore, gitops/, platform/, sdk/src/). Cuidado: backend/docs/ e docs/ são duas árvores de docs diferentes convivendo no mesmo repo — verificar duplicação antes de editar ou referenciar docs em qualquer uma delas.

## 2026-09-30 [clickhouse, backup, r2, gotcha, prod]
Backup nativo do ClickHouse 24.8 para Cloudflare R2 funciona (round-trip BACKUP/RESTORE validado 2026-09-30), com 3 quirks: (1) `TO S3(url, ak, sk, 'auto')` com 4 args FALHA com Code:42 — a 4ª string não é region em 24.8, use SOMENTE 3 args (url, ak, sk); (2) use ASYNC + monitore `system.backups` (status BACKUP_CREATED/BACKUP_FAILED + coluna error); (3) o storage.xml de dev aponta para o host `minio` — sem o perfil local-minio ativo, o ClickHouse NÃO sobe (DNS fail de `minio` trava o boot da instância). R2 region retorna 'ENAM' no head-bucket — irrelevante para o backup.
