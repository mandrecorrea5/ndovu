# backend (clone do ndovu-backend)

API de ingestão/consulta e Writer do Ndovu. Módulo Go único, **uma imagem com
dois entrypoints**, dois Deployments independentes no OpenShift.

> Este README é o modelo para o root do repositório `ndovu/backend`.
> Origem: `backend/` do monorepo Ndovu.

## O que este repositório é

O núcleo da plataforma. Tudo o que grava ou lê trace mora aqui.

| Binário | Papel |
|---------|-------|
| `cmd/api` | Recebe eventos do contrato v1 (`POST /v1/events`, `X-Api-Key`), valida e **publica no JetStream sem esperar o banco**. Serve toda a consulta autenticada (sessões, traces, issues, funis, métricas) e a administração (usuários, apps, empresas, chaves, alertas, sampling, GDPR, auditoria). |
| `cmd/writer` | Consome o JetStream em lote, aplica sampling e insere em bulk no ClickHouse. Hospeda também os loops de **avaliação de alertas, detecção de anomalias e digest semanal** — por isso roda em **1 réplica** (ver "Restrições operacionais"). |

Os dois compartilham `internal/` inteiro: `config`, `domain`, `usecase` e os
adapters de ClickHouse, PostgreSQL, NATS e blob store. Separar os binários em
dois repositórios exigiria versionar esse núcleo — e é justamente onde eles
precisam concordar (formato da mensagem no stream, schema, migrações).

Também são deste repositório, por estarem `go:embed`ados nos binários:

- `api/openapi.yaml` — especificação do contrato, servida em `/openapi.yaml` e `/docs`
- `internal/adapter/ctlpostgres/migrations/*.sql` — migrações do control plane
- `internal/adapter/clickhouse/schema.sql` — schema de `trace_events`

## Estrutura

```
cmd/api/            composition root da API
cmd/writer/         composition root do writer
internal/
  config/           Config.Load() — toda variável NDOVU_* nasce aqui
  domain/           entidades e ports (interfaces)
  usecase/          regras de negócio, sem dependência de infra
  adapter/
    httpapi/        rotas, handlers, middlewares (auth, CORS, rate limit)
    clickhouse/     repositório de traces + schema.sql
    ctlpostgres/    control plane + migrações embedadas
    natsstream/     publisher, consumer e criação do stream
    blobstore/      S3/MinIO para snapshots de sessão
  platform/         logger, métricas Prometheus, mailer (SMTP/SendGrid/noop)
api/                openapi.yaml + embed.go
dev/                compose.yml para desenvolvimento local
Dockerfile          multi-stage → distroless, dois binários
```

## Arquitetura em uma frase

A ingestão nunca espera o banco: a API publica no JetStream em ~1ms e responde;
o writer drena no ritmo do ClickHouse. Se o banco cair, nada se perde — as
mensagens ficam no stream pela janela de `NDOVU_STREAM_MAX_AGE_HOURS`.

## Desenvolvimento local

```bash
docker compose -f dev/compose.yml up -d postgres clickhouse nats minio mailhog
go mod tidy
go run ./cmd/api        # :8080
go run ./cmd/writer     # outro terminal
```

Os defaults do `config.go` casam com o compose. Para gerar carga de exemplo,
use o `seed` do repositório `ndovu-sdk`.

O buffer de ingestão é agnóstico: escolha o backend por `NDOVU_STREAM_BACKEND`
(`nats` default, `kafka` ou `rabbitmq`). O Kafka (Redpanda) e o RabbitMQ não
sobem por padrão — habilite o perfil correspondente junto com a variável:

```bash
# Kafka (Redpanda)
NDOVU_STREAM_BACKEND=kafka docker compose -f dev/compose.yml --profile kafka up -d postgres clickhouse nats minio mailhog kafka
# RabbitMQ
NDOVU_STREAM_BACKEND=rabbitmq docker compose -f dev/compose.yml --profile rabbitmq up -d postgres clickhouse nats minio mailhog rabbitmq
```

> A variável precisa chegar ao container. No fluxo acima ela vale para o `api`
> e `writer` via `x-app-env` (o compose já injeta as URLs dos brokers). Se você
> roda `go run` no host, exporte também `NDOVU_KAFKA_BROKERS=localhost:9092` ou
> `NDOVU_RABBITMQ_URL=amqp://ndovu:ndovu@localhost:5672/`.

```bash
go test -race ./...                    # unitários
go test -tags=integration ./...        # testcontainers (Postgres, ClickHouse, MinIO, Kafka/Redpanda, RabbitMQ)
```

## Build da imagem

```bash
docker build -t ndovu/backend:$(git rev-parse --short HEAD) .
```

Multi-stage: `golang:1.25-alpine` para compilar, `distroless/static-debian12:nonroot`
para rodar. Dois binários em uma imagem — `/ndovu-api` (ENTRYPOINT default) e
`/ndovu-writer`. Não há CGO; o binário estático roda sob qualquer UID, o que
satisfaz a SCC `restricted-v2` do OpenShift sem ajustes.

## Deploy no OpenShift

Este repositório **não contém manifests**. O estado desejado vive no
`ndovu-gitops`; aqui a pipeline apenas publica a imagem com a tag do SHA.

| Objeto no cluster | Como usa esta imagem |
|-------------------|----------------------|
| `Deployment/ndovu-api` | entrypoint default; Route `api.<domínio>` e `ingest.<domínio>` |
| `Deployment/ndovu-writer` | `command: ["/ndovu-writer"]`; sem Service, sem Route |
| `Job/ndovu-migrate` | PreSync hook do Argo CD; aplica migrações antes do rollout |

Fluxo: merge em `main` → CI (vet, testes, integração, build, scan) → push
`ndovu/backend:<sha>` → PR no `ndovu-gitops` alterando a tag do overlay →
Argo CD sincroniza.

Configuração: **nenhum valor de ambiente vive neste repositório.** Todas as
variáveis, com o ConfigMap ou Secret de destino e o valor por ambiente, estão
documentadas em `ndovu-gitops/docs/ENVS.md`. Ao adicionar uma variável nova no
`config.go`, a mesma PR deve abrir a issue de atualização daquele documento —
caso contrário a variável sobe com o default embutido e ninguém percebe.

Procedimento completo (ordem de subida, probes, migrações, observabilidade):
`ndovu-gitops/docs/DEPLOY.md`.

## Restrições operacionais

**O writer é singleton.** `cmd/writer/main.go` inicia, além do consumidor, as
goroutines de avaliação de alertas, detecção de anomalias, digest semanal e
refresh de sampling. Com N réplicas, cada alerta dispara N vezes e o digest
semanal é enviado N vezes. Enquanto essa separação não existir:

- `replicas: 1`, sem HPA
- estratégia de rollout `Recreate` (nunca dois writers simultâneos)
- o alerta de `num_pending` do JetStream é o principal sinal de saúde

Correção planejada: extrair `cmd/scheduler` como binário separado (singleton),
deixando `cmd/writer` puramente stateless e escalável horizontalmente. É a
mudança de maior impacto no roadmap de produção.

**Migrações rodam no boot.** O Postgres usa advisory lock e é seguro com
réplicas; o ClickHouse não tem proteção equivalente. Em produção, o schema é
aplicado pelo Job `ndovu-migrate` antes do rollout. A flag
`NDOVU_SKIP_MIGRATIONS` (a implementar) desliga a aplicação no boot dos pods.

**`/metrics`, `/docs` e `/openapi.yaml` são públicos** e compartilham o
listener das rotas de negócio. Não expor esses paths na Route de internet;
a correção definitiva é um listener administrativo em porta separada.

**Defaults inseguros existem.** `NDOVU_AUTH_SECRET`, `NDOVU_ADMIN_PASSWORD` e
`NDOVU_BOOTSTRAP_INGEST_KEY` têm valor default no `config.go` — se o Secret não
for montado, a aplicação **sobe** com eles em vez de falhar. A pipeline de
hml/prd deve bloquear o deploy quando o Secret estiver ausente.

## Contratos com outros repositórios

| Repositório | Relação |
|-------------|---------|
| `ndovu-sdk` | Consome o contrato v1. Mudança incompatível em `api/openapi.yaml` exige nova versão major do SDK |
| `ndovu-dashboard` | Cliente exclusivo das rotas `/v1/*` autenticadas por JWT |
| `ndovu-platform` | Provê ClickHouse, NATS, PostgreSQL e buckets. O schema é responsabilidade **deste** repositório; o servidor, daquele |
| `ndovu-gitops` | Consome a imagem publicada aqui |

O contrato v1 é público e versionado: qualquer frontend que envie o JSON
especificado está integrado, com ou sem SDK. Quebrá-lo quebra clientes que
este repositório não conhece.
