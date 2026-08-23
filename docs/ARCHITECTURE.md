# Ndovu — Arquitetura (sumário)

Ndovu ("elefante" em suaíli — porque elefante não esquece nada) é uma
plataforma de observabilidade de frontend: cada tela acessada, ação executada,
chamada HTTP, payload de request/response, tempo de resposta e erro, tudo
amarrado a `userId` + `sessionId` + `traceId`.

> **Este documento é um sumário.** Para a arquitetura completa (fluxos
> end-to-end, decisões e trade-offs de cada componente, escalabilidade, DR,
> segurança em profundidade, extensibilidade), leia
> [`ARQUITETURA.md`](ARQUITETURA.md).

---

## Visão geral

```
 ┌────────────┐  ┌────────────┐  ┌────────────┐
 │ Frontend A │  │ Frontend B │  │ Mobile ... │     qualquer tecnologia,
 │ (React)    │  │ (Vue)      │  │ (Flutter)  │     mesmo contrato JSON v1
 └─────┬──────┘  └─────┬──────┘  └─────┬──────┘
       │  POST /v1/events (batch, X-Api-Key)
       │  POST /v1/snapshots  (session replay MVP)
       │  POST /v1/feedbacks  (widget do usuário)
       ▼               ▼               ▼
 ┌─────────────────────────────────────────────┐
 │        Ndovu API (Go 1.25, :8080)           │  valida contrato, publica,
 │        ingestão = publicar e responder 202  │  responde em ~1ms — NUNCA
 └───────────────────┬─────────────────────────┘  espera o banco
                     ▼
        ┌────────────────────────┐
        │  NATS JetStream        │  buffer durável (file storage),
        │  stream NDOVU          │  dedup por Msg-Id (2min),
        └───────────┬────────────┘  retenção 48h, replay
                    ▼
        ┌────────────────────────┐  aplica sampling adaptativo,
        │  Ndovu Writer (Go)     │  bulk insert, ack só após persistir,
        │  + alertas + anomalia  │  avalia alertas/anomalias/digest
        │  + digest semanal      │  (single-instance por design)
        └───────────┬────────────┘
                    ▼
        ┌────────────────────────┐  hot (0-7d SSD) → warm (7-30d HDD) →
        │  ClickHouse            │  cold (30-90d S3) → delete (90d+)
        │  trace_events          │  dedup por eventId (ReplacingMergeTree)
        └───────────▲────────────┘  bloom filters, projeção by_session
                    │ GET /v1/* (read-only, Bearer JWT)
        ┌───────────┴────────────┐      ┌─────────────────────────┐
        │  Ndovu API (consulta)  │──────│  PostgreSQL             │
        └───────────┬────────────┘      │  control plane:         │
                    ▼                   │  users, companies, apps,│
          Dashboard Next.js 15 (:3000)  │  keys, alerts, anomaly, │
          login → RBAC + multi-tenant   │  sampling, source_maps, │
                                        │  audit_log, feedbacks…  │
                                        └─────────────────────────┘
                                                    │
                                        ┌───────────┴──────────┐
                                        │  MinIO / S3          │
                                        │  snapshots + cold    │
                                        │  tier do ClickHouse  │
                                        └──────────────────────┘
```

**Dois bancos, dois papéis.** ClickHouse guarda o que os usuários dos SEUS
frontends fizeram (massivo, imutável, analítico). Postgres guarda a
configuração da própria ferramenta (pequeno, transacional, mutável): contas,
empresas, apps, chaves, regras de alerta/anomalia/sampling, source maps,
audit log, feedbacks. **S3/MinIO** guarda blobs (snapshots + tier cold do
ClickHouse). Nenhum evento passa pelo Postgres.

Por que assim: a ingestão **não pode ser gargalo nem perder eventos**. A API
só valida e publica; o stream segura tudo que chegar (inclusive com o
ClickHouse fora do ar); o writer drena no ritmo do banco, em lotes grandes —
exatamente o formato de escrita em que o ClickHouse rende melhor.

### Garantias de entrega

| Ponto | Mecanismo |
|---|---|
| Retry da API/SDK | `Nats-Msg-Id` = hash do lote → JetStream descarta re-publicação idêntica (janela de 2min) |
| Crash do writer | ack só após insert; sem ack → reentrega (at-least-once) |
| Reentrega duplicada | `eventId` é chave de dedup no ClickHouse (ReplacingMergeTree) |
| Banco fora do ar | mensagens acumulam no stream (retenção 48h, configurável) e são drenadas depois |
| Reprocessamento | o stream permite replay: um novo consumer relê a janela inteira |
| S3 fora do ar | snapshots é fire-and-forget (não bloqueia evento); tier cold do ClickHouse degrada leitura de dados > 30d |

---

## Backend (Go 1.25)

Clean Architecture: o domínio não conhece HTTP, NATS nem SQL; os adapters
implementam ports declarados no domínio. Dois binários, um repositório:

```
backend/
├── cmd/api/               # composition root da API (ingestão + consulta + admin)
├── cmd/writer/            # composition root do writer (stream → ClickHouse + agendadores)
├── api/openapi.yaml       # contrato OpenAPI 3.0 (servido em /docs)
└── internal/
    ├── config/            # env vars com defaults sãos
    ├── domain/            # entidades, ports, erros — nunca importa adapter
    ├── usecase/           # 20+ services: Ingest, Query, Auth, App, Company,
    │                       # Issue, Alert, Anomaly, Sampling, SourceMap,
    │                       # Snapshot, Feedback, Digest, GDPR, Audit, Permission,
    │                       # Retention, Funnel, SavedView, Release, WriteService
    ├── adapter/
    │   ├── httpapi/       # router chi, 50+ handlers, middleware (bearerAuth,
    │   │                   # apiKeyAuth, tenantScope, requireRole, maxBody, cors)
    │   ├── natsstream/    # publisher + pull consumer JetStream (testado com embedded)
    │   ├── clickhouse/    # schema embedado, bulk insert, 15+ queries analíticas
    │   ├── ctlpostgres/   # 15 migrations embedadas + repository completo
    │   └── blobstore/     # S3-compatible (MinIO local / AWS S3 prod)
    └── platform/          # logger (slog), mailer (factory sendgrid/smtp/noop), metrics
```

- A API **nunca escreve no banco de eventos** e o pacote de consulta **não tem
  nenhum método de mutação** — read-only por construção.
- Idempotência fim-a-fim: retry do SDK → dedup no stream; reentrega do stream
  → dedup por `eventId` no engine.
- Trocar NATS por Kafka = escrever outro adapter de `EventStream`; domínio e
  usecases não mudam. Trocar SendGrid por SES = novo adapter de `Mailer`.

---

## Modelo de dados

### ClickHouse — `trace_events`

Uma única tabela desnormalizada, wide, ordenada para as queries mais comuns:

- **Engine** `ReplacingMergeTree(id)` — deduplica por `id` (eventId) em merges.
- **Ordenação** `(app, occurred_at, session_id, id)` — filtros por app + janela
  temporal em O(log N).
- **Partição** por dia (`toDate(occurred_at)`) — retenção por drop de
  partição é O(1) no metadata.
- **Projeção `by_session`** — ordena por `(session_id, occurred_at)`. Timeline
  de sessão em query direta, sem varrer a janela.
- **Skipping indexes**: bloom filter em `session_id`, `user_id`, `error_code`,
  `trace_id`; tokenbf_v1 em `http_url` (busca por substring).
- **Compressão**: ZSTD nos payloads JSON (request/response/error body,
  metadata, session_attrs).
- **Colunas de correlação**: `release`, `trace_id`, `span_id`, `parent_span_id`
  (adicionadas nas Fases 2-3 para release tracking e OpenTelemetry).

### PostgreSQL — control plane

15 migrações versionadas, executadas em startup. Tabelas principais:

| Tabela | Papel |
|---|---|
| `users` | Autenticação. Bcrypt. Roles admin/viewer + flag is_super. |
| `companies` | Multi-tenant (Fase 3). Toda user + app pertence a uma company. |
| `apps` | Emissores de eventos. FK company. |
| `api_keys` | Chaves de ingestão. Bcrypt. FK app. |
| `issue_states` | Triagem de erros por fingerprint. Assignee, status. |
| `issue_comments` | Discussão de triagem. |
| `alert_rules` + `alert_deliveries` | Regras threshold + histórico. |
| `anomaly_rules` + `anomaly_detections` | Regras z-score sazonal + histórico (Fase 4). |
| `sampling_rules` | Descarte adaptativo por app+tipo (Fase 3). |
| `source_maps` | Metadata + blob de .map para desminificação (Fase 2). |
| `session_snapshots` | Metadata de snapshots (blob em S3) (Fase 3). |
| `saved_views` | Presets de filtro por usuário. |
| `funnels` | Definições de funil (steps em JSON). |
| `user_app_permissions` | RBAC granular: viewer → apps específicos (Fase 3). |
| `audit_log` | Insert-only. Todas as ações admin. |
| `user_feedbacks` | Widget de feedback do SDK (Fase 4). |

### S3 / MinIO

- **Bucket `ndovu-snapshots`**: HTML gzip dos snapshots. Key:
  `snapshots/{app}/{yyyy-mm-dd}/{eventId}.html.gz`.
- **Bucket `ndovu-cold`**: usado pelo `storage_policy` do ClickHouse como
  tier cold para partes > 30 dias.

---

## Dashboard (Next.js 15)

- App Router com 22 rotas.
- React 19 + TanStack Query + Recharts + Tailwind.
- Consome exclusivamente a API (JWT em header).
- Múltiplas réplicas suportadas (stateless).
- Todas as telas com filtros mantêm estado na querystring — links são
  compartilháveis.

### Módulos principais

- **Consulta:** Overview, Issues, Releases, Performance (Web Vitals), Funis,
  Retenção, Explorador de eventos, Sessões, Session replay (via issues).
- **Admin:** Empresas, Apps, Usuários (+ RBAC granular), Chaves, Alertas,
  Anomalias, Sampling, Source Maps, Audit Log, LGPD (export/forget),
  Feedback.

---

## Control plane: autenticação e gestão

### Papéis

- **`admin`**: gerencia usuários, empresas, apps, chaves, regras, source maps,
  LGPD, feedbacks. Vê tudo da sua company.
- **`viewer`**: leitura + comentários em issues + saved views. Pode ter
  permissões granulares por app (RBAC granular, Fase 3).
- **`is_super`** (flag em user): transita entre companies. Usado tipicamente
  para operação centralizada da plataforma.

O sistema impede remover/rebaixar o último admin ativo por company.

### Bootstrap

No primeiro boot, se não existe nenhum usuário, a API cria o admin de
`NDOVU_ADMIN_EMAIL`/`NDOVU_ADMIN_PASSWORD` com `is_super=true`; se não
existe nenhuma chave, cria a de `NDOVU_BOOTSTRAP_INGEST_KEY`. Depois disso,
tudo é gerido pela própria ferramenta.

### Sessão

Login e-mail/senha (bcrypt) → JWT HS256 com TTL de 8h. Todas as rotas de
consulta exigem `Authorization: Bearer`; as de admin exigem role `admin`.

### Chaves de ingestão

Formato `ndk_<32 chars>`. Armazenadas bcrypt (nunca em claro depois da
criação). Validação na ingestão usa cache em memória (TTL 30s) — a rota
quente não paga uma ida ao Postgres por lote. Revogação DELETE tira do
banco; efeito em até 30s (TTL do cache).

### Multi-tenancy (Fase 3)

- **Company** é o tenant. Isolamento **lógico** (mesma instância, filtro
  por app em todas queries via middleware `tenantScope`).
- Middleware monta `AppScope{IsSuper, Apps[]}` no início de cada request.
  Todo `QueryService` exige o scope — não é possível "esquecer".
- **Cross-tenant guard** na ingestão: envelope.app deve bater com key.app.
  Impede envio como "outra app" mesmo com chave válida.

### RBAC granular (Fase 3)

Um viewer pode ter permissões explícitas para apps específicos via
`user_app_permissions`. Se tem N linhas, seu `AppScope` fica restrito
àquelas N apps. Se não tem nenhuma, vê todas as apps da company.

---

## Recursos avançados (Fases 2-4)

### Session replay MVP (Fase 3)

- Snapshot único do DOM no momento em que `sdk.error()` é chamado.
- SDK sanitiza: remove scripts/iframes, mascara inputs sensíveis,
  mascara elementos `data-ndovu-mask`.
- HTML é gzipped e enviado para `/v1/snapshots`.
- Metadata em Postgres (`session_snapshots`), blob em S3.
- Dashboard renderiza em `<iframe sandbox>` (defesa em profundidade).
- **Roadmap:** evolução para gravação contínua (rrweb).

### Sampling adaptativo (Fase 3)

- Regras por app + event_type com precedência
  (`app+type > app > type > global`).
- `keep_errors: true` preserva 100% dos erros mesmo com sample_rate baixo.
- Aplicado no **writer** (não na API) — se cache falha, aceita 100%
  (failure-open).
- Cache refresh a cada 30s.

### Detecção de anomalia (Fase 4)

- **Baseline sazonal**: compara janela atual com mesma hora + dia da semana
  nas últimas N semanas.
- Z-score: se `|z| > sensitivity`, dispara.
- Métricas: `error_count`, `event_count`, `error_rate`.
- Direção: `above` (spike), `below` (silêncio suspeito), `both`.
- Silêncio pós-disparo para não spamar.
- Roda no writer, a cada 5min (configurável).

### Correlação frontend↔backend (Fase 2)

- SDK gera W3C `traceparent` a cada `fetch()` interceptado.
- Header outgoing propaga automaticamente.
- Evento `http_request` grava `trace_id` + `span_id` + `parent_span_id`.
- Se backend também instrumentado com OpenTelemetry (Jaeger, Tempo,
  Honeycomb), o mesmo trace_id aparece nos dois lados — correlação livre.

### Source maps (Fase 2)

- Upload de `.map` associado a `app + release + filename`.
- API resolve automaticamente stack traces minificados quando o evento é
  consultado (`GET /v1/events/{id}/resolved-stack`).

### Alertas (Fase 1)

- Regras threshold: "N erros em M segundos".
- Canais: Slack (incoming webhook) e webhook genérico.
- Silêncio configurável.
- Roda no writer, a cada 60s.

### Digest semanal (Fase 2)

- Email automático com top erros, novos erros, Web Vitals, anomalias,
  feedbacks negativos.
- Configurável por dia da semana + hora UTC.
- Mailer factory: SendGrid → SMTP → noop (auto-detect por env).

### Retenção em camadas (Fase 3)

- Hot (0-7d, SSD local), warm (7-30d, HDD), cold (30-90d, S3), delete (90d+).
- Definido em TTL na tabela ClickHouse + `storage_policy`.
- Dados frios ficam em S3, transparente para queries.

### Audit log + LGPD (Fase 3)

- **Audit log**: insert-only, captura todas ações admin (login, criar/apagar
  chave, alterar permissões, exportar/apagar dados de titular).
- **LGPD**: endpoints `GDPR /user/{id}/export` (retorna JSON com tudo do
  titular) e `DELETE /user/{id}` (`ALTER DELETE` no ClickHouse).

### Widget de feedback (Fase 4)

- Método `sdk.mountFeedbackWidget()` cria botão flutuante + modal.
- Envia `POST /v1/feedbacks` com sessionId + lastEventId + viewport.
- Admin trata em `/admin/feedbacks` (triagem: novo → em triagem → resolvido).

---

## Números de referência

ClickHouse ingere >100k linhas/s por nó com inserts em lote e agrega bilhões
de linhas em sub-segundo — é o motor de PostHog, SigNoz e Sentry.
JetStream processa milhões de msgs/s em core NATS e centenas de milhares/s
persistidas. Para o volume de qualquer frontend corporativo, nenhum dos dois
é o gargalo.

### Dimensionamento típico

| Volume | API | Writer | Postgres | ClickHouse |
|---|---|---|---|---|
| 1M eventos/mês | 1 réplica, 1 vCPU | 1 vCPU | shared | 2 vCPU, 4GB |
| 10M eventos/mês | 2 réplicas | 2 vCPU, 4GB | 1 vCPU, 2GB | 4 vCPU, 8GB, 100GB SSD |
| 100M eventos/mês | 4 réplicas | 4 vCPU, 8GB | 2 vCPU, 4GB | 8 vCPU, 32GB, 500GB SSD |
| 1B eventos/mês | 8-16 réplicas | 8 vCPU, 16GB | 4 vCPU, 8GB | Cluster CH 3+ nós |

---

## Evolução

| Preocupação | Caminho |
|---|---|
| Escala de ingestão | réplicas da API (stateless); cluster JetStream com RAFT |
| Escala de leitura | ReplicatedMergeTree + ClickHouse Keeper; sharding por app |
| Kafka como padrão da empresa | novo adapter de `EventStream`; nada mais muda |
| SSO corporativo (Keycloak/OIDC) | adapter de `TokenVerifier`; ver roadmap Fase 5 |
| Session replay contínuo | evolução do MVP snapshot para rrweb (roadmap) |
| BI corporativo | Query API pública (SQL-as-a-service) — roadmap |
| Data warehouse | Export para S3/Parquet / streaming BigQuery — roadmap |
| Kubernetes | Helm chart — roadmap |

---

## Segurança

- **Ingestão**: `X-Api-Key` por app, bcrypt no banco, revogação em até 30s.
- **Consulta/admin**: JWT HS256 (segredo em env, rotação recomendada 90d).
- **Cross-tenant guard**: envelope.app == key.app enforced no middleware.
- **PII redaction**: SDK redige `password|token|cvv|card|senha` antes de sair
  do cliente. Snapshots mascaram inputs sensíveis e `data-ndovu-mask`.
- **Rate limit**: por chave (50 req/s default).
- **Payload limit**: 1 MB default.
- **Audit log**: insert-only, todas ações admin rastreadas.
- **LGPD**: export + esquecimento built-in.
- **Produção**: TLS em tudo, reverse proxy (Caddy/Nginx/Traefik), secrets em
  vault.

---

## Referências

- **Arquitetura completa (fluxos, decisões, trade-offs, DR):**
  [`ARQUITETURA.md`](ARQUITETURA.md)
- **Contrato de ingestão:** [`CONTRACT.md`](CONTRACT.md)
- **Integração passo-a-passo por tecnologia:**
  [`INTEGRATION.md`](INTEGRATION.md)
- **Guia de uso (padrões idiomáticos por tecnologia):** [`USO.md`](USO.md)
- **Referência técnica completa (endpoints, SDKs, deploy):**
  [`TECNICA.md`](TECNICA.md)
- **Funcionalidades do produto:** [`FUNCIONAL.md`](FUNCIONAL.md)
- **Visão executiva:** [`EXECUTIVA.md`](EXECUTIVA.md)

---

Documento vivo. Última atualização: agosto/2026.
Reflete estado após a Fase 4 concluída (roadmap 12+ meses entregue).
