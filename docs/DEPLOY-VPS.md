# Deploy na VPS — configuração e credenciais

Este guia define o perfil de configuração de produção do Ndovu no Docker
Compose (segredos, rede/TLS, backup e validação). **O passo a passo de
deploy, update e rollback está em
[`RUNBOOK-DEPLOY.md`](./RUNBOOK-DEPLOY.md)**; a operação de dados (disco,
backlog, restore, migração de cold, expansão) nos
[`platform/runbooks/`](../platform/runbooks/).

## Variáveis obrigatórias

Copie `.env.production.example` para `.env.production` na VPS e preencha os
valores, com permissões restritas (`chmod 600 .env.production`). Esse arquivo é
ignorado pelo Git. Gere valores novos para cada ambiente; não copie defaults
de desenvolvimento.

| Variável | Requisito |
|---|---|
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | Credenciais próprias; a senha não pode ser `ndovu`. Prefira caracteres hexadecimais para que a URL de conexão seja formada sem problemas de escaping. |
| `CLICKHOUSE_DB`, `CLICKHOUSE_USER`, `CLICKHOUSE_PASSWORD` | Credenciais próprias; a senha não pode ser `ndovu`. |
| `NDOVU_AUTH_SECRET` | Segredo aleatório com pelo menos 32 caracteres. Gere, por exemplo, com `openssl rand -hex 32`. Trocar esse segredo invalida todos os JWTs ativos. |
| `NDOVU_SESSION_ENC_KEY` | Chave AES-256 usada pelo BFF do dashboard para cifrar o cookie httpOnly de sessão. Gere com `openssl rand -hex 16` (≥ 32 caracteres). A API recusa iniciar sem ela em produção. Trocar derruba todas as sessões do dashboard (JWTs já emitidos continuam válidos até o `exp`). |
| `NDOVU_ADMIN_EMAIL`, `NDOVU_ADMIN_PASSWORD` | Conta inicial; senha com pelo menos 12 caracteres. A conta só é criada quando ainda não há usuários; em bancos existentes, a variável não redefine a senha. |
| `NDOVU_CORS_ORIGINS` | Lista separada por vírgulas das origens exatas que acessam a API **direto do browser** (SDKs de clientes web), todas com `https://`; não use `*`. O dashboard não precisa constar: ele é same-origin com o BFF e chama a API pela rede interna do Compose. |
| `NEXT_PUBLIC_NDOVU_API` | Mantida apenas por compatibilidade de build. O dashboard é **same-origin**: a UI chama `/api/*` do próprio Next (BFF), e o BFF chama a API via rede interna (`NDOVU_API=http://api:8080`, já fixo no Compose de produção). O token não transita mais pelo browser. |
| `NDOVU_APP_DOMAIN`, `NDOVU_API_DOMAIN` | Domínios públicos do dashboard e da API, usados pelo Caddy. |
| `NDOVU_TLS_EMAIL` | E-mail para avisos do ACME/Let's Encrypt. |
| `NDOVU_S3_ENDPOINT`, `NDOVU_S3_ACCESS_KEY`, `NDOVU_S3_SECRET_KEY`, `NDOVU_SNAPSHOT_BUCKET` | Credenciais e bucket privados de snapshots. `NDOVU_S3_USE_SSL` é forçado a `true` no perfil de produção. |
| `NDOVU_CLICKHOUSE_S3_ENDPOINT`, `NDOVU_CLICKHOUSE_S3_ACCESS_KEY`, `NDOVU_CLICKHOUSE_S3_SECRET_KEY` | Credenciais próprias do cold tier do ClickHouse. Use um bucket R2 separado, como `ndovu-cold`; o endpoint deve terminar com `/ndovu-cold/`. |
| `NDOVU_PG_BACKUP_S3_ENDPOINT`, `NDOVU_PG_BACKUP_S3_ACCESS_KEY`, `NDOVU_PG_BACKUP_S3_SECRET_KEY`, `NDOVU_PG_BACKUP_BUCKET` | Credenciais exclusivas do bucket `ndovu-prod-backups`; o endpoint HTTPS do R2 deve apontar para a conta. |
| `NDOVU_PG_BACKUP_AGE_RECIPIENT` | Recipient público age (`age1...`). A identidade privada correspondente deve ser guardada fora da VPS. |

O bootstrap da chave de ingestão `"dev"` é desativado em produção. Depois do
primeiro login, crie uma chave por aplicação em **Chaves de API**. Isso evita
semear uma chave de desenvolvimento inútil ou reutilizada no ambiente real.

## Validar configuração

Com `.env.production` preenchido, valide sem iniciar containers:

```bash
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml config --quiet
```

O perfil define `NDOVU_ENV=production`; a API recusa iniciar se encontrar
segredos ausentes/fracos, senha padrão, CORS wildcard ou storage de snapshots
sem TLS. O writer valida apenas as credenciais de banco que realmente usa e
não recebe os segredos exclusivos da API/R2.

## Rede e HTTPS preparados

O overlay `docker-compose.prod.yml` remove as portas públicas de Postgres,
ClickHouse, NATS, MinIO, API e dashboard. O único serviço publicado é o Caddy,
nas portas 80/443, encaminhando:

- `NDOVU_APP_DOMAIN` → `dashboard:3000`;
- `NDOVU_API_DOMAIN` → `api:8080`.

O Caddy solicita e renova certificados automaticamente via ACME quando os
domínios estiverem apontados para a VPS. Antes disso, não inicie o perfil
publicamente: os domínios de exemplo não emitirão certificados úteis.

Quando o DNS estiver pronto, confirme que 80 e 443 chegam à VPS, preencha os
domínios reais e valide:

```bash
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml config --quiet
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

As portas de administração dos serviços permanecem acessíveis apenas dentro
da rede Docker. O firewall da VPS ainda deve permitir somente SSH
administrativo e TCP 80/443; essa regra é externa ao Compose.

## Cold tier do ClickHouse no R2

O perfil de produção substitui o `storage.xml` local/MinIO por
`infra/clickhouse/storage-r2.xml`. O R2 usado para os arquivos frios do
ClickHouse é independente do bucket `ndovu-snapshots`, com credencial própria e
permissões limitadas a esse bucket. O ClickHouse continua usando os volumes
locais para dados hot/warm e para os metadados do disco frio; o R2 não elimina
essa necessidade.

No Cloudflare, crie um bucket `ndovu-cold` e uma credencial exclusiva para ele.
Não reutilize a credencial do bucket de snapshots. O perfil mantém MinIO
disponível somente sob o profile explícito `local-minio`, para desenvolvimento;
ele não é iniciado pelo Compose de produção.

Antes de apontar uma instalação com dados existentes para o novo cold tier,
faça backup e valide o estado das tabelas/políticas. A alteração de endpoint
não deve ser usada para mover ou apagar parts manualmente; a movimentação deve
ser feita pelo TTL/ClickHouse após a configuração ser validada.

## Backup e restore do PostgreSQL

O serviço one-shot `postgres-backup` gera um dump PostgreSQL custom, cifra o
stream com age antes de gravá-lo em disco e envia o arquivo `.dump.age` ao
bucket R2 de backup. Os objetos ficam em `postgres/YYYY/MM/DD/`; a retenção é
controlada pela lifecycle rule de 30 dias do bucket. As credenciais R2 deste
fluxo devem ter acesso exclusivo a `ndovu-prod-backups`.

### Chave de criptografia

Gere uma identidade age em uma máquina confiável diferente da VPS:

```bash
age-keygen -o ndovu-backup-identity.txt
age-keygen -y ndovu-backup-identity.txt
```

Guarde `ndovu-backup-identity.txt` em um gerenciador de segredos/offline,
fora da VPS e fora do repositório. Configure somente o recipient público
(`age1...`, exibido pelo segundo comando) em
`NDOVU_PG_BACKUP_AGE_RECIPIENT` no `.env.production`. Sem essa identidade
privada, os backups não podem ser descriptografados.

### Executar e agendar

Com Postgres saudável e o recipient preenchido:

```bash
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml --profile backup \
  run --rm postgres-backup
```

O container verifica o upload por `HeadObject` e imprime a URI gerada. Para
agendamento diário às 02:15 UTC, ajuste o caminho `/opt/ndovu` se necessário e
instale o timer:

```bash
sudo cp infra/backup/ndovu-postgres-backup.{service,timer} /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now ndovu-postgres-backup.timer
systemctl list-timers ndovu-postgres-backup.timer
```

Para inspecionar a última execução: `journalctl -u ndovu-postgres-backup.service`.

Antes de ativar o timer, faça o round-trip sintético (usa bancos temporários em
tmpfs, uma identidade age descartável e remove o objeto de teste do R2):

```bash
bash infra/backup/test-postgres-backup.sh
```

### Restaurar

Restaure sempre primeiro em um PostgreSQL vazio/temporário. Monte a identidade
privada age de forma somente leitura e defina no serviço de restore as
variáveis PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE para o destino temporário,
além de `AGE_IDENTITY_FILE=/run/secrets/ndovu-backup-identity.txt`. Execute a
imagem de backup com entrypoint `/usr/local/bin/postgres-restore` e argumento
`s3://ndovu-prod-backups/<object-key>`. O script baixa e descriptografa em
stream e usa `pg_restore --single-transaction`; não executa DROP nem aponta
automaticamente ao banco de produção. Valide tabelas, usuários e chaves antes
de promover qualquer restauração.

## Sessão do dashboard (BFF, httpOnly)

O dashboard não guarda credencial no navegador. No login, o BFF do Next
(`/api/auth/login`) chama a API, recebe o JWT e o devolve **somente ao
servidor**, que o grava em um cookie `ndovu_session` `httpOnly` + `Secure` +
`SameSite=Lax`, cifrado com AES-256-GCM via `NDOVU_SESSION_ENC_KEY`. A cada
request autenticada o BFF decifra o cookie no servidor e chama a API com
`Authorization: Bearer` server-to-server, revalidando em `/v1/auth/me` (ou
seja: desativação de usuário e revogação têm efeito imediato, não apenas no
`exp` do JWT).

Consequências de operação:

- O browser **nunca** vê o token — um XSS não consegue lê-lo, e um cookie
  roubado sem a chave de cifra (que vive só no processo do servidor) não
  decifra sessão alguma.
- Trocar `NDOVU_SESSION_ENC_KEY` encerra todas as sessões logadas.
- O rate limit de login/consulta por IP vive na Caddy (ver
  `infra/caddy/Caddyfile`); a API Go continua aplicando o rate por
  `X-Api-Key` na ingestão (`NDOVU_INGEST_RATE_RPS`).
- O middleware do Next protege navegação por **presença** do cookie (edge
  runtime não tem `node:crypto`); a validade é checada na primeira chamada
  autenticada. Não é proteção criptográfica no edge — é UX de redirect.
