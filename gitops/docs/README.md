# Ndovu no OpenShift — Plano de Separação e Deploy

Este diretório contém o plano para tirar o Ndovu do monorepo/`docker-compose`
e colocá-lo no OpenShift, quebrado em **repositórios independentes** e
**projects (namespaces) por ambiente**, com configuração externalizada em
ConfigMaps/Secrets.

> **Escopo deste plano:** decisão de arquitetura, fronteiras de repositório,
> padrão de configuração, procedimento de deploy e dimensionamento de infra.
> **Não** contém manifests YAML — eles são o próximo passo, e o formato de cada
> um está especificado aqui (nome, chaves, origem do valor).

## Índice

| Documento | Conteúdo |
|-----------|----------|
| [`README.md`](README.md) (este) | Decomposição, namespaces, decisões e riscos |
| [`REQUIREMENTS.md`](REQUIREMENTS.md) | Checklist de pré-requisitos para subir o Ndovu no OpenShift (acesso, operadores, secrets, infra) |
| [`ENVS.md`](ENVS.md) | Padrão de ConfigMap/Secret e tabela completa de variáveis por projeto |
| [`CAPACITY.md`](CAPACITY.md) | Dimensionamento de CPU/memória/disco por projeto, em 3 tiers |
| [`DEPLOY.md`](DEPLOY.md) | Pipeline, GitOps, ordem de subida, rotas, probes, observabilidade, DR |
| README de cada repositório | O papel de cada um está no `README.md` do próprio repositório (`ndovu-backend`, `ndovu-dashboard`, `ndovu-sdk`, `ndovu-platform`) |

---

## 1. Ponto de partida

Hoje o repositório é um monorepo com quatro artefatos e seis dependências de
infraestrutura, todos amarrados por um `docker-compose.yml`:

```
backend/     Go — dois binários (cmd/api, cmd/writer), um único módulo
dashboard/   Next.js 15 standalone
sdk/         4 arquivos JS de SDK (browser, node, next, react-native)
tools/seed/  gerador de carga de exemplo
infra/       storage.xml do ClickHouse
docs/        arquitetura, contrato v1, docs por funcionalidade
```

Dependências: PostgreSQL 16 (control plane), ClickHouse 24.8 (traces),
buffer de ingestão (NATS JetStream por padrão, com alternativas Kafka e
RabbitMQ — o backend é selecionado por `NDOVU_STREAM_BACKEND`), MinIO/S3
(cold tier + snapshots), SMTP (digest semanal).

## 2. Decomposição em repositórios

**Seis repositórios.** A fronteira escolhida é *ciclo de release*: código que
sobe junto, versiona junto e é revisado pelo mesmo time fica no mesmo repo.

| # | Repositório | O que é | Produz | Time dono |
|---|-------------|---------|--------|-----------|
| 1 | `ndovu-backend` | Módulo Go único: API de ingestão/consulta + Writer. Contrato OpenAPI, migrações do Postgres e schema do ClickHouse embedados. | 1 imagem, 2 entrypoints → 2 (3) Deployments | Backend |
| 2 | `ndovu-dashboard` | Next.js 15 standalone — backoffice web. | 1 imagem → 1 Deployment | Frontend |
| 3 | `ndovu-sdk` | SDKs JS do contrato v1 (browser/node/next/react-native) + `seed`. | Pacote npm no registry interno | Backend/DX |
| 4 | `ndovu-platform` | Camada de dados: ClickHouse, NATS, PostgreSQL, buckets, `storage.xml`, políticas de retenção, backup/restore e runbooks. | Manifests + Helm/Kustomize da infra | Plataforma/SRE |
| 5 | `ndovu-gitops` | Fonte da verdade do que está rodando: overlays por ambiente, ConfigMaps, referências de Secret, Routes, NetworkPolicies, quotas, Applications do Argo CD. | Estado desejado dos clusters | SRE + Backend |
| 6 | `ndovu-docs` *(opcional)* | Documentação de produto, arquitetura e guia de integração. | Site estático / MkDocs | Todos |

### Por que a API e o Writer ficam no mesmo repositório

Os dois binários compartilham **100% de `backend/internal`** — `config`,
`domain`, `usecase`, e os adapters de ClickHouse, Postgres, stream (NATS/Kafka/
RabbitMQ) e blobstore.
Separá-los em dois repositórios exigiria extrair um terceiro módulo Go
(`ndovu-core`) e passar a versioná-lo: toda mudança de contrato interno vira
três PRs, um bump de versão e uma janela de skew entre API e Writer justamente
nos pontos onde eles **precisam** concordar (formato da mensagem no buffer,
schema do ClickHouse, migrações do Postgres).

O ganho de separar seria isolamento de deploy — e esse ganho já existe sem
separar o repositório: **um repositório, uma imagem, Deployments independentes**.
`api` e `writer` escalam, rolam e falham de forma independente no OpenShift
porque são objetos distintos, não porque vieram de repos distintos.

> Se a decisão organizacional for repos separados mesmo assim, o caminho é:
> `ndovu-core` (módulo Go publicado por tag semver) + `ndovu-api` + `ndovu-writer`
> consumindo-o. Custo estimado: +1 semana de setup e ~2 PRs extras por mudança
> de contrato interno. O restante deste plano continua válido — só muda a
> coluna "repositório" na tabela de workloads.

### O que sai do monorepo e vira outra coisa

| Hoje | Vira | Motivo |
|------|------|--------|
| `docker-compose.yml` | `ndovu-backend/dev/compose.yml` | Ambiente local do backend; usa imagens publicadas do dashboard |
| `infra/clickhouse/storage.xml` | `ndovu-platform/clickhouse/config/` | É configuração de infraestrutura, não de aplicação |
| `tools/seed/` | `ndovu-sdk/tools/seed/` | É um cliente do contrato v1, igual aos SDKs |
| `backend/api/openapi.yaml` | continua em `ndovu-backend` | Está `go:embed`ado e servido em `/openapi.yaml`; separar quebra o embed |
| `docs/features/*` | `ndovu-docs` (ou permanece no backend) | Documentação de produto, sem acoplamento de build |

## 3. Projects (namespaces) no OpenShift

Sete projects. Aplicação e dados separados por ambiente — quotas, RBAC e
NetworkPolicy independentes; o time de banco não precisa de acesso ao namespace
de aplicação e vice-versa.

| Project | Conteúdo | Rede |
|---------|----------|------|
| `ndovu-app-dev` | api, writer, dashboard | Routes públicas (interno), sai para `ndovu-data-dev` |
| `ndovu-app-hml` | idem | idem, ambiente de homologação |
| `ndovu-app-prd` | idem | Routes externas com TLS edge |
| `ndovu-data-dev` | clickhouse, broker de stream (nats/kafka/rabbitmq), postgres, bucket | Sem Route; só ClusterIP |
| `ndovu-data-hml` | idem | idem |
| `ndovu-data-prd` | idem, com réplicas/backup | idem, NetworkPolicy restrita a `ndovu-app-prd` |
| `ndovu-cicd` | Argo CD Applications, Tekton (se usado), ImageStreams compartilhadas | — |

Comunicação entre namespaces por FQDN de Service:
`clickhouse.ndovu-data-prd.svc.cluster.local:9000`,
`nats.ndovu-data-prd.svc.cluster.local:4222` (ou `kafka.…:9092` / `rabbitmq.…:5672`
conforme `NDOVU_STREAM_BACKEND`),
`postgres.ndovu-data-prd.svc.cluster.local:5432`.

### Workloads por project de aplicação

| Workload | Tipo | Repo | Réplicas | Exposição |
|----------|------|------|----------|-----------|
| `ndovu-api` | Deployment | `ndovu-backend` | HPA (ver CAPACITY) | Route `api.<domínio>` + Route `ingest.<domínio>` |
| `ndovu-writer` | Deployment | `ndovu-backend` | **1 (fixa)** — ver risco R1 | Nenhuma |
| `ndovu-dashboard` | Deployment | `ndovu-dashboard` | 2+ | Route `ndovu.<domínio>` |
| `ndovu-migrate` | Job (PreSync) | `ndovu-backend` | 1 por deploy | Nenhuma |

### Workloads por project de dados

| Workload | Tipo | Imagem recomendada no OpenShift |
|----------|------|--------------------------------|
| `clickhouse` | StatefulSet via **Altinity ClickHouse Operator** | `altinity/clickhouse-server` |
| `nats` | StatefulSet via **NATS Helm chart** (JetStream file store) | `nats:2.10-alpine` |
| `kafka` *(alternativa)* | Redpanda/Strimzi Operator — broker + tópicos | `redpanda`/Kafka — usado quando `NDOVU_STREAM_BACKEND=kafka` |
| `rabbitmq` *(alternativa)* | RabbitMQ Cluster Operator | `rabbitmq:3.13-management` — usado quando `NDOVU_STREAM_BACKEND=rabbitmq` |
| `postgres` | Cluster via **CloudNativePG** ou `rhel9/postgresql-16` | não usar `postgres:16-alpine` — ver risco R6 |
| bucket S3 | **ObjectBucketClaim** (ODF/NooBaa) ou S3 corporativo externo | substitui o MinIO do compose |
| SMTP | Relay corporativo (Service externo/ExternalName) | substitui o MailHog do compose |

## 4. Configuração: o padrão

Regra única: **a imagem é idêntica em dev, hml e prd; só o ConfigMap/Secret muda.**

Cada workload consome exatamente três fontes, nessa ordem:

1. `ndovu-common-config` — ConfigMap do namespace, com o que é comum a api e
   writer (endpoints do ClickHouse/do broker de stream/S3, log level, URL do dashboard).
2. `ndovu-<workload>-config` — ConfigMap específico do workload.
3. `ndovu-<domínio>-credentials` — Secrets por domínio (postgres, clickhouse,
   auth, s3, smtp), nunca um Secret gigante por aplicação.

A tabela completa — cada variável do `config.go`, em qual ConfigMap/Secret ela
mora, valor por ambiente e se é obrigatória — está em [`ENVS.md`](ENVS.md).

## 5. Riscos e dívidas que a migração expõe

Sete pontos que funcionam no `docker-compose` e **quebram ou degradam** no
OpenShift. Cada um tem mitigação de curto prazo (dá para subir hoje) e correção
definitiva (issue no backlog do `ndovu-backend`).

| # | Problema | Impacto no OpenShift | Curto prazo | Correção |
|---|----------|---------------------|-------------|----------|
| **R1** | O writer roda, além do consumidor, os loops de **alertas, anomalias, digest semanal e refresh de sampling** como goroutines (`cmd/writer/main.go`). | Escalar o writer para N réplicas dispara N vezes cada alerta e envia N digests. O writer fica preso em 1 réplica → gargalo de ingestão e ponto único de falha. | `replicas: 1` fixo, sem HPA, `maxUnavailable: 0` no rollout. | Extrair um terceiro binário `cmd/scheduler` (singleton, 1 réplica) e deixar `cmd/writer` puramente stateless e escalável. É a mudança de maior impacto no plano. |
| **R2** | O dashboard embute `NEXT_PUBLIC_NDOVU_API` **em build time** (ARG do Dockerfile). | A imagem fica presa a um ambiente; não dá para promover o mesmo digest de hml para prd. | Build por ambiente na pipeline, tag `sha-<env>`. | Config em runtime: servir `/config.js` ou ler de `window.__NDOVU_CONFIG__` injetado pelo server component. Aí a imagem vira promovível. |
| **R3** | Migrações do Postgres e `EnsureSchema` do ClickHouse rodam **no boot** de api e writer. | Réplicas competindo pelo mesmo `ALTER TABLE` no ClickHouse durante rollout. O Postgres está protegido por advisory lock; o ClickHouse **não**. | Job `ndovu-migrate` como PreSync hook do Argo, rodando antes de api/writer. | Flag `NDOVU_SKIP_MIGRATIONS=true` nos Deployments; schema aplicado só pelo Job. |
| **R4** | O writer **não expõe HTTP** — sem `/health`, sem `/metrics`. | Impossível ter readiness/liveness real e não há métricas de lag de consumo, o sinal mais importante da ingestão. | Liveness por `exec`/processo; alarme sobre `num_pending` do JetStream via nats-exporter (ou consumer lag no Kafka). | Subir um servidor HTTP mínimo no writer com `/health` e `/metrics`, reusando `platform.Metrics`. |
| **R5** | `/metrics` e `/docs` ficam no mesmo listener das rotas públicas, sem autenticação. | Expor a Route da API expõe métricas internas e o Swagger na internet. | Route com `path:` restrito a `/v1`; segunda Service só para scraping interno. | Listener administrativo em porta separada (`NDOVU_ADMIN_PORT`). |
| **R6** | Imagens `postgres:16-alpine`, `clickhouse-server` e `minio` assumem UID fixo. | A SCC `restricted-v2` atribui UID aleatório → os três falham no boot. | Usar operadores/imagens compatíveis (tabela na seção 3). | — (decisão de plataforma, não de código) |
| **R7** | `NDOVU_STREAM_MAX_AGE_HOURS` mantém horas de eventos brutos no buffer. | O PVC do broker de stream vira o maior disco do ambiente em produção (ver CAPACITY §4.2). | `12h` em prd + limite de bytes no stream/tópico. | Avaliar `MaxBytes` no `EnsureStream` (NATS) / `retention.bytes` (Kafka) além do `MaxAge`. |

Riscos menores, sem bloqueio: `NDOVU_CORS_ORIGINS=*` por default (fixar a lista
em prd); `NDOVU_ADMIN_PASSWORD` e `NDOVU_BOOTSTRAP_INGEST_KEY` de bootstrap
(gerar aleatório em prd e revogar a chave após criar as reais); o dashboard
standalone pode querer escrever em `.next/cache` (montar `emptyDir` se o
filesystem for read-only).

## 6. Ondas de migração

Cinco ondas. Cada uma termina em estado testável — nenhuma depende de a
seguinte estar pronta.

| Onda | Entrega | Critério de pronto |
|------|---------|--------------------|
| **1. Fundação** | Criar os 6 repositórios, mover o código com histórico (`git filter-repo`), CI verde em cada um, imagens publicadas no registry. | `ndovu-backend` e `ndovu-dashboard` buildam e publicam imagem por commit. |
| **2. Camada de dados** | `ndovu-platform`: ClickHouse, broker de stream (NATS default) e Postgres no `ndovu-data-dev`, bucket via OBC, backup configurado. | Conexão validada de um pod debug; `storage.xml` aplicado; snapshot de backup restaurado uma vez. |
| **3. Aplicação em dev** | ConfigMaps/Secrets do `ENVS.md`, Job de migração, Deployments de api/writer/dashboard, Routes. | Login no dashboard, `seed` gera sessões, trace aparece no explorador. |
| **4. GitOps + hml** | `ndovu-gitops` com overlays, Argo CD sincronizando dev e hml, Secrets via ESO/Sealed Secrets. | Merge no gitops → rollout automático; nenhum `oc apply` manual. |
| **5. Produção** | `ndovu-app-prd` + `ndovu-data-prd` no tier dimensionado, HPA, quotas, ServiceMonitor, alertas, DR testado. | Teste de carga no pico do tier passa; failover do ClickHouse exercitado. |

Correções R1 a R5 entram como issues do `ndovu-backend` durante as ondas 3 e 4 —
**R1 e R3 antes da onda 5**, os demais podem ir depois com a mitigação de curto
prazo em produção.
