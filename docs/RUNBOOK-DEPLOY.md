# Runbook — Deploy, update e rollback (VPS / Docker Compose)

**Escopo:** a trilha de produção atual (VPS + `docker-compose.yml` +
`docker-compose.prod.yml` + Cloudflare Tunnel + Caddy). A trilha OpenShift
(`docs/openshift/`, `gitops/`) tem procedimento próprio — não a usamos agora.

## 1. Primeiro deploy (bootstrap) — na ordem

Prerequisitos já validados localmente: `DEPLOY-VPS.md` inteiro +
`.env.production` real (todos os segredos ≥ requisito, `chmod 600`) + Tunnel
criado na Cloudflare, token configurado e duas rotas públicas apontando para
`https://caddy:443` + 3+1 buckets R2 com credenciais isoladas + identidades
age PG/CH no cofre (não na VPS).

```bash
cd /opt/ndovu
git pull

# 1. valida a composição sem subir nada (a API recusaria boot com segredo fraco)
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml config --quiet

# 2. sobe
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml up -d --build

# 3. fumaça
docker compose ... ps                       # esperado: healthy / running
curl -sSf https://NDOVU_API_DOMAIN/health   # 200
# login no domínio do app; TROQUE a senha bootstrap do admin no 1º acesso
# crie 1 chave de API por app (bootstrap "dev" está DESATIVADO em produção)
```

4. **Backup:** execute o round-trib sintético
   (`bash infra/backup/test-postgres-backup.sh`), depois instale os timers
   (`ndovu-postgres-backup.timer`) e confirme o 1º disparo no
   `journalctl -u ndovu-postgres-backup.service`.

5. **Tunnel:** confirme `docker compose logs cloudflared` sem erros de conexão
   e teste `curl -vI https://NDOVU_APP_DOMAIN` e
   `curl -sSf https://NDOVU_API_DOMAIN/health`. Os certificados públicos são
   gerenciados pela Cloudflare; o Caddy usa TLS interno na origem.

### 1.5 Acesso público pelo Tunnel

O Ndovu não publica as portas do Caddy na VPS. Acesso público requer domínio
na zona Cloudflare, Tunnel conectado e rotas do app e da API configuradas
conforme `DEPLOY-VPS.md`. Não use o IP da VPS como hostname público nem
publique `8443` como alternativa.

Se a rota não funcionar, confira primeiro o estado do Tunnel no painel e os
logs do conector. Para cada hostname, confirme `Origin Server Name` igual ao
hostname e **No TLS Verify** ativado: o Caddy apresenta certificado emitido
pela CA interna.

## 2. Update de versão (rotina)

```bash
cd /opt/ndovu
git fetch && git log --oneline HEAD..origin/main   # revisa o que entra
git pull
# migrations? ver §3 abaixo
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml up -d --build
docker compose ... ps                              # healthy dos 4 serviços
# smoke: /health 200 + login + 1 evento 202 + visível no dashboard
```

## 3. Migrations (quando um release as contém)

As migrations vivem no backend (`internal/adapter/clickhouse/schema.sql` +
`ctlpostgres`). A aplicação aplica idempotente no boot
(`CREATE TABLE IF NOT EXISTS` / `ALTER ... IF`). **Ainda assim, antes de
qualquer release com migration: backup dos 2 bancos confirmado.**

## 4. Rollback

- **Código, sem migration:** `git checkout <tag anterior> &&
  up -d --build`.
- **Com migration aplicada:** NUNCA reverter código para antes da migration
  sem antes restaurar o banco correspondente —
  `platform/runbooks/postgres-restore.md` / backup do ClickHouse. Migration
  é via-forward: o caminho é corrigir para frente ou restaurar dados.
- **Parelha de reversão:** código antigo + schema novo pode falhar de forma
  silenciosa (coluna esperada). Após qualquer rollback com migration no
  meio, rode a smoke inteira, não só `/health`.

## 5. Reinciar um serviço específico

```bash
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml restart api
# writer: reiniciar é seguro — consumer durável, sem perda
# caddy/postgres/clickhouse/nats: SEMPRE com os runbooks de dados por perto
```

## 6. Onde está a saúde

- API: `GET /health` (valida Postgres, ClickHouse e stream) — a API
  distroless não tem healthcheck interno possível; liveness vem do
  `restart: unless-stopped` + monitor externo em `/health`.
- NATS: `http://nats:8222/healthz` (rede interna do compose).
- Writer: sem rota de saúde — sinais são `docker logs` +
  `NumPending` (runbook `jetstream-backlog.md`).
- **`/metrics` exige Basic Auth no Caddy** (`NDOVU_METRICS_PASSWORD_HASH`,
  usuário `metrics`). Prometheus/scrape envia o header; um browser não
  manda auth em navegação — use o proxy do monitoramento. Se for necessário
  acessar a origem diretamente para diagnóstico, faça isso pela rede Docker;
  Caddy e cloudflared não publicam portas web no host.
