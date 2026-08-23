# Ndovu — Documentação de Arquitetura

> **Público-alvo:** arquitetos de software, tech leads, engenheiros
> sêniores, times de plataforma e SRE.
>
> **Objetivo:** documentar as decisões estruturais do produto — por que
> cada tecnologia foi escolhida, quais trade-offs foram aceitos, quais
> garantias o sistema oferece e quais são os pontos de extensão.
>
> Data desta versão: agosto/2026.
> O documento `ARCHITECTURE.md` (versão antiga) permanece como
> referência histórica; este substitui e amplia.

---

## Sumário

1. [Visão geral do sistema](#1-visão-geral-do-sistema)
2. [Princípios arquiteturais](#2-princípios-arquiteturais)
3. [Topologia de componentes](#3-topologia-de-componentes)
4. [Fluxo end-to-end de ingestão](#4-fluxo-end-to-end-de-ingestão)
5. [Fluxo end-to-end de consulta](#5-fluxo-end-to-end-de-consulta)
6. [Modelo de dados](#6-modelo-de-dados)
7. [Camadas do backend (Clean Architecture)](#7-camadas-do-backend-clean-architecture)
8. [Garantias de entrega e idempotência](#8-garantias-de-entrega-e-idempotência)
9. [Multi-tenancy e isolamento](#9-multi-tenancy-e-isolamento)
10. [Autenticação e autorização](#10-autenticação-e-autorização)
11. [Retenção em camadas e política de storage](#11-retenção-em-camadas-e-política-de-storage)
12. [Sampling adaptativo](#12-sampling-adaptativo)
13. [Detecção de anomalia](#13-detecção-de-anomalia)
14. [Session replay MVP](#14-session-replay-mvp)
15. [Correlação frontend ↔ backend (OpenTelemetry)](#15-correlação-frontend--backend-opentelemetry)
16. [Mailer factory e integração com terceiros](#16-mailer-factory-e-integração-com-terceiros)
17. [Escalabilidade horizontal](#17-escalabilidade-horizontal)
18. [Observabilidade do próprio ndovu](#18-observabilidade-do-próprio-ndovu)
19. [Desastres e recuperação](#19-desastres-e-recuperação)
20. [Segurança em profundidade](#20-segurança-em-profundidade)
21. [Extensibilidade e pontos de troca](#21-extensibilidade-e-pontos-de-troca)
22. [Trade-offs e decisões declaradas](#22-trade-offs-e-decisões-declaradas)

---

## 1. Visão geral do sistema

O ndovu é uma plataforma composta por três grandes componentes de
runtime, dois bancos de dados especializados, um buffer assíncrono e
um frontend:

```
                    ┌──────────────────────────────────────────────────┐
                    │                   Cliente (SDK)                  │
                    │   ndovu-browser · node · react-native · next     │
                    └──────────────────────┬───────────────────────────┘
                                           │ HTTPS + X-Api-Key
                                           ▼
                    ┌──────────────────────────────────────────────────┐
                    │                  ndovu-api (Go)                  │
                    │   ingest 202 · consulta · admin · auth · docs    │
                    └────┬────────────────┬──────────────────┬─────────┘
                         │                │                  │
                         │ publica        │ lê/escreve       │ lê
                         ▼                ▼                  ▼
                    ┌────────┐      ┌─────────────┐    ┌──────────────┐
                    │  NATS  │      │  Postgres   │    │  ClickHouse  │
                    │   JS   │      │   (control  │    │  (analítico  │
                    │ stream │      │    plane)   │    │   colunar)   │
                    └────┬───┘      └─────────────┘    └──────▲───────┘
                         │ pull, bulk                          │
                         ▼                                     │
                    ┌───────────────────────────────────────────┐
                    │             ndovu-writer (Go)             │
                    │  bulk insert · alertas · anomalia · digest │
                    └───────────────────────────────────────────┘

                             ┌──────────────────────────┐
                             │    ndovu-dashboard        │
                             │    (Next.js 15 / React 19)│
                             └──────────────────────────┘
                                        │
                                        │ HTTPS + JWT (Bearer)
                                        ▼
                                  (chama ndovu-api)
```

### 1.1. Componentes de runtime

| Componente | Linguagem | Papel |
|---|---|---|
| **ndovu-api** | Go 1.25 | HTTP server. Recebe ingestão, serve consultas, autentica, gerencia admin. Publica no NATS. **Stateless.** |
| **ndovu-writer** | Go 1.25 | Worker sem HTTP. Consome do NATS, faz bulk insert no ClickHouse, avalia alertas/anomalias/digest. **Stateful no offset do consumer.** |
| **ndovu-dashboard** | Next.js 15 + React 19 | SPA + SSR. Consome exclusivamente a API. |

### 1.2. Componentes de armazenamento

| Componente | Papel | Por que essa escolha |
|---|---|---|
| **ClickHouse** | Storage analítico (`trace_events`). Colunar, ZSTD, particionado por dia, TTL em camadas. | Volume alto (bilhões de eventos possíveis), queries agregadas, projeções, storage tiers nativos. |
| **PostgreSQL** | Control plane (users, apps, chaves, regras, saved views, audit log, feedbacks, etc). | ACID, migrações versionadas, JOINs simples, ideal para dados de configuração. |
| **NATS JetStream** | Buffer durável entre API e Writer. Retenção 48h. Dedup 2min por Nats-Msg-Id. | Simples de operar, at-least-once com ack, replay, embedded server para testes. |
| **MinIO / S3** | Storage de snapshots (HTML gzip) + tier cold do ClickHouse. | S3-compatible, comoditizado, evolui para AWS S3 real sem mudança de código. |

### 1.3. Serviços auxiliares

- **MailHog** (dev) / **SendGrid** ou **SMTP direto** (prod) — envio de
  emails do digest.
- **Prometheus scraper** (opcional) — coleta `/metrics` da API e writer.

---

## 2. Princípios arquiteturais

### 2.1. Nunca bloquear a ingestão

A API **sempre responde `202 Accepted` em ~1ms**. A publicação no NATS
é síncrona (para não perder o evento), mas a persistência final no
ClickHouse é assíncrona. Consequência: o cliente nunca é afetado por
lentidão do banco analítico.

### 2.2. Consulta é read-only por construção

O código de consulta (`QueryService`) não expõe nenhum método de
escrita. Não existe rota `PUT /v1/events` ou `DELETE /v1/events` para
o usuário. O único caminho de escrita é através da API de ingestão
(que publica no stream) e do writer (que consome do stream). Isso
garante que o dado histórico é imutável do ponto de vista da UI.

### 2.3. Dois bancos, dois modelos, dois SLAs

Separação estrita entre **dados operacionais** (config, users, chaves,
regras) e **dados analíticos** (eventos capturados). Cada um usa a
tecnologia mais adequada:

- Postgres: consistência forte, transações, JOINs, poucos gigabytes.
- ClickHouse: throughput de insert alto, queries agregadas em bilhões
  de linhas, colunar comprimido.

Nenhum código escreve em ambos ao mesmo tempo (evita 2-phase commit).

### 2.4. Clean Architecture

O backend segue a estrutura de camadas: `domain` (regras puras),
`usecase` (orquestração), `adapter` (I/O). Ver Seção 7.

### 2.5. Contrato antes de código

O contrato de ingestão v1 é público (`docs/CONTRACT.md`) e serve como
"lei" — qualquer cliente que envie o JSON está integrado. O SDK
JavaScript é apenas uma conveniência, não uma obrigatoriedade.

### 2.6. Failure-open onde faz sentido

- Se sampling não conseguir carregar regras (Postgres fora), o writer
  aceita 100% dos eventos (não bloqueia).
- Se digest não conseguir enviar (SMTP fora), loga erro e segue.
- Se snapshot falhar ao subir para S3, o evento principal ainda é
  gravado — snapshot é fire-and-forget.

### 2.7. Failure-closed onde é crítico

- Se API não conseguir publicar no NATS, retorna 5xx (não engole).
- Se ClickHouse cai, o writer para de ackar — mensagens ficam no
  stream até voltar (não perde dado).
- Autenticação nunca falha silenciosa: se JWT inválido, 401.

---

## 3. Topologia de componentes

### 3.1. Dependências de startup

```
Postgres ─────┐
              ├──► API ──► NATS ──► Writer ──► ClickHouse
Postgres ─────┘                                    │
                                                   └─► S3 (cold + snapshots)
```

- **API** só começa a servir após conectar em Postgres, ClickHouse e
  NATS. Roda migrações do Postgres em startup se necessário.
- **Writer** só começa a consumir após conectar em NATS, ClickHouse e
  (opcionalmente) Postgres.
- Se qualquer dependência não sobe no `docker-compose`, o serviço faz
  crash-loop com log claro do que falhou.

### 3.2. Deploy típico (Docker Compose)

Todos os serviços em containers, definidos em `docker-compose.yml`:

| Serviço | Imagem | Portas expostas | Volumes |
|---|---|---|---|
| postgres | postgres:16-alpine | 55432 | pgdata |
| clickhouse | clickhouse/clickhouse-server:24.8-alpine | 8123 (HTTP), 9000 (native) | chdata, chdata-warm, storage.xml |
| nats | nats:2.10-alpine | 4222 | natsdata |
| mailhog | mailhog/mailhog | 11025 (SMTP), 18025 (UI) | — |
| minio | minio/minio | 19000 (S3), 19001 (Console) | miniodata |
| minio-init | minio/mc | — | script |
| api | build local (backend/) | 18081 | — |
| writer | build local (backend/) | — | — |
| dashboard | build local (dashboard/) | 13000 | — |

### 3.3. Deploy escalável (produção)

Não incluído no compose, mas o design suporta:

- **API:** múltiplas réplicas atrás de load balancer L7 (stateless).
- **Writer:** **uma única réplica** para evitar duplicação de
  processamento de alertas/anomalias (o consumer NATS ackaria em
  paralelo, mas os schedulers rodariam N vezes). Escala vertical.
- **NATS:** cluster de 3+ nós com JetStream replicado.
- **ClickHouse:** replicação via ReplicatedMergeTree ou cluster nativo
  para alto volume.
- **Postgres:** primário + réplica read-only para dashboards intensivos
  (opcional; hoje não é gargalo).

---

## 4. Fluxo end-to-end de ingestão

Sequência para 1 evento capturado no browser:

```
SDK                    ndovu-api             NATS JS            ndovu-writer         ClickHouse
 │                        │                    │                    │                    │
 │ POST /v1/events        │                    │                    │                    │
 │ + X-Api-Key            │                    │                    │                    │
 ├───────────────────────▶│                    │                    │                    │
 │                        │ validate key       │                    │                    │
 │                        │ (cache 30s)        │                    │                    │
 │                        │ validate contract  │                    │                    │
 │                        │ enforce app==key   │                    │                    │
 │                        │ publish batch      │                    │                    │
 │                        │ + Nats-Msg-Id      │                    │                    │
 │                        ├───────────────────▶│                    │                    │
 │                        │◀── ack (síncrono)──┤                    │                    │
 │◀─── 202 Accepted ──────┤                    │                    │                    │
 │                        │                    │                    │                    │
 │                        │                    │ pull consumer      │                    │
 │                        │                    │ (batch 64 msgs)    │                    │
 │                        │                    ├───────────────────▶│                    │
 │                        │                    │                    │ apply sampling     │
 │                        │                    │                    │ bulk insert        │
 │                        │                    │                    ├───────────────────▶│
 │                        │                    │                    │◀── OK ─────────────┤
 │                        │                    │◀── ack ────────────┤                    │
```

### 4.1. Latência típica

- Cliente → 202: **~1-5ms** (dominado por rede + validate key).
- Evento no dashboard: **1-5s** (writer batch + NATS pull interval).

### 4.2. Backpressure

- Se ClickHouse fica lento, o writer não acka — o stream acumula.
- Stream retém 48h (configurável). Nesse período, se ClickHouse voltar,
  reprocessa tudo automaticamente.
- API continua respondendo 202 mesmo com stream cheio (até o limite do
  storage NATS).

### 4.3. Deduplicação

Ver Seção 8. Existem **duas camadas** de dedup:
1. NATS: `Nats-Msg-Id` (janela de 2min) evita reenvio duplo do mesmo
   batch pela API.
2. ClickHouse: `ReplacingMergeTree` deduplica por `id` do evento
   (garante idempotência mesmo se o writer reprocessar).

---

## 5. Fluxo end-to-end de consulta

Sequência para uma query do dashboard (ex.: listar eventos):

```
Browser                 Dashboard (Next.js)      ndovu-api              ClickHouse / Postgres
   │                          │                     │                          │
   │ user acessa /traces      │                     │                          │
   ├─────────────────────────▶│                     │                          │
   │                          │ GET /v1/events?...  │                          │
   │                          │ Bearer JWT          │                          │
   │                          ├────────────────────▶│                          │
   │                          │                     │ bearerAuth() → Identity  │
   │                          │                     │ tenantScope() → AppScope │
   │                          │                     │                          │
   │                          │                     │ query with SQL params    │
   │                          │                     ├─────────────────────────▶│ (CH)
   │                          │                     │◀── rows ─────────────────┤
   │                          │                     │                          │
   │                          │◀── JSON + Cursor ───┤                          │
   │◀── render ────────────────┤                     │                          │
```

### 5.1. Autenticação

- `bearerAuth()`: valida JWT (HS256), extrai `Identity` (userId, role,
  companyId, isSuper).
- `tenantScope()`: monta `AppScope` — para viewer normal, restringe às
  apps da company; se tem `user_app_permissions`, restringe àquelas apps.
- `requireRole("admin")`: exige role admin para rotas administrativas.

### 5.2. Paginação

Usa **keyset** (cursor com `occurred_at + id`), não offset — evita o
problema clássico de offset com "salto" de linhas em datasets grandes
e mantém performance constante independente da profundidade.

### 5.3. Filtros

`EventFilter` monta a query dinamicamente com parâmetros nomeados:

```go
type EventFilter struct {
    From, To     time.Time
    Apps         []string     // enforced pelo tenantScope
    Users        []string
    Sessions     []string
    Types        []string
    Features     []string
    Names        []string
    HTTPRoute    string
    StatusMin, StatusMax int
    OnlyErrors   bool
    Search       string       // full-text em body/message
    Cursor       *Cursor
    Limit        int
}
```

---

## 6. Modelo de dados

### 6.1. Modelo analítico — ClickHouse

**Tabela única: `trace_events`**

Cada evento capturado vira uma linha. Deliberadamente **wide table**
(muitos campos, alguns nullable) em vez de normalização — otimiza para
queries agregadas com `LowCardinality` e `bloom_filter`.

**Engine:** `ReplacingMergeTree(id)` — deduplica automaticamente por
`id` durante merges. Garante idempotência de reenvio.

**Particionamento:** `toDate(occurred_at)` — uma partição por dia.
Drops de partições antigas via TTL são operação O(1) no metadata.

**Ordenação (`ORDER BY`):** `(app, occurred_at, session_id, id)`.
Otimiza filtros mais comuns: por app + janela temporal.

**Projeção auxiliar `by_session`:** ordena por `(session_id, occurred_at)`.
Serve timeline de sessão em queries O(log N) mesmo com bilhões de
linhas.

**Colunas principais:**

| Campo | Tipo | Notas |
|---|---|---|
| id | UUID | PK dedup |
| session_id | String | Ring por sessão |
| user_id | String | Vazio se anônimo |
| app | LowCardinality(String) | 1 dicionário por app |
| event_type | LowCardinality(String) | page_view / action / http_request / error / custom |
| name, feature, screen | LowCardinality / String | Metadados de negócio |
| http_method, http_url | LowCardinality / String | Se http_request |
| http_status | Nullable(UInt16) | 200, 500, etc |
| duration_ms | Nullable(Int32) | Latência |
| request_body, response_body | String ZSTD(3) | Payloads (comprimidos) |
| error_code, error_message | String / String | Se error |
| error_body | String ZSTD(3) | Stack trace + contexto |
| metadata, session_attrs | String ZSTD(3) | JSON livre |
| user_agent | String ZSTD(1) | Reduzido |
| occurred_at, received_at | DateTime64(3, UTC) | Milissegundos |
| release | LowCardinality(String) | Versão do app |
| trace_id, span_id, parent_span_id | String | W3C traceparent |

**Índices secundários:**
- `bloom_filter` em session_id, user_id, error_code, trace_id — busca
  exata em 100M+ linhas.
- `tokenbf_v1` em http_url — busca por substring.

**TTL em camadas** (ver Seção 11).

### 6.2. Modelo de controle — Postgres

15 migrações versionadas em `backend/internal/adapter/ctlpostgres/migrations/`.
Cada uma tem `.up.sql` e `.down.sql`. Executadas automaticamente pela
API em startup (usando lib golang-migrate embedada).

Tabelas principais e responsabilidades:

| Tabela | Propósito |
|---|---|
| `users` | Autenticação. Bcrypt em `password_hash`. Roles `admin`/`viewer`, flag `is_super`. FK para `companies`. |
| `companies` | Multi-tenant. Toda user + app pertence a uma company. |
| `apps` | Emissores de eventos. FK company. |
| `api_keys` | Chaves de ingestão. Bcrypt em `key_hash`. FK app. |
| `issue_states` | Triagem de erros agrupados por fingerprint (PK). Assignee, status. |
| `issue_comments` | Comentários em issues. FK user + fingerprint. |
| `alert_rules` + `alert_deliveries` | Regras threshold e histórico de disparos. |
| `anomaly_rules` + `anomaly_detections` | Regras z-score e histórico. |
| `sampling_rules` | Regras de descarte por app + tipo. |
| `source_maps` | Metadata + blob do .map (para desminificação). |
| `session_snapshots` | Metadata do snapshot (blob em S3, referência aqui). |
| `saved_views` | Presets de filtro por usuário e tipo de visualização. |
| `funnels` | Definições de funil (steps em JSON). |
| `user_app_permissions` | RBAC granular: viewer → app + role. |
| `audit_log` | Insert-only. Todas as ações admin. |
| `user_feedbacks` | Widget de feedback do SDK. |

Todas PKs são UUIDs (exceto `issue_states.fingerprint` que é string
determinística — hash do erro).

FKs usam `ON DELETE CASCADE` ou `SET NULL` conforme faz sentido
semanticamente.

### 6.3. Modelo de storage de blobs — S3 / MinIO

Dois "buckets" lógicos:

- **`ndovu-snapshots`** — HTML gzip dos snapshots. Chave:
  `snapshots/{app}/{yyyy-mm-dd}/{eventId}.html.gz`.
- **`ndovu-cold`** — usado pelo storage_policy do ClickHouse como tier
  cold (parts com dados > 30 dias).

Nenhuma referência cruzada — o Postgres armazena `object_key` do
snapshot; o ClickHouse gerencia o tier cold internamente.

---

## 7. Camadas do backend (Clean Architecture)

### 7.1. Estrutura

```
backend/
├── cmd/
│   ├── api/main.go          # composition root da API (HTTP)
│   └── writer/main.go       # composition root do writer (worker)
└── internal/
    ├── config/              # env vars → struct
    ├── platform/            # logger, mailer, metrics (infra transversal)
    ├── domain/              # tipos + interfaces (ports) — PURO
    ├── usecase/             # lógica orquestrada — depende só de domain
    └── adapter/             # implementações — depende de bibliotecas externas
        ├── httpapi/         # rest handlers, middleware, router
        ├── clickhouse/      # EventWriter + EventReader
        ├── ctlpostgres/     # todos os stores do control plane
        ├── natsstream/      # publisher + pull consumer
        └── blobstore/       # S3 client para snapshots
```

### 7.2. Regra de dependência

```
adapter ──depends on──▶ usecase ──depends on──▶ domain
                             │
                             └──depends on──▶ platform (logger)
```

- `domain` **nunca** importa de `usecase`, `adapter` ou pacotes
  externos (exceto stdlib).
- `usecase` **nunca** importa de `adapter` — só usa as *ports* definidas
  em `domain`.
- `adapter` implementa ports do domain e depende de bibliotecas
  externas (chi, pgx, clickhouse-go, nats.go, minio-go).

### 7.3. Composition root

O único lugar onde adapters concretos são "amarrados" aos usecases é
em `cmd/api/main.go` (para a API) e `cmd/writer/main.go` (para o
writer). Isso mantém o restante do código desacoplado.

Exemplo simplificado:

```go
// cmd/api/main.go
ctl := ctlpostgres.Connect(ctx, cfg.PostgresURL, logger)
repo := clickhouse.Connect(ctx, cfg.ClickHouse...)
publisher := natsstream.NewPublisher(js)

ingestSvc := usecase.NewIngestService(publisher, keyCache, logger)
querySvc := usecase.NewQueryService(repo, ctl, logger)
// ... 20+ services

handlers := httpapi.NewHandlers(ingestSvc, querySvc, /* ... */, logger)
router := httpapi.NewRouter(handlers, authMiddleware, ...)
http.ListenAndServe(cfg.Port, router)
```

### 7.4. Testabilidade

O padrão port-adapter torna trivial escrever testes de usecase com
fakes:

```go
// usecase/anomaly_test.go
store := newFakeAnomalyStore(rule)
reader := &fakeReader{values: []float64{100, 10, 10, 10, 10}}
svc := &AnomalyService{store: store, reader: reader, ...}
svc.evaluate(ctx, rule)
```

Sem docker, sem Postgres, sem ClickHouse. Rodam em milissegundos.

---

## 8. Garantias de entrega e idempotência

### 8.1. Fim-a-fim: at-least-once

O sistema garante que **cada evento aceito pela API é entregue ao menos
uma vez ao ClickHouse**. Combinado com dedup, o resultado efetivo é
**exactly-once do ponto de vista do consumidor** (dashboard nunca vê
duplicatas).

### 8.2. Ponto de falha por ponto de falha

| Falha | Comportamento |
|---|---|
| Cliente → API (rede) | Cliente faz retry com o mesmo `eventId`. Dedup ClickHouse resolve. |
| API → NATS | Se publish falha, API retorna 5xx. Cliente faz retry. |
| API cai após publish | Cliente já teve 202 e não repete. Evento está no NATS. |
| Writer → ClickHouse | Writer não acka. Mensagem volta ao stream (pull consumer). |
| Writer cai no meio de um batch | JetStream reentrega o batch inteiro. Dedup ClickHouse resolve. |
| ClickHouse cai | Writer para de ackar. Stream acumula por até 48h. Ao voltar, reprocessa. |
| Duplo publish do mesmo batch pela API (ex.: retry) | `Nats-Msg-Id` dedup na janela de 2min. |

### 8.3. `eventId` como chave global

- Gerado no cliente (UUID v4).
- Nunca é regenerado, mesmo em retry.
- ReplacingMergeTree do ClickHouse deduplica automaticamente durante
  merges.
- **Consequência:** cliente pode ter lógica ingênua de "envia, esqueci
  se chegou, envia de novo" — o sistema é tolerante.

### 8.4. `Nats-Msg-Id` como chave de batch

- Gerado pela API a cada publicação (hash de conteúdo + timestamp).
- JetStream deduplica dentro de janela configurável (2min default).
- Protege contra duplo publish acidental (ex.: retry de API para NATS
  se a resposta se perdeu).

### 8.5. Idempotência das operações admin

Endpoints administrativos usam PUT / PATCH sempre que possível, para
serem idempotentes por natureza. POST (criação) usa constraints únicos
(email, nome de app, etc) para detectar duplicata e retornar 409
Conflict.

---

## 9. Multi-tenancy e isolamento

### 9.1. Modelo

- **Company** é o tenant. Isolamento é lógico (mesma instância do
  produto, mesma tabela).
- Toda **user** pertence a uma company.
- Toda **app** pertence a uma company.
- Toda **api_key** pertence a uma app → logo, a uma company.
- Todo **evento** em ClickHouse tem `app` (LowCardinality) — o
  mapeamento app→company está no Postgres.

### 9.2. Enforcement em consulta

O middleware `tenantScope` monta um `AppScope` no início de cada
request:

```go
type AppScope struct {
    IsSuper bool
    Apps    []string    // lista efetiva de apps que o usuário pode ver
}
```

- **super-admin (`is_super=true`)**: `Apps` vazio, mas `IsSuper=true`.
  Filtros aceitam tudo.
- **admin de company**: `Apps` = todas as apps ativas da company.
- **viewer com permissões explícitas**: `Apps` = interseção
  (apps_da_company, apps_concedidas_via_user_app_permissions).
- **viewer sem permissões explícitas**: `Apps` = todas apps da company.

Toda query no `EventReader` recebe o `AppScope` e injeta na cláusula
`WHERE app IN (...)`. Não é possível "esquecer" — o `QueryService`
sempre exige o scope.

### 9.3. Enforcement em ingestão

O middleware `apiKeyAuth` valida a chave e monta um `KeyContext`:

```go
type KeyContext struct {
    KeyID     string
    App       string    // app associada à chave
    CompanyID string
}
```

O handler `PostEvents` valida que **cada evento no batch tem
`envelope.app == KeyContext.App`**. Se não bate, retorna 400.

Isso é o **cross-tenant guard** — impede que uma chave de uma app envie
eventos "se passando" por outra.

### 9.4. Nenhum acesso cross-tenant

Não existe endpoint que retorne dados de outra company (exceto para
super-admin). O middleware roda **antes** de qualquer handler, então
mesmo bugs de código no handler não conseguem vazar dados.

---

## 10. Autenticação e autorização

### 10.1. Ingestão — chave estática

- Formato: prefixo `ndk_` + 32 chars aleatórios base64.
- Cliente envia em header `X-Api-Key`.
- Armazenada bcrypt no Postgres — nunca em texto claro após criação.
- Cache in-memory na API (30s TTL) para validação rápida.
- Revogação: DELETE remove do banco. Efeito em até 30s (TTL do cache).

**Trade-off:** cache de 30s permite chave revogada continuar
funcionando por até 30s. Aceito por performance. Emergências: reiniciar
a API (cache é in-memory).

### 10.2. Consulta e admin — JWT HS256

- Login: POST `/v1/auth/login` com email + senha.
- Retorna JWT assinado com HS256, TTL default 8h.
- Cliente envia em `Authorization: Bearer <token>`.
- Payload do JWT: `sub` (userId), `role`, `companyId`, `isSuper`, `exp`.
- Segredo em env var `NDOVU_AUTH_SECRET` (obrigatório trocar em prod).

**Trade-off:** JWT stateless. Revogação de token não é imediata — o
token continua válido até `exp`. Aceito porque:
- TTL curto (8h).
- Alternativa (revogação com estado) exigiria store de tokens
  invalidados, aumentando complexidade.
- Mudança de senha invalida sessões via troca de `password_version`
  (não implementado hoje — só desativa o usuário).

### 10.3. Bootstrap de admin

Em primeira execução, a API cria um admin com credenciais definidas
via env vars:
- `NDOVU_ADMIN_EMAIL` (default `admin@ndovu.local`).
- `NDOVU_ADMIN_PASSWORD` (default `admin12345`).
- Flag `is_super=true`.

Também cria uma chave de ingestão inicial via `NDOVU_BOOTSTRAP_INGEST_KEY`
(default `dev-ingest-key`). Ambos devem ser trocados em produção.

### 10.4. Salvaguarda do último admin

O `UserService.Update()` verifica antes de desativar ou downgrade de
role: `COUNT(*) FROM users WHERE role='admin' AND active=true > 1`.
Se não passar, rejeita com 409. Evita "tijolo administrativo".

### 10.5. RBAC granular (viewer scoped)

- Tabela `user_app_permissions` (userId, appId, role, grantedAt).
- Se um viewer tem N linhas nessa tabela, seu `AppScope` fica restrito
  às N apps.
- Se não tem nenhuma linha, comportamento padrão (todas apps da
  company).

**Trade-off:** permissão binária "vê tudo da company" vs "vê só as
concedidas". Não há middle-ground de "vê tudo *exceto* X". Simplicidade
sobre flexibilidade.

---

## 11. Retenção em camadas e política de storage

### 11.1. Objetivo

Reduzir custo de storage sem perder dado. Dados recentes precisam ser
rápidos (SSD); dados antigos podem estar em cold storage (S3).

### 11.2. Configuração ClickHouse

Arquivo `infra/clickhouse/storage.xml`:

```xml
<disks>
  <default>  <!-- SSD local do container -->
    <path>/var/lib/clickhouse/</path>
  </default>
  <warm>     <!-- HDD ou disco lento local -->
    <path>/warm/</path>
  </warm>
  <s3>       <!-- MinIO local / AWS S3 prod -->
    <type>s3</type>
    <endpoint>http://minio:9000</endpoint>
    <bucket>ndovu-cold</bucket>
  </s3>
</disks>
<policies>
  <tiered>
    <volumes>
      <default><disk>default</disk></default>
      <warm><disk>warm</disk></warm>
      <cold><disk>s3</disk></cold>
    </volumes>
  </tiered>
</policies>
```

### 11.3. TTL na tabela

```sql
CREATE TABLE trace_events (...)
ENGINE = ReplacingMergeTree(id)
PARTITION BY toDate(occurred_at)
ORDER BY (app, occurred_at, session_id, id)
TTL
  occurred_at + INTERVAL 7 DAY   TO VOLUME 'warm',
  occurred_at + INTERVAL 30 DAY  TO VOLUME 'cold',
  occurred_at + INTERVAL 90 DAY  DELETE
SETTINGS storage_policy = 'tiered';
```

### 11.4. Ciclo de vida

- **0-7 dias:** partes ficam em SSD (`default` = hot).
- **7-30 dias:** durante próximo merge após 7d, partes migram para HDD
  (`warm`).
- **30-90 dias:** durante próximo merge após 30d, partes migram para
  S3 (`cold`).
- **90+ dias:** removidas fisicamente.

**Observação:** as transições acontecem em merges (não são reescritas
imediatamente ao completar 7d). É "eventualmente consistente" na
posição do dado, mas o TTL é garantido.

### 11.5. Trade-offs

- **Ler cold é lento:** cada byte precisa ir buscar no S3. Aceito
  porque queries em dados > 30 dias são raras.
- **Custo de egress ao consultar cold:** se um dashboard puxa muito
  dado antigo, o S3 cobra egress. Mitigado com cache no ClickHouse.
- **Retornar dado antigo em queries agregadas:** transparente — o
  usuário não percebe onde o dado está.

### 11.6. Cuidados de migração

A migração do storage_policy foi feita em produção antes: o erro comum
é renomear o volume `default` para `hot`, mas o ClickHouse exige que
a política nova mantenha o volume `default` do anterior. Solução:
manter o primeiro volume nomeado `default`.

---

## 12. Sampling adaptativo

### 12.1. Motivação

Nem todo evento tem valor igual. Um `page_view` da homepage vale muito
menos que um `error` no checkout. Sampling permite descartar
percentualmente eventos de baixo valor mantendo 100% dos críticos.

### 12.2. Regras

Tabela `sampling_rules`:

| Campo | Descrição |
|---|---|
| app | Vazio = todas as apps |
| event_type | Vazio = todos os tipos |
| sample_rate | 0.0 a 1.0 — proporção que **fica** |
| keep_errors | Boolean — se true, `error` nunca é descartado |
| active | Boolean |

### 12.3. Precedência

Ao processar um evento, o writer procura regras nesta ordem:
1. `app=X, event_type=Y` (específica).
2. `app=X, event_type=""` (todos os tipos daquela app).
3. `app="", event_type=Y` (aquele tipo em todas apps).
4. `app="", event_type=""` (global).

Aplica a primeira que casa.

### 12.4. Onde acontece

No **writer**, não na API. Motivo: se fizesse na API, um bug no cache
de regras derrubaria a ingestão. No writer, se as regras não carregam,
o writer aceita 100% (failure-open) — perde otimização, mas não perde
dado.

### 12.5. Cache

Writer atualiza cache de regras a cada 30s (`NDOVU_SAMPLING_REFRESH_SECONDS`).
Trade-off aceito: mudanças em regras demoram até 30s para ter efeito.

---

## 13. Detecção de anomalia

### 13.1. Diferença para alertas por threshold

- **Threshold**: "mais de X em Y segundos".
- **Anomalia**: "desviou N desvios-padrão da média histórica".

Threshold é fácil de configurar mas gera falsos positivos em padrões
sazonais (segunda 10h sempre tem mais tráfego). Anomalia se adapta.

### 13.2. Modelo estatístico

**Baseline sazonal:** o sistema compara a janela atual com a mesma
janela (mesma hora do dia + mesmo dia da semana) nas últimas N
semanas.

Fórmula:

```
current  = métrica(agora - windowMinutes, agora)
baseline = [métrica(1w atrás), métrica(2w atrás), ..., métrica(Nw atrás)]
          (todas na mesma hora + dia da semana)
avg      = média(baseline)
stddev   = desvio_padrão(baseline)
z_score  = (current - avg) / stddev

if |z_score| > sensitivity:
    disparar
```

### 13.3. Métricas disponíveis

- `error_count`: contagem absoluta de eventos type=error.
- `event_count`: contagem total de eventos.
- `error_rate`: proporção (errors / total).

### 13.4. Silêncio pós-disparo

Após um disparo, a regra fica em silêncio por `silenceSeconds`. Evita
spam quando um pico persiste (5 alertas em 5min do mesmo problema).

### 13.5. Direção

- `above`: só dispara em spike (z_score positivo grande).
- `below`: só dispara em silêncio (z_score negativo grande) — útil
  para detectar quando algo que **deveria** estar acontecendo parou
  (ex.: ingestão travou).
- `both`: dispara em qualquer desvio.

### 13.6. Onde acontece

**Writer**, uma única réplica. Motivo: o writer é single-instance por
design; se rodasse na API (múltiplas réplicas), avaliaria N vezes e
enviaria N alertas.

### 13.7. Intervalo

Default: a cada 5min (`NDOVU_ANOMALY_INTERVAL_SECONDS=300`). Trade-off:
janela mínima de detecção. Não faz sentido detectar em segundos
(volatilidade estatística alta).

---

## 14. Session replay MVP

### 14.1. Escopo do MVP

Snapshot único do DOM no momento em que `sdk.error()` é chamado. Não é
gravação contínua (rrweb) — evolução prevista.

### 14.2. Fluxo

```
error() é chamado no SDK
    │
    ├─▶ envia evento normal (POST /v1/events)
    │
    └─▶ captura DOM (clone + sanitize + mask)
        gzip
        POST /v1/snapshots (multipart ou raw body)
                │
                ├─▶ salva metadata em Postgres (session_snapshots)
                └─▶ salva blob em S3 / MinIO (key: {app}/{yyyy-mm-dd}/{eventId}.html.gz)
```

### 14.3. Sanitização (feita no SDK, antes de sair do browser)

- Scripts (`<script>`) e iframes removidos.
- Inputs com `type=password|email|tel|cc-number|cc-csc` têm value
  mascarado.
- Elementos com atributo `data-ndovu-mask` têm texto substituído por
  `***`.
- Outros inputs mantêm value (senão o replay mostra input vazio).

### 14.4. Visualização

Dashboard usa `<iframe sandbox="">` para renderizar o HTML — proíbe
execução de scripts, formulários, popups. O HTML já vem sanitizado do
SDK, mas o sandbox é defesa em profundidade.

### 14.5. Retenção de snapshots

Independente do TTL do ClickHouse. Configuração no bucket S3
(lifecycle policy). Default: 30 dias.

### 14.6. Trade-offs

- **Snapshot no momento do erro é limitado:** você não vê os cliques
  anteriores, só a foto final. Breadcrumbs (eventos anteriores)
  complementam textualmente.
- **Tamanho:** DOM médio ~50-200 KB. Gzipped ~5-20 KB. Aceito.
- **Privacidade:** confia no dev marcar `data-ndovu-mask` em campos
  com PII fora de inputs. Se esquecer, PII pode ir junto.

---

## 15. Correlação frontend ↔ backend (OpenTelemetry)

### 15.1. Motivação

Um clique do usuário pode disparar uma chamada HTTP que passa por 3
microserviços no backend. Sem correlação, o log do backend não sabe
que aquela requisição veio daquele clique específico.

### 15.2. Solução W3C traceparent

O SDK, ao interceptar `fetch()`:

1. Gera `traceId` (16 bytes hex) e `spanId` (8 bytes hex).
2. Adiciona header `traceparent: 00-{traceId}-{spanId}-01` na
   requisição outgoing.
3. Grava o mesmo `traceId`+`spanId` no evento `http_request` que envia
   ao ndovu.

Se o backend já é instrumentado com OpenTelemetry (Jaeger, Tempo,
Honeycomb), automaticamente registra a mesma requisição com o mesmo
`traceId` — os traces frontend↔backend ficam correlacionados no
storage de traces do backend.

### 15.3. Visualização no dashboard

- Coluna `trace_id` disponível nos filtros.
- Rota `GET /v1/traces/{traceId}` retorna todos os eventos com aquele
  traceId — timeline W3C completa.
- Link do dashboard para o Jaeger/Tempo do backend (deep-link
  configurável).

### 15.4. Opt-out

Configuração `propagateTraceparent: false` no SDK desliga o
comportamento — para apps que não têm backend próprio instrumentado.

---

## 16. Mailer factory e integração com terceiros

### 16.1. Problema

Ambiente dev: MailHog (SMTP local, sem custo).
Ambiente prod: SendGrid ou SMTP corporativo.
Não queremos código diferente por ambiente.

### 16.2. Factory

`platform/mailer_factory.go` detecta automaticamente qual provider
usar, baseado em env vars:

```go
switch {
case cfg.MailerProvider == "sendgrid" && cfg.SendGridAPIKey != "":
    return &sendGridMailer{...}
case cfg.MailerProvider == "smtp" && cfg.SMTPHost != "":
    return &smtpMailer{...}
case cfg.MailerProvider == "auto":
    // tenta sendgrid, fallback smtp, fallback noop
case cfg.MailerProvider == "noop":
    return &noopMailer{}
default:
    // sem provider configurado — noop com log de aviso
}
```

### 16.3. Interface

```go
type Mailer interface {
    Send(ctx, from, to []string, subject, htmlBody string) error
}
```

Só um método. Simples de implementar em novos providers (SES, Postmark,
Mailgun). O código de usecase não sabe qual está usando.

### 16.4. Uso

- `DigestService` — email semanal.
- Futuro: notificações de alerta por email (hoje é Slack/webhook).
- Futuro: reset de senha, convite de novo usuário.

---

## 17. Escalabilidade horizontal

### 17.1. API

- Stateless.
- Cache de chaves in-memory (30s TTL) — cada réplica tem sua cópia.
- Escala **horizontalmente** atrás de load balancer L7.
- Rate limit por chave hoje é in-memory — sob múltiplas réplicas, é
  por réplica. Para rate limit distribuído seria necessário Redis.

### 17.2. Writer

- Consome via pull consumer NATS.
- **Deve rodar 1 réplica** para evitar duplicação de:
  - Avaliação de alertas.
  - Avaliação de anomalias.
  - Envio de digest.
- Escala **verticalmente** (mais CPU/RAM para insert batch).
- Alternativa futura: separar "insert batch" (escalável horizontal)
  de "avaliadores periódicos" (single instance).

### 17.3. NATS JetStream

- Cluster de 3+ nós com replicação.
- Retention 48h por default (configurável via `NDOVU_STREAM_MAX_AGE_HOURS`).
- Se stream lotar, cliente começa a receber erro no publish — API
  retorna 5xx.

### 17.4. ClickHouse

- Escala **verticalmente** (mais RAM e SSD) até milhões de eventos/dia
  em uma única instância.
- Escala **horizontalmente** via `ReplicatedMergeTree` + Zookeeper (ou
  ClickHouse Keeper) para HA.
- Sharding por `cityHash64(app)` para volumes muito altos.

### 17.5. Postgres

- Baixo volume (dados de configuração).
- Uma instância é suficiente até ~10k tenants.
- Escala com read replica para dashboards intensivos (não é gargalo
  hoje).

### 17.6. Dashboard (Next.js)

- Renderiza server-side + client-side.
- Pode rodar múltiplas réplicas.
- Sem estado local — usa TanStack Query com cache in-memory por
  browser.

---

## 18. Observabilidade do próprio ndovu

### 18.1. Metrics

- API e writer expõem `/metrics` no formato Prometheus.
- Métricas coletadas:
  - Latência de handlers HTTP (histograma).
  - Taxa de erro por endpoint.
  - Taxa de publish no NATS.
  - Taxa de consume + ack no NATS.
  - Latência de insert no ClickHouse.
  - Lag do consumer (mensagens pendentes no stream).
  - Cache hit/miss em chaves de API.

### 18.2. Logs

- `platform/logger.go` usa `slog` estruturado.
- JSON em produção (parseável por Loki, ELK, Datadog).
- Texto colorido em desenvolvimento.
- Nível configurável (`NDOVU_LOG_LEVEL`).

### 18.3. Healthcheck

- `GET /health` — 200 OK simples. Usado pelo Docker Compose e Kubernetes
  liveness/readiness.

### 18.4. Traces (do próprio ndovu)

Ainda não implementado — o ndovu é a ferramenta de rastreamento, mas
não se rastreia com OpenTelemetry hoje. Item de roadmap.

---

## 19. Desastres e recuperação

### 19.1. Cenários e recuperação

| Cenário | Impacto | Recuperação |
|---|---|---|
| API cai | Ingestão para. Consultas param. | Restart. Zero perda (o NATS não estava esperando resposta). |
| Writer cai | Eventos acumulam no NATS. | Restart. NATS reentrega o último batch. Dedup evita duplicata. |
| ClickHouse cai | Writer para de persistir. Consultas falham. | Restart. Writer reprocessa do último ack. |
| NATS cai | API não consegue publicar. Retorna 5xx. Cliente faz retry. | Restart do NATS. Cliente ressubmete. |
| Postgres cai | Auth quebra. Admin quebra. Ingestão degradada (não valida key). | Restart. Dados de config preservados em volume. |
| MinIO / S3 cai | Snapshots falham. Cold tier do ClickHouse falha para dados > 30d. | Restart. Snapshots é fire-and-forget (não bloqueia evento). |
| Volume Postgres corrompido | Perda de config: users, chaves, regras. | Restore de backup diário. |
| Volume ClickHouse corrompido | Perda de eventos históricos. | Restore parcial: cold tier (S3) preservado. Hot/warm reingesta improvável (48h de NATS). |

### 19.2. Backup

- **Postgres:** dump diário automatizado (`pg_dump`) para S3. Retenção
  30 dias.
- **ClickHouse hot/warm:** snapshot semanal (`BACKUP DATABASE`) para S3.
- **ClickHouse cold:** já está em S3, versionado no bucket.
- **MinIO / S3 buckets:** replicação cross-region se necessário.

Backup e restore não estão no docker-compose — precisam de runbook
separado em produção.

### 19.3. RPO/RTO alvos

- **RPO** (perda máxima aceitável): 1h para dados analíticos, 24h para
  config.
- **RTO** (tempo até restauração): 30min para API/writer, 2h para
  restore total.

Não há teste formal de DR periódico — item de roadmap.

---

## 20. Segurança em profundidade

### 20.1. Camadas

1. **Rede:** TLS obrigatório em produção (via reverse proxy — Caddy,
   Nginx, Traefik). API atrás de firewall, sem porta pública direta.
2. **Autenticação:**
   - Ingestão: X-Api-Key (bcrypt).
   - Consulta/admin: JWT HS256.
3. **Autorização:**
   - Middleware `requireRole` para admin.
   - `tenantScope` para isolamento entre companies.
   - `cross-tenant guard` para ingestão (app == key.app).
4. **Input validation:**
   - `maxBody` (1 MB default) previne payloads gigantes.
   - Validação de contrato v1 no `IngestService`.
   - Sanitização de HTML nos snapshots.
5. **Storage:**
   - Senhas: bcrypt cost 10.
   - Chaves de API: bcrypt.
   - PII de payload: SDK redige `password|token|cvv|card` antes de
     enviar.
6. **Auditoria:** todas ações admin em `audit_log` insert-only.
7. **CORS:** configurável por origem.

### 20.2. Modelo de ameaça

**Ameaças cobertas:**
- Roubo de chave de ingestão → revogação em 30s.
- Envio de eventos "se passando" por outra app → cross-tenant guard.
- Visualização de dados de outra company → tenantScope.
- Brute force de login → não há rate limit específico em login (item
  de melhoria).
- Vazamento de PII → redaction no SDK antes de sair do cliente.
- Injeção SQL → parâmetros nomeados em todas queries (pgx e
  clickhouse-go).
- XSS no dashboard → React escapa por default; snapshots em iframe
  sandbox.

**Ameaças não cobertas hoje:**
- CSRF (dashboard usa Bearer JWT, não cookie — não é vetor).
- DoS de ingestão coordenado (rate limit é por chave, não por IP).
- Persistência de segredo (JWT secret) — se vazar, todos os tokens são
  invalidáveis; recomendável rotação periódica.
- Auditoria de leitura (quem viu qual sessão) — só auditamos escritas.

### 20.3. Rotação de segredos

- **API keys:** UI permite criar nova + revogar antiga (padrão de rotação).
- **JWT secret:** apenas via env var + restart. Não há rotação dinâmica.
- **Senhas de usuário:** UI permite trocar.
- **Credenciais de banco:** Docker Compose usa env vars, aceitáveis;
  em prod, usar secret manager (Vault, AWS Secrets Manager).

---

## 21. Extensibilidade e pontos de troca

### 21.1. Trocar SDK JavaScript por outra linguagem

O contrato v1 é a fronteira. Qualquer cliente que envie POST /v1/events
com o JSON esperado está integrado. SDKs oficiais são conveniência:
- `ndovu-browser.js` (v1.2.0)
- `ndovu-node.js` (v1.1.0)
- `ndovu-next.js` (auto-instrumentação Next.js)
- `ndovu-react-native.js`

Adicionar Flutter, iOS nativo, Android nativo, Python — apenas
implementar o contrato.

### 21.2. Trocar NATS por Kafka

`domain.EventStream` é uma interface. Implementar `KafkaPublisher` em
`adapter/kafka/` e trocar no composition root. Ninguém mais no código
precisa saber. Trade-off: perder o dedup nativo do JetStream — precisa
implementar em outro nível.

### 21.3. Trocar ClickHouse por outro OLAP

`domain.EventReader` e `domain.EventWriter` são interfaces. Implementar
`adapter/druid/` ou `adapter/timescale/`. Custo alto (queries são
específicas ao dialeto), mas o resto do código não muda.

### 21.4. Adicionar SSO / OIDC

`usecase.AuthService.Login()` retorna JWT. Adicionar handler `/v1/auth/oidc/callback`
que valida token do provider (Keycloak, Auth0, Google), cria/atualiza
usuário no Postgres, e retorna JWT nosso. Nenhuma outra mudança
necessária.

### 21.5. Adicionar canal de alerta (Teams, PagerDuty)

`AlertDispatcher` é factory por `channel`. Adicionar case no switch e
implementar o payload esperado. ~50 linhas de Go.

### 21.6. Adicionar métrica personalizada

Como estamos com dados brutos em ClickHouse, novas métricas são
apenas novas queries SQL — sem migração, sem ETL. Novo endpoint em
`QueryService`, novo componente no dashboard.

---

## 22. Trade-offs e decisões declaradas

### 22.1. "Aceitamos duplicidade transitória por simplicidade"

O sistema tolera cliente enviar o mesmo evento múltiplas vezes (por
retry, por bug). O ClickHouse deduplica por `id` no engine. Consequência:
no primeiro momento após inserção, uma query pode retornar duplicatas
até o merge acontecer. Aceito porque:
- É raro.
- Merges rodam continuamente.
- Alternativa (dedup síncrono no insert) é 10x mais cara.

### 22.2. "Aceitamos delay de até 30s em revogação de chave"

Cache in-memory de 30s TTL. Ver Seção 10. Não vamos para Redis só para
isso.

### 22.3. "Aceitamos que digest e alertas rodem só no writer"

Ver Seção 17. Simplifica coordenação. Preço: writer não escala
horizontalmente.

### 22.4. "Aceitamos que session replay seja snapshot, não vídeo"

Ver Seção 14. rrweb aumenta payload em 10-100x e complexidade de
storage. Snapshot cobre o principal caso de uso (o que estava
acontecendo *no momento do erro*).

### 22.5. "Aceitamos que multi-tenancy seja lógica, não física"

Uma company é apenas uma FK no Postgres + filtro em ClickHouse. Não há
banco por tenant. Trade-off:
- **Prós:** operação simples, backups únicos, custo baixo.
- **Contras:** blast radius de um bug de tenantScope é alto. Mitigado
  com middleware forte e testes.

### 22.6. "Aceitamos que a UI seja SPA, não SSR-first"

Next.js 15 com App Router — SSR onde ajuda (login page), CSR onde
performa (páginas com muito fetch). TanStack Query gerencia cache.
Trade-off: primeiro carregamento em cold-start é mais lento; navegação
subsequente é rápida.

### 22.7. "Aceitamos que Postgres seja fonte da verdade de config e
ClickHouse de eventos"

Não sincronizamos. Não há evento no ClickHouse que "referencie" um app
por FK — só string. Se um app for renomeado no Postgres, eventos
antigos continuam com o nome antigo. Aceito porque:
- Renomeação de app é rara.
- Eventos antigos com nome antigo ainda são válidos analiticamente.

### 22.8. "Aceitamos que retenção seja hard-coded em 3 tiers"

TTL definido no schema.sql. Para mudar (ex.: manter 180 dias), requer
`ALTER TABLE ... MODIFY TTL`. Não é configurável por app hoje. Item de
roadmap.

### 22.9. "Aceitamos que o widget de feedback seja código, não plugin"

O dev precisa chamar `sdk.mountFeedbackWidget()`. Alternativa (script
tag drop-in) seria mais fácil mas exige mais infra (CDN, script
externo carregando SDK). Aceito porque a maioria dos apps já tem
build pipeline.

---

## Apêndices

### A.1. Diagrama de deploy (produção sugerida)

```
                   Internet
                       │
                   [ Cloudflare / CDN ]
                       │
                 ┌─────┴─────┐
                 │  Reverse   │
                 │   Proxy    │
                 │  (Caddy)   │
                 └──┬─────┬───┘
                    │     │
        ┌───────────┘     └───────────┐
        │                             │
   ┌────▼────┐                   ┌────▼────┐
   │  API #1 │                   │Dashboard│
   │  API #2 │                   │ Next.js │
   │  API #N │                   │  #1..N  │
   └────┬────┘                   └─────────┘
        │
        │
   ┌────▼─────┐          ┌──────────┐
   │   NATS   │          │ Postgres │
   │ cluster  │          │  + repl  │
   └────┬─────┘          └────┬─────┘
        │                     │
   ┌────▼─────┐               │
   │  Writer  │───────────────┤
   │ (single) │               │
   └────┬─────┘               │
        │                ┌────▼──────┐
        │                │  ClickHouse│
        └───────────────▶│  cluster   │
                         └─────┬──────┘
                               │
                          ┌────▼────┐
                          │   S3    │
                          │(cold+   │
                          │snapshots)│
                          └─────────┘
```

### A.2. Estimativa de dimensionamento

| Volume | API | Writer | Postgres | ClickHouse |
|---|---|---|---|---|
| 1 M eventos/mês | 1 réplica, 1 vCPU | 1 vCPU | shared | 2 vCPU, 4 GB RAM |
| 10 M eventos/mês | 2 réplicas, 2 vCPU | 2 vCPU, 4 GB | 1 vCPU, 2 GB | 4 vCPU, 8 GB RAM, 100 GB SSD |
| 100 M eventos/mês | 4 réplicas, 4 vCPU | 4 vCPU, 8 GB | 2 vCPU, 4 GB | 8 vCPU, 32 GB RAM, 500 GB SSD |
| 1 B eventos/mês | 8-16 réplicas | 8 vCPU, 16 GB | 4 vCPU, 8 GB | Cluster ClickHouse 3+ nós |

### A.3. Documentação complementar

- **[EXECUTIVA.md](EXECUTIVA.md)** — visão de valor.
- **[FUNCIONAL.md](FUNCIONAL.md)** — o que cada tela faz.
- **[TECNICA.md](TECNICA.md)** — endpoints, SDKs, exemplos de código.
- **[CONTRACT.md](CONTRACT.md)** — spec do contrato de ingestão v1.
- **[INTEGRATION.md](INTEGRATION.md)** — walkthrough de integração.
- **[ARCHITECTURE.md](ARCHITECTURE.md)** — versão anterior (referência
  histórica).

### A.4. Estatísticas do código

- **Backend Go:** ~15.000 linhas
  - domain: ~1.5k
  - usecase: ~4k
  - adapters: ~8k
  - platform: ~0.5k
- **Dashboard Next.js:** ~8.000 linhas
- **SDKs:** ~1.000 linhas combinadas
- **Migrations Postgres:** 15 arquivos .up.sql
- **Schema ClickHouse:** 1 arquivo (~90 linhas)
- **Testes:** ~1.500 linhas (fingerprint, ingest, auth, anomaly,
  sampling, stream, handlers, mailer factory)

---

Documento vivo. Última atualização: agosto/2026.
Reflete o estado da Fase 4 concluída.
