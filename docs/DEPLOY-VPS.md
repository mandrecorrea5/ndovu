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
| `NDOVU_API_KEY_ENC_KEY` | Chave aleatória estável (mínimo 32 caracteres), gerada com `openssl rand -hex 32`, para cifrar chaves de ingestão recuperáveis. Faça backup seguro e não a rotacione sem recifrar o histórico. |
| `NDOVU_GITHUB_STATUS_TOKEN` | Fine-grained GitHub token da VPS, restrito ao repositório Ndovu e à permissão **Commit statuses: read and write**. Usado pelo timer para confirmar o deploy do SHA aprovado; mantenha somente no `.env.production` com modo `600`. |
| `NDOVU_SESSION_ENC_KEY` | Chave AES-256 usada pelo BFF do dashboard para cifrar o cookie httpOnly de sessão. Gere com `openssl rand -hex 16` (≥ 32 caracteres). A API recusa iniciar sem ela em produção. Trocar derruba todas as sessões do dashboard (JWTs já emitidos continuam válidos até o `exp`). |
| `NDOVU_ADMIN_EMAIL`, `NDOVU_ADMIN_PASSWORD` | Conta inicial; senha com pelo menos 12 caracteres. A conta só é criada quando ainda não há usuários; em bancos existentes, a variável não redefine a senha. |
| `NDOVU_CORS_ORIGINS` | Lista separada por vírgulas das origens exatas que acessam a API **direto do browser** (SDKs de clientes web), todas com `https://`; não use `*`. O dashboard não precisa constar: ele é same-origin com o BFF e chama a API pela rede interna do Compose. |
| `NEXT_PUBLIC_NDOVU_API` | Mantida apenas por compatibilidade de build. O dashboard é **same-origin**: a UI chama `/api/*` do próprio Next (BFF), e o BFF chama a API via rede interna (`NDOVU_API=http://api:8080`, já fixo no Compose de produção). O token não transita mais pelo browser. |
| `NDOVU_APP_DOMAIN`, `NDOVU_API_DOMAIN` | Hostnames públicos do dashboard e da API, cadastrados no Cloudflare Tunnel e roteados pelo Caddy. |
| `NDOVU_CLOUDFLARED_TUNNEL_TOKEN` | Token secreto do Tunnel criado no Cloudflare Zero Trust. Guarde no `.env.production` (ou nas variáveis protegidas da stack no Portainer), nunca no Git. |
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

## Cloudflare Tunnel, rede e HTTPS

O overlay `docker-compose.prod.yml` não publica portas da aplicação na VPS.
O `cloudflared` inicia conexões de saída para a Cloudflare e alcança o Caddy
por uma rede Docker dedicada; somente o Caddy participa também da rede interna
dos serviços Ndovu. Não é necessário abrir portas de entrada 80/443 nem
8443 para o Ndovu, nem apontar um registro DNS para o IP público da VPS.

### Criar e configurar o Tunnel

1. No Cloudflare Zero Trust, crie um Tunnel do tipo **Cloudflared** e copie o
   token. Configure `NDOVU_CLOUDFLARED_TUNNEL_TOKEN` no `.env.production` da
   VPS, com permissões `600`, ou nas variáveis protegidas da stack no Portainer.
   Não cole o token em arquivos versionados nem o compartilhe em logs.
2. Nas rotas públicas do Tunnel, cadastre os dois hostnames com serviço de
   origem HTTP na rede Docker:
   - `NDOVU_APP_DOMAIN` → `http://caddy:80`;
   - `NDOVU_API_DOMAIN` → `http://caddy:80`.
3. Em **Additional application settings**, deixe **HTTP** em **Using defaults**
   e não configure opções de TLS de origem. O Host HTTP original deve ser
   preservado para o Caddy escolher a rota correta.
4. Deixe a Cloudflare criar/gerenciar os registros DNS do Tunnel. O domínio
   deve estar ativo na zona Cloudflare; não crie registros A apontando para a
   VPS para estes hostnames.

O TLS público termina na Cloudflare. Entre `cloudflared` e Caddy, o tráfego
usa HTTP dentro da rede Docker privada da stack, sem publicar a porta 80 no
host. O Caddy continua encaminhando `/api/*` ao backend e o restante do
hostname do app ao dashboard. A rede Docker do Tunnel não dá ao `cloudflared`
acesso direto aos bancos nem aos demais serviços.

Valide e suba a stack após configurar o token e as duas rotas:

```bash
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml config --quiet
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml up -d --build
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml logs --tail=100 cloudflared caddy
curl -sSf https://NDOVU_API_DOMAIN/health
```

As portas de entrada e regras de firewall de outros serviços da VPS não devem
ser alteradas como parte desta configuração. Para o Tunnel funcionar, o
container `cloudflared` precisa poder acessar a Internet para estabelecer suas
conexões de saída.

**`/metrics` protegido:** a rota de métricas da API (contadores de
tráfego/erros) exige Basic Auth no Caddy — usuário `ndovu`, senha cujo
hash bcrypt está em `NDOVU_METRICS_PASSWORD_HASH` (gere a senha com
`openssl rand -hex 24` e o hash com
`docker run --rm caddy:2.9-alpine caddy hash-password --plaintext '<senha>'`).
O scraper do monitoramento usa usuário/senha; `/health` permanece público
(é a sonda de uptime). Teste: sem credencial → 401; com a senha → 200.

**Rate limit:** a imagem oficial do Caddy 2.9 **não** inclui diretiva de
rate limit — a proteção de ingestão vive na API Go (token bucket por
`X-Api-Key`, `NDOVU_INGEST_RATE_RPS`, default 50 rps). Não adicione
`rate_limit` ao Caddyfile sem validar a disponibilidade do módulo na
versão pinada (a config é validada no boot e o Caddy não sobe com
diretiva desconhecida).

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
