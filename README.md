# 🐘 Ndovu — Frontend Journey Tracing

Plataforma de monitoramento de frontends: cada tela acessada, ação executada e
chamada HTTP feita pelos seus usuários vira um **trace** — com payloads de
request/response, tempo de resposta e detalhes de erro — amarrado a
`userId` + `sessionId`. A ferramenta visual mostra o **rastro completo de uma
sessão** para troubleshooting preciso.

> *Ndovu* é "elefante" em suaíli — porque elefante não esquece nada.

## Stack

```
Frontends ──contrato v1──▶ API (Go) ──▶ NATS JetStream ──▶ Writer (Go) ──▶ ClickHouse
                                                                              ▲
                              Dashboard (Next.js) ◀── API de consulta ────────┘
```

| Camada | Tecnologia | Porta | Papel |
|--------|-----------|-------|-------|
| API | Go 1.24 · chi · Clean Architecture | 8080 | valida o contrato e publica no stream (~1ms); consulta read-only autenticada |
| Stream | NATS JetStream | 4222 | buffer durável: dedup, replay, DLQ, absorve picos e quedas do banco |
| Writer | Go (binário separado) | — | consome em lote e insere em bulk no ClickHouse |
| Armazém | ClickHouse 24.8 | 9000/8123 | traces: colunar, partição por dia, TTL 90d, dedup por eventId |
| Control plane | PostgreSQL 16 | 5432 | usuários do backoffice (multiusuário, auto gerenciável) e chaves de API |
| Dashboard | Next.js 15 · TS · TanStack Query · Recharts | 3000 | login, visão geral, explorador, rastro, telas admin |

A ingestão **nunca espera o banco** e nada se perde se o ClickHouse cair — as
mensagens ficam no stream e o writer drena depois. Detalhes e garantias de
entrega: [`docs/ARQUITETURA.md`](docs/ARQUITETURA.md).

## Subindo tudo (Docker Compose)

```bash
cp .env.example .env      # opcional: ajuste chaves/origens
docker compose up --build
```

Schemas (ClickHouse e Postgres), stream JetStream, admin inicial, empresa
"Padrão" e chave de ingestão bootstrap são criados automaticamente no boot.

### Acesso local (dev)

Portas expostas pelo `docker-compose.yml`:

| Serviço | URL | Observação |
|---------|-----|------------|
| Dashboard | http://localhost:13000 | Next.js — interface web |
| API | http://localhost:18081 | Ingestão + consulta (Swagger em `/docs`) |
| ClickHouse (HTTP) | http://localhost:8123 | Console de debug |
| NATS | nats://localhost:4222 | JetStream |
| Postgres | localhost:55432 | Control plane |
| MailHog SMTP | localhost:11025 | SMTP fake para digest local |
| MailHog UI | http://localhost:18025 | Vê os e-mails recebidos |
| MinIO S3 API | localhost:19000 | Cold tier do ClickHouse (S3-compatible) |
| MinIO Console | http://localhost:19001 | UI do MinIO (user `ndovu` / senha `ndovu-secret`) |

**Retenção em camadas ativa** (Sprint F.1+F.2): o `trace_events` usa a policy
`tiered` definida em `infra/clickhouse/storage.xml`:

| Idade | Tier | Storage |
|-------|------|---------|
| 0–7 dias | hot | volume `default` (SSD local) |
| 7–30 dias | warm | volume `/warm` (HDD local separado) |
| 30–90 dias | cold | disk S3 (MinIO local) |
| 90+ dias | — | delete via TTL |

Migração é automática via `TTL ... TO VOLUME`. Para trocar por AWS S3 real em
produção: edite `infra/clickhouse/storage.xml` mudando `endpoint`,
`access_key_id`, `secret_access_key`. Nenhuma mudança de código.

**Digest semanal** (opcional): defina `NDOVU_DIGEST_RECIPIENTS` (comma-separated)
para ativar. Roda no writer aos domingos 20:00 UTC por default; ajustável via
`NDOVU_DIGEST_WEEKDAY`/`NDOVU_DIGEST_HOUR_UTC`. Testa imediatamente com:
```
curl -X POST http://localhost:18081/v1/admin/digest/send-now -H "Authorization: Bearer <token>"
```

O provider de e-mail é escolhido em modo `auto` por default: prefere **SendGrid**
se `NDOVU_SENDGRID_API_KEY` estiver setado, senão cai pra **SMTP** (MailHog
local no compose, ou seu relay em produção), senão vira **noop** (só loga).
Force um provider específico com `NDOVU_MAILER_PROVIDER=sendgrid|smtp|noop`.

| Env | Uso |
|-----|-----|
| `NDOVU_SENDGRID_API_KEY` | Chave da REST API do SendGrid — setou, priorizou. |
| `NDOVU_SMTP_HOST`/`_PORT`/`_USER`/`_PASSWORD` | Relay SMTP (SES, Postmark, MailHog…) |
| `NDOVU_SMTP_FROM` | Remetente — vale para os dois providers |
| `NDOVU_MAILER_PROVIDER` | `""` (auto), `sendgrid`, `smtp`, `noop` |

**Credenciais bootstrap do backoffice** (defina outros valores no `.env` e
troque a senha no primeiro acesso em produção):

```
E-mail: admin@ndovu.local
Senha:  admin12345
```

Variáveis de ambiente correspondentes (para override):
`NDOVU_ADMIN_EMAIL` e `NDOVU_ADMIN_PASSWORD`.

**Chave de ingestão bootstrap** (para SDKs de teste enviarem eventos):

```
X-Api-Key: dev-ingest-key
```

Override via `NDOVU_BOOTSTRAP_INGEST_KEY`. A chave só é criada quando ainda
não existe nenhuma chave cadastrada no control plane.

### Backoffice multiusuário

O acesso é autenticado (JWT) e auto gerenciável: em **Usuários**, admins criam
contas com papel `admin` (gerencia) ou `viewer` (só consulta), desativam
acessos e redefinem senhas — sempre existe ao menos um admin ativo. Em
**Chaves de API**, admins geram uma chave por app emissor (exibida em claro só
na criação; armazenada como hash) e revogam com efeito imediato na ingestão.
A verificação de token fica atrás de um port — plugar Keycloak/OIDC depois é
escrever um adapter, sem tocar no resto (ver `docs/ARQUITETURA.md`).

### Dados de exemplo

Com a stack no ar, simule sessões reais (login, 2ª via, fatura, PIX, erros):

```bash
node tools/seed/seed.mjs --sessions 40
# opções: --endpoint http://localhost:8080 --key dev-ingest-key
```

## Rodando em modo dev (sem Docker para os apps)

```bash
docker compose up clickhouse nats postgres   # só a infraestrutura
cd backend && go mod tidy
go run ./cmd/api                         # API em :8080
go run ./cmd/writer                      # writer (outro terminal)
cd dashboard && npm install && npm run dev   # dashboard em :3000
```

Defaults de env casam com o compose (`localhost:9000`, `nats://localhost:4222`,
usuário/senha `ndovu`) — ver `backend/internal/config/config.go`.

## Como um frontend se integra

Qualquer tecnologia (React, Angular, Flutter, TV…) envia lotes para
`POST /v1/events` com `X-Api-Key`, seguindo o **contrato v1** — veja
[`docs/INTEGRATION.md`](docs/INTEGRATION.md) (guia completo, passo a passo,
para desenvolvedores) e [`docs/CONTRACT.md`](docs/CONTRACT.md) (especificação
do contrato). Para web há um SDK de referência:

```js
import { createNdovu } from './sdk/ndovu-browser.js';

const ndovu = createNdovu({
  endpoint: 'http://localhost:18081',
  apiKey: 'dev-ingest-key',
  app: 'portal-cliente',
  getUserId: () => window.currentUserId ?? null,
});

ndovu.instrumentFetch();          // toda chamada fetch vira trace automaticamente
ndovu.pageView('/faturas');       // telas
ndovu.action('clicou_segunda_via', { feature: 'faturas' }); // ações de negócio
```

O SDK acumula eventos, envia em lote, mascara campos sensíveis
(`password`, `token`, `cvv`…) e reenvia com segurança em caso de falha de rede —
a idempotência é garantida fim-a-fim (dedup no stream e por `eventId` no armazém).

## A ferramenta visual

- **Visão geral** — eventos/sessões/usuários/erros na janela escolhida, série
  temporal, top rotas (com média e p95) e top erros. Filtrável por app e período.
- **Explorador** — todos os traces com filtros 100% combináveis: período, app,
  tipo, funcionalidade, usuário, rota, status HTTP, somente-erros e busca livre.
  Filtros vivem na URL (visões compartilháveis por link); cada linha expande
  para mostrar os payloads completos.
- **Sessões → rastro** — a timeline cronológica de tudo que o usuário fez em uma
  sessão, com telas, ações, chamadas, durações e erros em destaque.

A consulta é **read-only por construção**: a API não expõe nenhuma rota de
mutação de traces — dados são imutáveis, apenas inseridos e consultados.

## Estrutura

```
ndovu/
├── backend/
│   ├── cmd/api/               # API: ingestão (publica) + consulta
│   ├── cmd/writer/            # writer: stream → ClickHouse
│   ├── api/openapi.yaml       # OpenAPI escrito à mão (servido em /docs)
│   └── internal/              # domain, usecase, adapters (httpapi, natsstream, clickhouse)
├── dashboard/                 # Next.js — visão geral, explorador, rastro
├── sdk/                       # SDK browser de referência (contrato v1)
├── tools/seed/                # simulador de sessões para demo/testes
├── docs/                      # ARQUITETURA, EXECUTIVA, FUNCIONAL, TECNICA, USO, TESTING, INTEGRATION, CONTRACT + features/
└── docker-compose.yml         # clickhouse + nats + api + writer + dashboard
```

## Qualidade e testes

Plano completo em [`docs/TESTING.md`](docs/TESTING.md). Estado atual (Sprints
1–6 concluídos):

| Camada | Ferramenta | Cobertura / escopo |
|--------|-----------|-------|
| Domain Go | `go test` | 88% |
| Usecase Go | `go test` | 59% |
| HTTP handlers | `go test` + fakes | 63% |
| Adapters (Postgres/ClickHouse/MinIO) | `testcontainers-go` | 60–71% |
| Dashboard E2E | Playwright | 29 specs — auth, admin CRUD, RBAC, cross-tenant, N apps, triage, funnels, saved views, feedback, rotação de chaves |

```bash
# backend — unit + integration
cd backend && go test ./... && go vet ./...
# adapters de integração ficam atrás de build tag:
cd backend && go test -tags=integration ./...

# dashboard — typecheck + E2E
cd dashboard && npm run typecheck
cd dashboard && npx playwright test       # sobe stack via docker compose; ver docs/TESTING.md
```

## Documentação

- [`docs/EXECUTIVA.md`](docs/EXECUTIVA.md) — pitch e proposta de valor.
- [`docs/FUNCIONAL.md`](docs/FUNCIONAL.md) — o que cada tela faz.
- [`docs/ARQUITETURA.md`](docs/ARQUITETURA.md) — decisões e trade-offs.
- [`docs/TECNICA.md`](docs/TECNICA.md) — endpoints, SDKs, exemplos.
- [`docs/USO.md`](docs/USO.md) — guia de operação diária.
- [`docs/INTEGRATION.md`](docs/INTEGRATION.md) e [`docs/CONTRACT.md`](docs/CONTRACT.md) — instrumentar um app.
- [`docs/TESTING.md`](docs/TESTING.md) — plano de testes.
- [`docs/features/`](docs/features/) — **guia por funcionalidade** (o que
  serve, como usar, o que mostrar pro usuário). Comece por
  [`docs/features/README.md`](docs/features/README.md).

## Segurança (POC → produção)

- Ingestão autenticada por `X-Api-Key` gerida no control plane (uma chave por app, hash-only, revogação imediata); consulta e admin por JWT com roles.
- Mascaramento de PII é responsabilidade do SDK **antes** do envio; a API
  limita payload (1 MB) e valida o contrato.
- Produção: TLS em tudo, chave read-only para consulta atrás de gateway, rate
  limit por app, retenção via TTL — ver `docs/ARQUITETURA.md`.
