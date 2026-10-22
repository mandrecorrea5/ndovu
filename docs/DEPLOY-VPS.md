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
| `NDOVU_CORS_ORIGINS` | Lista global legada de origens HTTPS para integrações sem origens cadastradas no app; não use `*`. Para novos sites, cadastre as origens em **Apps** — não é necessário alterar esta variável a cada cliente. O dashboard é same-origin com o BFF e não precisa constar. |
| `NEXT_PUBLIC_NDOVU_API` | Mantida apenas por compatibilidade de build. O dashboard é **same-origin**: a UI chama `/api/*` do próprio Next (BFF), e o BFF chama a API via rede interna (`NDOVU_API=http://api:8080`, já fixo no Compose de produção). O token não transita mais pelo browser. |
| `NDOVU_APP_DOMAIN`, `NDOVU_API_DOMAIN` | Hostnames públicos do dashboard e da API, cadastrados no Cloudflare Tunnel e roteados pelo Caddy. |
| `NDOVU_OTLP_DOMAIN` | Opcional. Hostname público do receptor OTLP (ex.: `otlp.ndovu.solutions`), para backends que exportam OpenTelemetry. Sem ela, o bloco do Caddy fica inerte (`otlp.invalid`). Mudar o valor exige recriar o container do Caddy (`up -d caddy`); `caddy reload` não lê variáveis novas. |
| `NDOVU_SPAN_QUOTA_ALERT_TO` | Opcional. Destinatário do e-mail de cota diária de spans estourada; o padrão são os destinatários do digest. |
| `NDOVU_CLOUDFLARED_TUNNEL_TOKEN` | Token secreto do Tunnel criado no Cloudflare Zero Trust. Guarde no `.env.production` (ou nas variáveis protegidas da stack no Portainer), nunca no Git. |
| `NDOVU_S3_ENDPOINT`, `NDOVU_S3_ACCESS_KEY`, `NDOVU_S3_SECRET_KEY`, `NDOVU_SNAPSHOT_BUCKET` | Credenciais e bucket privados de snapshots. `NDOVU_S3_USE_SSL` é forçado a `true` no perfil de produção. Provisionamento — criação do bucket, policy, retenção: seção "Bucket de snapshots no R2". |
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
2. Nas rotas públicas do Tunnel (**Public Hostname** / **Published
   application routes**), cadastre os hostnames com serviço de origem HTTP na
   rede Docker:
   - `NDOVU_APP_DOMAIN` → `http://caddy:80`;
   - `NDOVU_API_DOMAIN` → `http://caddy:80`;
   - `NDOVU_OTLP_DOMAIN` → `http://caddy:80` (opcional, só se for receber
     OTLP). Também aponta para o Caddy, nunca para `api:4318`: o
     `cloudflared` só enxerga o Caddy pela rede `tunnel`, e é o Caddy que
     encaminha para o listener OTLP.
3. Em **Additional application settings**, deixe **HTTP** em **Using defaults**
   e não configure opções de TLS de origem. O Host HTTP original deve ser
   preservado para o Caddy escolher a rota correta.
4. Deixe a Cloudflare criar/gerenciar os registros DNS do Tunnel. O domínio
   deve estar ativo na zona Cloudflare; não crie registros A apontando para a
   VPS para estes hostnames.
5. O hostname OTLP recebe tráfego só de servidores (exporters
   OpenTelemetry), nunca de browser. Se a zona tiver **Bot Fight Mode** ou
   regras de WAF com *challenge*, crie uma exceção (**Security → WAF → Custom
   rules → Skip**) para `NDOVU_OTLP_DOMAIN`; senão o exporter recebe 403 ou
   uma página HTML da Cloudflare em vez da resposta do Ndovu.

O TLS público termina na Cloudflare. Entre `cloudflared` e Caddy, o tráfego
usa HTTP dentro da rede Docker privada da stack, sem publicar a porta 80 no
host. O Caddy continua encaminhando `/api/*` ao backend e o restante do
hostname do app ao dashboard. A rede Docker do Tunnel não dá ao `cloudflared`
acesso direto aos bancos nem aos demais serviços.

Valide e suba a stack após configurar o token e as rotas:

```bash
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml config --quiet
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml up -d --build
docker compose --env-file .env.production \
  -f docker-compose.yml -f docker-compose.prod.yml logs --tail=100 cloudflared caddy
curl -sSf https://NDOVU_API_DOMAIN/health
```

Com o hostname OTLP configurado, confira também:

```bash
curl -sSf https://NDOVU_OTLP_DOMAIN/health          # 200
curl -si -X POST https://NDOVU_OTLP_DOMAIN/v1/traces \
  -H 'Content-Type: application/json' -d '{}'        # 401: resposta do Ndovu, rota ok
```

Um 404 vazio indica que o Caddy não recebeu `NDOVU_OTLP_DOMAIN` (recrie o
container). Um 502 ou erro 1033 indica destino errado no Tunnel ou
`cloudflared` parado. Uma página HTML de challenge indica bloqueio de bot/WAF
(item 5). O cadastro do app com **aceita OTLP** e a configuração do exporter
estão em `docs/INTEGRATION.md` §14.

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

## Bucket de snapshots no R2

O replay de sessão grava HTML gzip'ado num bucket S3-compatível. Em produção o
bucket é `ndovu-snapshots` no **R2** — não no MinIO. O MinIO do Compose existe
só para desenvolvimento local: `docker-compose.prod.yml` zera as portas e o
tranca no profile `local-minio`, então o Compose de produção nunca o sobe
(`docs/CAPACITY-VPS.md` §7 também assume R2).

O bucket fica **100% privado**. O dashboard não fala com o R2: o handler
`GetSnapshot` lê o objeto, descompacta e devolve `text/html` pela API, e o BFF do
dashboard repassa. Não configure política de acesso público, presigned URL nem
CORS para este bucket — nada nesse caminho precisa.

### Quem precisa das credenciais

Só o serviço `api`. A validação de produção que exige storage está dentro do
ramo `component == "api"` (`internal/config/config.go:235`), e exige
`NDOVU_S3_ENDPOINT`, `NDOVU_S3_ACCESS_KEY`, `NDOVU_S3_SECRET_KEY`,
`NDOVU_SNAPSHOT_BUCKET` e `NDOVU_S3_USE_SSL=true`. O `writer` consome NATS e
grava no ClickHouse; ele roda com `LoadFor("writer")` (`cmd/writer/main.go:33`),
que não valida storage, e por isso o Compose de produção **zera** essas três
variáveis no serviço `writer` de propósito. Não copie credencial de S3 para o
writer ao "consertar" ausência.

`NDOVU_S3_USE_SSL` é fixado em `"true"` pelo próprio Compose de produção
(`docker-compose.prod.yml`, serviço `api`). **Não defina no `.env.production`**:
o valor do Compose vence, e a variável ausente no arquivo não é erro de config.
Para rodar contra um R2/S3 real em ambiente local, aí sim defina
`NDOVU_S3_USE_SSL=true` — o Compose base tem default `false`
(`docker-compose.yml:26`), que é o valor correto para o MinIO local.

### Criar o bucket

O `minio-init` do Compose base é o único que cria bucket, e ele fica de fora da
produção. Em produção o bucket **não é criado por ninguém** — crie antes de
subir a API. Sem o bucket, o boot do `api` falha com
`bucket ndovu-snapshots não existe — cria via minio-init`
(`internal/adapter/blobstore/minio.go:46`); o serviço não degrada, ele não sobe.

```bash
# <acct> = account ID do R2 (aparece no dashboard da Cloudflare)
aws configure set aws_access_key_id     "$NDOVU_S3_ACCESS_KEY"   --profile r2
aws configure set aws_secret_access_key "$NDOVU_S3_SECRET_KEY"   --profile r2
aws configure set region auto                                    --profile r2

aws s3api create-bucket \
  --bucket ndovu-snapshots \
  --endpoint-url "https://<acct>.s3.r2.cloudflarestorage.com" \
  --profile r2
```

`ndovu-snapshots` é o default de `NDOVU_SNAPSHOT_BUCKET`
(`internal/config/config.go:181`). Usando esse nome, a variável dispensa definição.

### Formato do endpoint

Sem scheme. O adapter repassa `cfg.Endpoint` cru para o cliente MinIO
(`internal/adapter/blobstore/minio.go:36`), que espera host puro:

```
NDOVU_S3_ENDPOINT=<acct>.s3.r2.cloudflarestorage.com     # correto
NDOVU_S3_ENDPOINT=https://<acct>.s3.r2.cloudflarestorage.com   # quebra
```

Também sem barra final e sem o nome do bucket no path — o bucket vai em
`NDOVU_SNAPSHOT_BUCKET`.

### Credencial e escopo mínimo

O adapter executa exatamente três operações S3: `BucketExists`
(`minio.go:43`), `PutObject` (`minio.go:55`) e `GetObject` (`minio.go:68`). A
policy da credencial cobre estas três e nada além:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": ["arn:aws:s3:::ndovu-snapshots"]
    },
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject"],
      "Resource": ["arn:aws:s3:::ndovu-snapshots/*"]
    }
  ]
}
```

Não reutilize a credencial do cold tier do ClickHouse nem a dos backups — são
quatro grupos separados no `.env.production` (`NDOVU_S3_*`,
`NDOVU_CLICKHOUSE_S3_*`, `NDOVU_PG_BACKUP_S3_*`, `NDOVU_CH_BACKUP_S3_*`), um por
bucket, conforme a seção anterior.

### Retenção: configure no R2, não no código

**Nenhum caminho no backend apaga snapshot.** Não há `DeleteObject`/
`RemoveObject` em `blobstore` nem em lugar algum do `internal/`. O único
deletor de objeto é o `DeleteObject` que a policy deliberadamente não concede.
Isso significa que desligar replay **não** libera espaço: o objeto de um app
desativado continua no bucket para sempre.

A retenção, portanto, é uma **lifecycle rule no bucket**, sem código e sem
deploy. Configure a expiração pelo dashboard do R2 ou por
`put-lifecycle-configuration`. É o que torna o teto de body da rota de snapshot
seguro: `NDOVU_SNAPSHOT_MAX_BODY_BYTES` tem default 24 MB
(`internal/config/config.go:132`), porque a rota recebe a serialização
JSON-escaped de um `outerHTML` — o teto de 1 MB da ingestão comum cortava
qualquer tela com CSS embutido. Sem lifecycle, cada replay é crescimento
líquido permanente no bucket.

Isso também é contenção de PII, não só custo. O `captureDOM` do SDK mascara
`<input>` de senha/e-mail e elementos marcados com `data-ndovu-mask`, e remove
`<script>` — mas **não** redige texto plano genérico. Conteúdo marcado por nada
vai para o bucket como está. Retenção explícita é a única coisa limitando o
tempo que isso fica gravado.

### Verificar depois de configurar

```bash
# 1. bucket existe e a credencial enxerga
aws s3api head-bucket --bucket ndovu-snapshots \
  --endpoint-url "https://<acct>.s3.r2.cloudflarestorage.com" --profile r2

# 2. round-trip put/get/delete com a MESMA credencial da API
#    (a policy nega DeleteObject — espere 403 aqui; é o resultado correto)
echo teste | aws s3api put-object --bucket ndovu-snapshots --key _check/probe.txt \
  --body /dev/stdin \
  --endpoint-url "https://<acct>.s3.r2.cloudflarestorage.com" --profile r2

# 3. o api subiu com storage ligado: sem S3 o processo nem fica de pé
docker compose -f docker-compose.prod.yml logs api | tail -20
```

Depois, valide o caminho completo num app ativo. Dispare um erro real pela API
pública do SDK — `ndovu.error('teste_snapshot', { code: 'PROBE', message: '...' })`
é o que chega a gravar snapshot, porque a captura de tela só sai desse método — e
confirme que a sessão abre o replay renderizado. Prefira uma tela com CSS
embutido: snapshot pequeno **não** prova nada sobre o teto de body, e o caso que
quebrou antes foi justamente o de tela grande.

Referência da feature: `docs/features/ingest-snapshots.md`.

### Falhas esperadas e o que significam

| Sintoma | Causa |
|---|---|
| boot: `bucket ndovu-snapshots não existe — cria via minio-init` | Bucket não criado. `minio-init` não roda em produção. |
| boot: `configuração de produção inválida: NDOVU_S3_ENDPOINT, ...` | Uma das cinco variáveis ausente. `NDOVU_S3_USE_SSL` vem do Compose, não do `.env`. |
| `minio client:` ou endpoint inválido no boot | Scheme ou barra no `NDOVU_S3_ENDPOINT`, ou bucket no path. |
| `putting snapshot ...: Access Denied` | Policy sem `s3:PutObject`, ou credencial de outro bucket. |
| `verificando bucket ...: 404` | Nome do bucket ≠ `NDOVU_SNAPSHOT_BUCKET`, ou credencial sem `s3:ListBucket`. |
| Boot com erro de assinatura/redirect | Região. O adapter não define `Region` explicitamente (`minio.go:36`) e depende de detecção; no R2 o esperado é `auto`. |
| Replay sem estilo, só texto | Não é storage. O snapshot preserva os `<link href="/assets/...">` do HTML, e resolvidos contra a origem do dashboard eles dão 404. Falha conhecida do replay, sem correção no SDK. |
| `snapshots: 0` em app que dispara `ndovu.error()` | Verifique `envelope.app` da chave vs. app dos eventos; a rota de gravação rejeita com 400 antes de tocar no storage. |

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
