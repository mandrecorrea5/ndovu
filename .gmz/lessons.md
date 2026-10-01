
## 2026-09-30 [git, repo-structure, gotcha, docs]
O push para origin/main pode ser rejeitado com um commit "sync" que traz um espelho do repo inteiro DENTRO das pastas já existentes (backend/docs/, backend/dev/, backend/.github/, dashboard/.gitignore, gitops/, platform/, sdk/src/). Cuidado: backend/docs/ e docs/ são duas árvores de docs diferentes convivendo no mesmo repo — verificar duplicação antes de editar ou referenciar docs em qualquer uma delas.

## 2026-09-30 [clickhouse, backup, r2, gotcha, prod]
Backup nativo do ClickHouse 24.8 para Cloudflare R2 funciona (round-trip BACKUP/RESTORE validado 2026-09-30), com 3 quirks: (1) `TO S3(url, ak, sk, 'auto')` com 4 args FALHA com Code:42 — a 4ª string não é region em 24.8, use SOMENTE 3 args (url, ak, sk); (2) use ASYNC + monitore `system.backups` (status BACKUP_CREATED/BACKUP_FAILED + coluna error); (3) o storage.xml de dev aponta para o host `minio` — sem o perfil local-minio ativo, o ClickHouse NÃO sobe (DNS fail de `minio` trava o boot da instância). R2 region retorna 'ENAM' no head-bucket — irrelevante para o backup.

## 2026-09-30 [caddy, proxy, gotcha, metrics, security]
A imagem oficial caddy:2.9-alpine NÃO traz a diretiva/matcher `rate_limit` (nem http.matchers.rate_limit — a config falha no `caddy validate` e derruba o boot). A proteção de rate de ingestão vive na API Go (token bucket por X-Api-Key, NDOVU_INGEST_RATE_RPS). /metrics da API exige Basic Auth no Caddy desde 222e940 (usuário ndovu + hash bcrypt via NDOVU_METRICS_PASSWORD_HASH).

## 2026-10-01 [vps, docker, deploy, gotcha, cd, backup]
Na VPS de produção do Ndovu (srv5321122), os testes de credencial com `docker compose exec` SÓ VÁLIDOS se rodados a partir de `/opt/ndovu` com `--env-file .env.production`. Rodados de `~`/`/root`, o compose não acha o env → todas as variáveis chegam vazias → **falsos DIVERGE** (custou 4 turnos de diagnóstico). Guarda-corpo: começar com `cd /opt/ndovu || exit 1`.

Outros fatos não-óbvious da mesma máquina: a imagem da API é **distroless** (sem `wget`/`curl`/shell → smoke interno tem que ser feita a partir do container `caddy`, que tem busybox). O `/health` da API é a prova real de PG+ClickHouse+NATS (valida os três). A variável que a **API** lê é `NDOVU_CLICKHOUSE_PASSWORD`; `CLICKHOUSE_PASSWORD` serve só ao host (criação de usuário) — nomes diferentes, propósitos diferentes.
