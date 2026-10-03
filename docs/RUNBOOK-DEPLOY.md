# Runbook — Deploy, update e rollback (VPS / Docker Compose)

**Escopo:** a trilha de produção atual (VPS + `docker-compose.yml` +
`docker-compose.prod.yml` + Cloudflare Tunnel + Caddy). A trilha OpenShift
(`docs/openshift/`, `gitops/`) tem procedimento próprio — não a usamos agora.

## 1. Primeiro deploy (bootstrap) — na ordem

Prerequisitos já validados localmente: `DEPLOY-VPS.md` inteiro +
`.env.production` real (todos os segredos ≥ requisito, `chmod 600`) + Tunnel
criado na Cloudflare, token configurado e duas rotas públicas apontando para
`http://caddy:80` + 3+1 buckets R2 com credenciais isoladas + identidades
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
   gerenciados pela Cloudflare; o tráfego da origem segue pela rede Docker
   privada até o Caddy via HTTP.

### 1.5 Acesso público pelo Tunnel

O Ndovu não publica as portas do Caddy na VPS. Acesso público requer domínio
na zona Cloudflare, Tunnel conectado e rotas do app e da API configuradas
conforme `DEPLOY-VPS.md`. Não use o IP da VPS como hostname público nem
publique `8443` como alternativa.

Se a rota não funcionar, confira primeiro o estado do Tunnel no painel e os
logs do conector. Confirme que os serviços das duas rotas são
`http://caddy:80` e que o Host HTTP está usando o padrão.

## 2. Update de versão (rotina)

Para releases **sem migration**, o timer da VPS continua buscando `origin/main`
a cada 5 minutos e faz o deploy automaticamente.

```bash
cd /opt/ndovu
git fetch origin
git log --oneline .cd/deployed-sha..origin/main   # veja o commit pendente
sudo tail -f /var/log/ndovu-cd/deploy.log         # acompanhe o timer
```

## 3. Migrations (quando um release as contém)

As migrations vivem no backend (`internal/adapter/clickhouse/schema.sql` +
`ctlpostgres`). A aplicação as aplica no boot. O timer **não** atualiza um
release com migration até que o workflow manual aprove o SHA exato de `main`.
Após a aprovação, a VPS confirma backups de PostgreSQL e ClickHouse no R2
antes de iniciar o deploy; se qualquer backup falhar, o deploy para.

Procedimento:

1. Aguarde o CI terminar com sucesso para o commit que contém a migration.
2. No GitHub, abra **Actions → Approve migration deploy → Run workflow**.
3. Informe o SHA completo (40 caracteres) do commit atual de `main`. O
   workflow recusa SHA que não seja o topo atual de `main`, SHA inválido,
   CI pendente/com falha ou uma aprovação já existente para esse SHA.
4. O workflow cria a tag anotada `deploy-approved-<SHA>` e aguarda. A VPS
   busca essa tag no próximo ciclo do timer, executa e verifica os dois
   backups, aplica o commit e, após health check, publica sucesso para esse
   SHA. O workflow termina verde apenas depois de receber esse status.
5. Acompanhe **Actions** e, na VPS, `/var/log/ndovu-cd/deploy.log`. A
   aprovação continua disponível após falhas para o timer tentar novamente.
   O workflow aguarda até 5h50; depois disso ele termina vermelho, mas o timer
   continua tentando e pode publicar sucesso quando o problema for resolvido.

Antes de aprovar, configure uma única vez `NDOVU_API_KEY_ENC_KEY` em
`/opt/ndovu/.env.production` (mínimo 32 caracteres, estável, com backup seguro)
e confirme permissões `600`. **Não troque essa chave em releases futuros**:
ela permite revelar as chaves de API armazenadas.

Crie um Fine-grained personal access token em **GitHub → Settings →
Developer settings → Personal access tokens**. Restrinja-o ao repositório
Ndovu e conceda somente **Commit statuses: read and write**. Configure-o como
`NDOVU_GITHUB_STATUS_TOKEN` em `/opt/ndovu/.env.production`; não o envie ao
Compose nem o grave no Git. O token só é usado pelo script local da VPS para
publicar `pending` e, após o health check, `success` para o SHA exato. Use
`sudoedit /opt/ndovu/.env.production` e depois `sudo chmod 600
/opt/ndovu/.env.production`.

O workflow usa o `GITHUB_TOKEN` com `contents: write` para criar a tag,
`actions: read` para confirmar o CI e `statuses: read` para aguardar a
confirmação da VPS. Se necessário, habilite a permissão de escrita de conteúdo
do `GITHUB_TOKEN` nas configurações de Actions do repositório. Mantenha o
acesso ao workflow restrito a pessoas autorizadas a aprovar deploys de
produção. A VPS mantém o modelo pull-only e não precisa expor SSH para o
GitHub Actions.

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
