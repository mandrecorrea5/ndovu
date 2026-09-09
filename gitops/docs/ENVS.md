# Padrão de Configuração — ConfigMaps e Secrets por projeto

Regra fundamental: **a imagem é a mesma nos três ambientes.** Nada de
`ndovu-api:prd`. O que distingue dev de produção é exclusivamente o conteúdo
dos ConfigMaps e Secrets do namespace.

Exceção conhecida: o dashboard, que hoje embute a URL da API em build time
(risco R2 do [`README.md`](README.md)) — até isso ser corrigido, ele é o único
artefato com imagem por ambiente.

---

## 1. Convenção de nomes

| Objeto | Nome | Onde vive | Conteúdo |
|--------|------|-----------|----------|
| ConfigMap | `ndovu-common-config` | `ndovu-app-<env>` | Endpoints e ajustes comuns a api + writer |
| ConfigMap | `ndovu-api-config` | `ndovu-app-<env>` | Só o que a API usa |
| ConfigMap | `ndovu-writer-config` | `ndovu-app-<env>` | Só o que o writer usa |
| ConfigMap | `ndovu-dashboard-config` | `ndovu-app-<env>` | Runtime do Next.js |
| Secret | `ndovu-postgres-credentials` | ambos os namespaces | URL/usuário/senha do control plane |
| Secret | `ndovu-clickhouse-credentials` | ambos os namespaces | Usuário/senha do ClickHouse |
| Secret | `ndovu-auth-credentials` | `ndovu-app-<env>` | Segredo JWT e bootstrap do admin |
| Secret | `ndovu-s3-credentials` | ambos os namespaces | Access/secret key do bucket |
| Secret | `ndovu-smtp-credentials` | `ndovu-app-<env>` | SMTP ou API key do SendGrid |

Por que Secrets por **domínio** e não um por aplicação: a rotação da senha do
Postgres não deve exigir tocar no Secret que guarda o segredo do JWT, e o time
de banco precisa de permissão de escrita só no dele.

Nos Deployments, o consumo é por `envFrom` (ConfigMaps e Secrets inteiros),
não `env` item a item — adicionar uma variável nova vira mudança de ConfigMap,
sem alterar o Deployment.

Ordem de precedência no pod (a última vence):
`ndovu-common-config` → `ndovu-<workload>-config` → Secrets → `env` inline
(só `NDOVU_PORT` e afins que nunca mudam por ambiente).

---

## 2. `ndovu-api` — API de ingestão, consulta e administração

### 2.1 ConfigMap `ndovu-common-config` (compartilhado com o writer)

| Variável | dev | hml | prd | Nota |
|----------|-----|-----|-----|------|
| `NDOVU_CLICKHOUSE_ADDR` | `clickhouse.ndovu-data-dev.svc:9000` | `…-hml.svc:9000` | `…-prd.svc:9000` | protocolo nativo, não HTTP |
| `NDOVU_CLICKHOUSE_DB` | `ndovu` | `ndovu` | `ndovu` | |
| `NDOVU_STREAM_BACKEND` | `nats` | `nats` | `nats` | buffer de ingestão: `nats` \| `kafka` \| `rabbitmq`. Seleciona quais URLs abaixo são usadas |
| `NDOVU_NATS_URL` | `nats://nats.ndovu-data-dev.svc:4222` | `…-hml` | `…-prd` | usado quando `STREAM_BACKEND=nats` |
| `NDOVU_KAFKA_BROKERS` | `kafka.ndovu-data-dev.svc:9092` | `…-hml` | `…-prd` | usado quando `STREAM_BACKEND=kafka` |
| `NDOVU_RABBITMQ_URL` | `amqp://…@rabbitmq.ndovu-data-dev.svc:5672/` | `…-hml` | `…-prd` | usado quando `STREAM_BACKEND=rabbitmq` |
| `NDOVU_STREAM_MAX_AGE_HOURS` | `48` | `24` | `12` | janela de replay do buffer; dita o PVC do broker (ver risco R7 e CAPACITY §4.2) |
| `NDOVU_LOG_LEVEL` | `debug` | `info` | `info` | |
| `NDOVU_S3_ENDPOINT` | endpoint da OBC | idem | idem | **vazio desliga snapshots** (`/v1/snapshots` → 503) |
| `NDOVU_S3_USE_SSL` | `false` | `true` | `true` | |
| `NDOVU_SNAPSHOT_BUCKET` | `ndovu-snapshots-dev` | `-hml` | `-prd` | vem do `configMap` gerado pela ObjectBucketClaim |
| `NDOVU_DIGEST_DASHBOARD_URL` | `https://ndovu-dev.apps…` | hml | prd | entra nos links do e-mail |

### 2.2 ConfigMap `ndovu-api-config`

| Variável | dev | hml | prd | Nota |
|----------|-----|-----|-----|------|
| `NDOVU_PORT` | `8080` | `8080` | `8080` | porta do container |
| `NDOVU_CORS_ORIGINS` | `*` | domínio hml | **lista explícita** | `*` em produção libera qualquer origem no backoffice |
| `NDOVU_MAX_BODY_BYTES` | `1048576` | `1048576` | `2097152` | teto do lote de eventos; casar com o `proxy-body-size` da Route |
| `NDOVU_AUTH_TOKEN_TTL_HOURS` | `8` | `8` | `4` | validade do token de sessão |
| `NDOVU_KEY_CACHE_TTL_SECONDS` | `30` | `30` | `30` | cache de validação de `X-Api-Key`; **é a latência de propagação de uma revogação** |
| `NDOVU_INGEST_RATE_RPS` | `50` | `200` | por tier | rate limit por API key; `0` desliga. Ver CAPACITY |

### 2.3 Secrets consumidos pela API

| Secret | Chave | Origem do valor |
|--------|-------|-----------------|
| `ndovu-postgres-credentials` | `NDOVU_POSTGRES_URL` | gerado pelo CloudNativePG; em prd **com `sslmode=require`** |
| `ndovu-clickhouse-credentials` | `NDOVU_CLICKHOUSE_USER`, `NDOVU_CLICKHOUSE_PASSWORD` | criado junto do cluster ClickHouse |
| `ndovu-auth-credentials` | `NDOVU_AUTH_SECRET` | **aleatório, ≥32 bytes, por ambiente**. Trocar invalida todas as sessões |
| | `NDOVU_ADMIN_EMAIL`, `NDOVU_ADMIN_PASSWORD` | bootstrap do primeiro admin; senha aleatória em prd |
| | `NDOVU_BOOTSTRAP_INGEST_KEY` | chave de ingestão inicial; em prd **gerar aleatória e revogar** pela tela de Chaves de API após criar as reais |
| `ndovu-s3-credentials` | `NDOVU_S3_ACCESS_KEY`, `NDOVU_S3_SECRET_KEY` | Secret da ObjectBucketClaim |
| `ndovu-smtp-credentials` | `NDOVU_SMTP_*` ou `NDOVU_SENDGRID_API_KEY` | relay corporativo / SendGrid |

> As três chaves de bootstrap (`AUTH_SECRET`, `ADMIN_PASSWORD`,
> `BOOTSTRAP_INGEST_KEY`) têm default embutido no `config.go`
> (`dev-secret-troque-em-producao`, `admin12345`, `dev-ingest-key`).
> **Se o Secret não for montado, a API sobe com esses valores** — não falha.
> Incluir na pipeline uma verificação que bloqueia o deploy em hml/prd se o
> Secret estiver ausente.

---

## 3. `ndovu-writer` — consumidor do stream e agendador

Consome `ndovu-common-config` (seção 2.1) e os Secrets de Postgres, ClickHouse,
S3 e SMTP. Não usa `NDOVU_AUTH_*` nem `NDOVU_CORS_*`.

### ConfigMap `ndovu-writer-config`

| Variável | dev | hml | prd | Nota |
|----------|-----|-----|-----|------|
| `NDOVU_WRITER_BATCH` | `64` | `256` | por tier | mensagens por fetch; maior = menos inserts, mais memória |
| `NDOVU_WRITER_MAX_WAIT_MS` | `1000` | `1000` | `500` | espera máxima para fechar um lote |
| `NDOVU_ALERTS_INTERVAL_SECONDS` | `60` | `60` | `60` | intervalo de avaliação das regras de alerta |
| `NDOVU_SAMPLING_REFRESH_SECONDS` | `30` | `30` | `30` | refresh do cache de sampling |
| `NDOVU_ANOMALY_INTERVAL_SECONDS` | `300` | `300` | `300` | cada ciclo faz consultas pesadas no ClickHouse |
| `NDOVU_DIGEST_RECIPIENTS` | vazio | e-mails de teste | lista real | **vazio desliga o digest** |
| `NDOVU_DIGEST_WEEKDAY` | `0` | `0` | `0` | 0 = domingo |
| `NDOVU_DIGEST_HOUR_UTC` | `20` | `20` | `11` | 11 UTC ≈ 08h BRT de segunda |
| `NDOVU_DIGEST_MINUTE_UTC` | `0` | `0` | `0` | |
| `NDOVU_DIGEST_TICK_SECONDS` | `300` | `300` | `300` | granularidade do agendador interno |
| `NDOVU_MAILER_PROVIDER` | `noop` | `smtp` | `smtp` ou `sendgrid` | vazio = auto (SendGrid se houver chave, senão SMTP, senão noop) |

> **Os quatro loops de agendamento vivem neste workload** (alertas, anomalias,
> digest, refresh de sampling). É por isso que ele é singleton — ver risco R1.
> Ao dividir em `cmd/scheduler`, as variáveis `ALERTS_`, `ANOMALY_`, `DIGEST_`
> e `SAMPLING_` migram para um `ndovu-scheduler-config`, e este ConfigMap fica
> só com `WRITER_BATCH` e `WRITER_MAX_WAIT_MS`.

---

## 4. `ndovu-dashboard`

| Variável | Tipo | Nota |
|----------|------|------|
| `NEXT_PUBLIC_NDOVU_API` | **build-time** (`ARG` do Dockerfile) | URL da API **vista pelo browser do usuário** — nunca o Service interno. Ver risco R2 |
| `PORT` | runtime | `3000` |
| `HOSTNAME` | runtime | `0.0.0.0` — sem isso o `server.js` standalone escuta em `localhost` e a probe falha |
| `NODE_ENV` | runtime | `production` |

O `ndovu-dashboard-config` existe hoje só com `PORT`/`HOSTNAME`/`NODE_ENV`.
Ele passa a carregar a URL da API quando o R2 for corrigido — e é o motivo de
criar o ConfigMap desde já, mesmo quase vazio: a mudança futura não mexe no
Deployment.

Valores de `NEXT_PUBLIC_NDOVU_API` por ambiente:

| Ambiente | Valor |
|----------|-------|
| dev | `https://api-ndovu-dev.apps.<cluster>` |
| hml | `https://api-ndovu-hml.apps.<cluster>` |
| prd | `https://api.ndovu.<domínio-corporativo>` |

---

## 5. `ndovu-platform` — camada de dados

| Componente | Objeto | Chaves |
|------------|--------|--------|
| ClickHouse | ConfigMap `clickhouse-storage-config` | `storage.xml` (policy `tiered`: hot/warm/cold) — hoje em `infra/clickhouse/` |
| | Secret `ndovu-clickhouse-credentials` | `CLICKHOUSE_USER`, `CLICKHOUSE_PASSWORD`, `CLICKHOUSE_DB` |
| | ConfigMap `clickhouse-tuning` | `max_memory_usage`, `background_pool_size`, `merge_max_block_size` por tier |
| PostgreSQL | Secret `ndovu-postgres-credentials` | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `NDOVU_POSTGRES_URL` |
| NATS | ConfigMap `nats-config` | `nats-server.conf` com JetStream file store, `max_file_store` casado com o PVC |
| Kafka/Redpanda | ConfigMap `kafka-config` | alternativa a NATS quando `NDOVU_STREAM_BACKEND=kafka` (broker + tópicos `ndovu.events`/`ndovu.dlq`) |
| RabbitMQ | ConfigMap `rabbitmq-config` | alternativa a NATS quando `NDOVU_STREAM_BACKEND=rabbitmq` (filas `ndovu.events`/`ndovu.dlq`) |
| Bucket | ObjectBucketClaim `ndovu-cold`, `ndovu-snapshots` | gera ConfigMap (endpoint/nome) + Secret (chaves) automaticamente |

O `storage.xml` referencia endpoint e credenciais do bucket cold **dentro do
XML** — no OpenShift ele vira um template preenchido pelo overlay do ambiente
a partir do Secret da OBC, não um arquivo fixo commitado com senha.

---

## 6. Como os Secrets chegam no cluster

Nada de `oc create secret` manual e nada de valor real no Git.

| Opção | Quando usar |
|-------|-------------|
| **External Secrets Operator + Vault/CyberArk** | Recomendada se a organização já tem cofre. O Git guarda só o `ExternalSecret` apontando o caminho |
| **Sealed Secrets** | Se não há cofre. O Git guarda o cifrado, só o controller do cluster decifra |
| Secrets gerados por operador | Postgres (CloudNativePG) e buckets (OBC) já geram os seus — o overlay só referencia |

Rotação: `NDOVU_AUTH_SECRET` derruba todas as sessões ao mudar (aceitável,
agendar fora do horário). Senha do Postgres e do ClickHouse exigem restart dos
pods de api/writer — o rollout resolve. `NDOVU_BOOTSTRAP_INGEST_KEY` só tem
efeito quando não existe nenhuma chave cadastrada; depois disso é inerte.

---

## 7. Checklist antes de promover para produção

- [ ] `NDOVU_AUTH_SECRET` aleatório, ≥32 bytes, diferente de hml
- [ ] `NDOVU_ADMIN_PASSWORD` aleatória; senha trocada no primeiro acesso
- [ ] `NDOVU_BOOTSTRAP_INGEST_KEY` revogada após criar as chaves reais por app
- [ ] `NDOVU_CORS_ORIGINS` com lista explícita, sem `*`
- [ ] `NDOVU_POSTGRES_URL` com `sslmode=require`
- [ ] `NDOVU_S3_USE_SSL=true`
- [ ] `NDOVU_STREAM_MAX_AGE_HOURS` casado com o PVC do broker de stream ([`CAPACITY.md`](CAPACITY.md))
- [ ] `NDOVU_DIGEST_RECIPIENTS` apontando para a lista real (ou vazio conscientemente)
- [ ] Nenhum ConfigMap contendo valor que deveria ser Secret (`grep -i 'password\|secret\|key'` nos overlays)
