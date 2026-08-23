# Ndovu — Documentação Técnica

> **Público-alvo:** desenvolvedores de aplicação (front-end e back-end),
> engenheiros de plataforma, SRE, DevOps.
>
> **Objetivo:** manual operacional completo. Como integrar o SDK, como
> chamar cada endpoint, como configurar o ambiente, como diagnosticar
> problemas, como estender.
>
> Data desta versão: agosto/2026.
> Cobre 100% da API e SDKs após a Fase 4.

---

## Sumário

1. [Setup do ambiente de desenvolvimento](#1-setup-do-ambiente-de-desenvolvimento)
2. [Variáveis de ambiente](#2-variáveis-de-ambiente)
3. [Contrato de ingestão v1](#3-contrato-de-ingestão-v1)
4. [SDK Browser (JavaScript)](#4-sdk-browser-javascript)
5. [SDK Next.js](#5-sdk-nextjs)
6. [SDK Node.js](#6-sdk-nodejs)
7. [SDK React Native](#7-sdk-react-native)
8. [Referência da API HTTP](#8-referência-da-api-http)
9. [Autenticação (login, JWT, chaves)](#9-autenticação-login-jwt-chaves)
10. [Ingestão de eventos](#10-ingestão-de-eventos)
11. [Ingestão de snapshots](#11-ingestão-de-snapshots)
12. [Ingestão de feedback](#12-ingestão-de-feedback)
13. [Consultas: eventos, sessões, traces](#13-consultas-eventos-sessões-traces)
14. [Consultas: overview, vitals, releases](#14-consultas-overview-vitals-releases)
15. [Consultas: issues, funnels, retention](#15-consultas-issues-funnels-retention)
16. [Endpoints administrativos](#16-endpoints-administrativos)
17. [LGPD: export e forget](#17-lgpd-export-e-forget)
18. [Rate limit, quotas e erros esperados](#18-rate-limit-quotas-e-erros-esperados)
19. [Como estender o backend](#19-como-estender-o-backend)
20. [Como estender o dashboard](#20-como-estender-o-dashboard)
21. [Como escrever um SDK novo](#21-como-escrever-um-sdk-novo)
22. [Testes automatizados](#22-testes-automatizados)
23. [Deploy em produção](#23-deploy-em-produção)
24. [Troubleshooting](#24-troubleshooting)

---

## 1. Setup do ambiente de desenvolvimento

### 1.1. Pré-requisitos

- **Docker + Docker Compose** (v2.20+).
- **Go 1.25+** (opcional, para rodar backend fora do container).
- **Node.js 20+** (opcional, para rodar dashboard fora do container).

### 1.2. Subir tudo com Docker

```bash
cd /Users/marcoscorrea/Develop/personal-projects/ndovu
docker compose up -d
```

Isso levanta 8 containers:

| Serviço | Porta local | UI |
|---|---|---|
| ndovu-api | 18081 | http://localhost:18081/docs (Swagger) |
| ndovu-dashboard | 13000 | http://localhost:13000 |
| ndovu-writer | — | — |
| postgres | 55432 | psql -h localhost -p 55432 -U ndovu |
| clickhouse | 8123 (HTTP), 9000 (native) | http://localhost:8123/play |
| nats | 4222 | — |
| mailhog | 11025 (SMTP), 18025 (UI) | http://localhost:18025 |
| minio | 19000 (API), 19001 (Console) | http://localhost:19001 (login: ndovu / ndovu-secret) |

### 1.3. Credenciais de bootstrap

Definidas em `docker-compose.yml`, criadas no primeiro startup:

- **Dashboard login:** `admin@ndovu.local` / `admin12345`
- **Chave de ingestão inicial:** `dev-ingest-key`

Trocar em produção via `NDOVU_ADMIN_EMAIL`, `NDOVU_ADMIN_PASSWORD`,
`NDOVU_BOOTSTRAP_INGEST_KEY`.

### 1.4. Rodar backend localmente (fora do Docker)

Útil para debug e desenvolvimento com hot-reload.

```bash
# Manter só as dependências no docker
docker compose up -d postgres clickhouse nats minio mailhog

# Rodar API local
cd backend
export NDOVU_POSTGRES_URL=postgres://ndovu:ndovu@localhost:55432/ndovu
export NDOVU_CLICKHOUSE_ADDR=localhost:9000
export NDOVU_NATS_URL=nats://localhost:4222
export NDOVU_PORT=18081
go run ./cmd/api

# Em outra aba: writer
cd backend
export NDOVU_POSTGRES_URL=postgres://ndovu:ndovu@localhost:55432/ndovu
export NDOVU_CLICKHOUSE_ADDR=localhost:9000
export NDOVU_NATS_URL=nats://localhost:4222
go run ./cmd/writer
```

### 1.5. Rodar dashboard localmente

```bash
cd dashboard
npm install
export NEXT_PUBLIC_NDOVU_API=http://localhost:18081
npm run dev
```

Abre em http://localhost:3000.

### 1.6. Gerar dados de exemplo (seed)

```bash
cd tools/seed
node seed.mjs
```

Simula sessões: login, 2ª via de boleto, geração de PIX, com erros
plantados. Rápido para popular o dashboard e testar funcionalidades.

### 1.7. Verificar saúde da instalação

```bash
# API viva?
curl -s http://localhost:18081/health

# Login funciona?
curl -sX POST http://localhost:18081/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@ndovu.local","password":"admin12345"}' | jq

# Ingestão funciona?
curl -sX POST http://localhost:18081/v1/events \
  -H 'X-Api-Key: dev-ingest-key' \
  -H 'Content-Type: application/json' \
  -d '{
    "app":"portal-teste",
    "session":{"sessionId":"s-1","userAgent":"curl"},
    "events":[{
      "eventId":"e-1",
      "type":"page_view",
      "name":"tela_home",
      "screen":"/",
      "timestamp":"2026-08-23T10:00:00Z"
    }]
  }'
# Esperado: HTTP 202 e {"accepted":1}
```

---

## 2. Variáveis de ambiente

Referência completa. Todas prefixadas com `NDOVU_`.

### 2.1. HTTP

| Var | Default | Descrição |
|---|---|---|
| `NDOVU_PORT` | `8080` | Porta HTTP da API |
| `NDOVU_LOG_LEVEL` | `info` | debug \| info \| warn \| error |
| `NDOVU_MAX_BODY_BYTES` | `1048576` | Limite de payload (1 MB) |

### 2.2. Bancos e dependências

| Var | Default | Descrição |
|---|---|---|
| `NDOVU_CLICKHOUSE_ADDR` | `localhost:9000` | Host:port ClickHouse (native protocol) |
| `NDOVU_CLICKHOUSE_DB` | `ndovu` | Nome do database |
| `NDOVU_CLICKHOUSE_USER` | `ndovu` | User |
| `NDOVU_CLICKHOUSE_PASSWORD` | `ndovu` | Senha |
| `NDOVU_NATS_URL` | `nats://localhost:4222` | URL NATS |
| `NDOVU_STREAM_MAX_AGE_HOURS` | `48` | Retenção do JetStream (h) |
| `NDOVU_WRITER_BATCH` | `64` | Msgs por fetch do writer |
| `NDOVU_WRITER_MAX_WAIT_MS` | `1000` | Espera máx (ms) por batch |
| `NDOVU_POSTGRES_URL` | `postgres://ndovu:ndovu@localhost:5432/ndovu` | Conn string |

### 2.3. Autenticação

| Var | Default | Descrição |
|---|---|---|
| `NDOVU_AUTH_SECRET` | `dev-secret-troque-em-producao` | Segredo HS256 do JWT |
| `NDOVU_AUTH_TOKEN_TTL_HOURS` | `8` | TTL do JWT |
| `NDOVU_ADMIN_EMAIL` | `admin@ndovu.local` | Bootstrap: email admin |
| `NDOVU_ADMIN_PASSWORD` | `admin12345` | Bootstrap: senha admin |
| `NDOVU_BOOTSTRAP_INGEST_KEY` | `dev-ingest-key` | Bootstrap: chave inicial |
| `NDOVU_KEY_CACHE_TTL_SECONDS` | `30` | Cache de validação de chaves |
| `NDOVU_INGEST_RATE_RPS` | `50` | Rate limit por chave (req/s) |

### 2.4. Alertas, anomalias, sampling

| Var | Default | Descrição |
|---|---|---|
| `NDOVU_ALERTS_INTERVAL_SECONDS` | `60` | Intervalo do avaliador de alertas |
| `NDOVU_SAMPLING_REFRESH_SECONDS` | `30` | Refresh do cache de sampling |
| `NDOVU_ANOMALY_INTERVAL_SECONDS` | `300` | Intervalo do avaliador de anomalias (5 min) |

### 2.5. Storage (S3 / MinIO)

| Var | Default | Descrição |
|---|---|---|
| `NDOVU_S3_ENDPOINT` | `""` | Endpoint S3 (vazio = snapshots off) |
| `NDOVU_S3_ACCESS_KEY` | `""` | Access key |
| `NDOVU_S3_SECRET_KEY` | `""` | Secret key |
| `NDOVU_S3_USE_SSL` | `false` | TLS |
| `NDOVU_SNAPSHOT_BUCKET` | `ndovu-snapshots` | Bucket de snapshots |

### 2.6. Email (mailer)

| Var | Default | Descrição |
|---|---|---|
| `NDOVU_MAILER_PROVIDER` | `""` | `auto` \| `sendgrid` \| `smtp` \| `noop` |
| `NDOVU_SMTP_HOST` | `""` | Host SMTP |
| `NDOVU_SMTP_PORT` | `1025` | Porta SMTP |
| `NDOVU_SMTP_USER` | `""` | User SMTP |
| `NDOVU_SMTP_PASSWORD` | `""` | Password SMTP |
| `NDOVU_SMTP_FROM` | `""` | Remetente (obrigatório se SMTP ativo) |
| `NDOVU_SENDGRID_API_KEY` | `""` | Se preenchido, prioriza SendGrid |

### 2.7. Digest semanal

| Var | Default | Descrição |
|---|---|---|
| `NDOVU_DIGEST_RECIPIENTS` | `""` | CSV de emails destino |
| `NDOVU_DIGEST_DASHBOARD_URL` | `http://localhost:13000` | URL nos links do email |
| `NDOVU_DIGEST_WEEKDAY` | `0` | 0=domingo, 6=sábado |
| `NDOVU_DIGEST_HOUR_UTC` | `20` | Hora UTC |
| `NDOVU_DIGEST_MINUTE_UTC` | `0` | Minuto UTC |
| `NDOVU_DIGEST_TICK_SECONDS` | `300` | Frequência do scheduler |

---

## 3. Contrato de ingestão v1

Este é o contrato "de lei". Qualquer cliente que envie o JSON está
integrado. Ver também `docs/CONTRACT.md`.

### 3.1. Endpoint

```
POST {NDOVU_API}/v1/events
Content-Type: application/json
X-Api-Key: <sua-chave>
```

### 3.2. Envelope (top-level)

```typescript
{
  app: string;              // obrigatório. Deve bater com a chave.
  sdkVersion?: string;      // ex.: "1.2.0"
  session: {
    sessionId: string;      // obrigatório. UUID persistente por visita.
    userId?: string;        // opcional. Só se autenticado.
    userAgent?: string;
    attributes?: Record<string, any>;  // plano, canal, release, etc.
  };
  events: Event[];          // 1..N
}
```

### 3.3. Event

```typescript
{
  eventId: string;                       // UUID único. Chave de dedup.
  type: "page_view" | "action" | "http_request" | "error" | "custom";
  name: string;                          // nome de negócio
  feature?: string;
  screen?: string;
  timestamp: string;                     // ISO 8601 UTC
  durationMs?: number;
  http?: {
    method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
    url: string;
    statusCode?: number;
    requestBody?: any;                   // será JSON.stringify
    responseBody?: any;
  };
  error?: {
    code: string;                        // ex.: "AUTH_401", "NETWORK_ERROR"
    message: string;
    body?: any;                          // stack, source, line, col, extras
  };
  trace?: {
    traceId: string;                     // 32 hex chars (W3C)
    spanId: string;                      // 16 hex chars
    parentSpanId?: string;
  };
  metadata?: Record<string, any>;        // livre
}
```

### 3.4. Respostas

| HTTP | Body | Quando |
|---|---|---|
| 202 | `{"accepted": N}` | Aceitou N eventos |
| 400 | `{"error": "...", "details": [...]}` | Contrato inválido, `app` não bate |
| 401 | `{"error": "unauthorized"}` | Chave inválida ou revogada |
| 413 | `{"error": "payload too large"}` | > `NDOVU_MAX_BODY_BYTES` |
| 429 | `{"error": "rate limit exceeded"}` | Rate limit atingido |
| 500 | `{"error": "internal"}` | Falha ao publicar no stream |

### 3.5. Cross-tenant guard

Se `envelope.app != key.app`, retorna 400 com detalhe. Impede envio
como "outra app". Se você tem 5 apps, precisa de 5 chaves.

---

## 4. SDK Browser (JavaScript)

Arquivo: `sdk/ndovu-browser.js` (v1.2.0, ~700 linhas, **zero
dependências**).

### 4.1. Criar instância

```javascript
import { createNdovu } from './ndovu-browser.js';

const ndovu = createNdovu({
  endpoint: 'https://ndovu.mycompany.internal',  // sem barra final
  apiKey: 'ndk_xxxxxxxxxxxx',                    // chave da app
  app: 'portal-cliente',                         // deve bater com a chave

  getUserId: () => window.currentUserId ?? null, // callback dinâmico
  flushIntervalMs: 5000,                         // batch a cada 5s
  maxBatch: 20,                                  // ou 20 eventos
  sessionAttributes: { plano: 'gold' },          // metadados da sessão
  release: '1.2.3',                              // versão do app

  captureGlobals: true,      // window.onerror, unhandledrejection, console.error
  captureWebVitals: true,    // LCP, CLS, INP, FID, TTFB, FCP
  breadcrumbs: true,         // ring buffer de últimos 30 eventos
  captureSnapshots: true,    // snapshot do DOM em cada error()
});
```

### 4.2. Métodos principais

```javascript
// Página visitada
ndovu.pageView('/faturas', { feature: 'faturas' });

// Ação do usuário
ndovu.action('clicou_gerar_boleto', { feature: 'faturas', screen: '/faturas' });

// Erro capturado manualmente
ndovu.error('falha_gerar_boleto', {
  code: 'BOLETO_INDISPONIVEL',
  message: 'Serviço fora do ar',
  body: { stack: err.stack },
  feature: 'faturas',
  screen: '/faturas',
});

// Chamada HTTP manual (se não usar instrumentFetch)
ndovu.httpRequest('gerar_boleto', {
  method: 'POST',
  url: '/api/boletos',
  statusCode: 500,
  requestBody: { cpf: '***' },
  responseBody: { error: 'timeout' },
  durationMs: 4200,
});

// Evento livre
ndovu.custom('feature_flag_ativada', { metadata: { flag: 'new-ui' } });

// Força envio da fila (útil antes de navegar)
ndovu.flush();

// Envia feedback do usuário
await ndovu.feedback('Botão de pagar não funciona', {
  type: 'bug',
  email: 'cliente@example.com',
});

// Monta widget flutuante de feedback
const unmount = ndovu.mountFeedbackWidget({
  position: 'bottom-right',
  label: 'Feedback',
});
// Depois: unmount() pra remover
```

### 4.3. Auto-instrumentação de fetch

```javascript
ndovu.instrumentFetch({
  nameFor: (url) => url.split('?')[0],   // customiza name
  ignore: ['/analytics/', '/beacon'],    // rotas a ignorar
  propagateTraceparent: true,            // W3C header no outgoing
});
```

Após chamar, cada `fetch()` no app vira um evento `http_request`
automaticamente — com método, URL, status, request/response body,
duração e trace_id/span_id.

### 4.4. Redação de PII

Chaves que combinam com este regex são substituídas por `***` antes de
enviar:

```
/pass(word)?|senha|token|secret|authorization|cvv|card|cart[aã]o/i
```

Aplicado recursivamente em `requestBody`, `responseBody`, `error.body`.

### 4.5. Mascaramento no snapshot (session replay)

Inputs sensíveis (`type=password|email|tel|cc-number|cc-csc`) têm
`value` substituído por `***`. Elementos com atributo `data-ndovu-mask`
têm o `textContent` substituído por `***`.

```html
<!-- Este número não vai no snapshot -->
<span data-ndovu-mask>{{ user.cpf }}</span>
```

### 4.6. Integração em SPA (React exemplo)

```jsx
// src/ndovu.js
import { createNdovu } from 'ndovu-browser';
export const ndovu = createNdovu({
  endpoint: process.env.REACT_APP_NDOVU_URL,
  apiKey: process.env.REACT_APP_NDOVU_KEY,
  app: 'meu-app',
  getUserId: () => window.__CURRENT_USER__?.id,
});
ndovu.instrumentFetch();

// src/App.jsx
import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { ndovu } from './ndovu';

function App() {
  const location = useLocation();
  useEffect(() => {
    ndovu.pageView(location.pathname);
  }, [location.pathname]);
  return <Routes />;
}
```

### 4.7. Error boundary React

```jsx
class NdovuErrorBoundary extends React.Component {
  componentDidCatch(error, info) {
    ndovu.error('react_error_boundary', {
      code: 'REACT_RENDER',
      message: error.message,
      body: { stack: error.stack, componentStack: info.componentStack },
    });
  }
  render() { return this.props.children; }
}
```

---

## 5. SDK Next.js

Arquivo: `sdk/ndovu-next.js`. Auto-instrumentação para Next.js App
Router.

```tsx
// app/layout.tsx
'use client';
import { NdovuProvider, NdovuAutoRouter } from './ndovu-next.js';
import { createNdovu } from './ndovu-browser.js';

const ndovu = createNdovu({
  endpoint: process.env.NEXT_PUBLIC_NDOVU_URL,
  apiKey: process.env.NEXT_PUBLIC_NDOVU_KEY,
  app: 'meu-app-next',
});
ndovu.instrumentFetch();

export default function RootLayout({ children }) {
  return (
    <html>
      <body>
        <NdovuProvider ndovu={ndovu}>
          <NdovuAutoRouter includeSearch={false} />
          {children}
        </NdovuProvider>
      </body>
    </html>
  );
}

// Em qualquer componente client:
import { useNdovu } from './ndovu-next.js';
function Comp() {
  const ndovu = useNdovu();
  return <button onClick={() => ndovu.action('clicou_X')}>X</button>;
}
```

`NdovuAutoRouter` observa `usePathname()` + `useSearchParams()` e
dispara `pageView()` automaticamente a cada mudança de rota — sem
código no app.

---

## 6. SDK Node.js

Arquivo: `sdk/ndovu-node.js` (v1.1.0-node, **zero dependências**).

Use em Express, Fastify, workers, cron jobs para capturar eventos do
backend (ou correlacionar com frontend via trace_id).

### 6.1. Criar instância

```javascript
import { createNdovu } from 'ndovu-node';

const ndovu = createNdovu({
  endpoint: process.env.NDOVU_URL,
  apiKey: process.env.NDOVU_KEY,
  app: 'api-cobranca',
  release: process.env.APP_VERSION,
});
```

### 6.2. Middleware Express

```javascript
import express from 'express';
const app = express();
app.use(ndovu.middleware());

app.get('/faturas/:id', async (req, res) => {
  // ndovu.getRequestId(req) — id da request
  // req.ndovu — SDK vinculado com sessionId derivado do request
  try {
    const fatura = await service.get(req.params.id);
    res.json(fatura);
  } catch (err) {
    ndovu.error('erro_get_fatura', {
      code: 'DB_ERROR',
      message: err.message,
      body: { faturaId: req.params.id, stack: err.stack },
    });
    res.status(500).send('erro');
  }
});
```

### 6.3. Captura de exceção não-tratada

```javascript
ndovu.captureUncaught();  // instala uncaughtException + unhandledRejection
```

### 6.4. Diferenças vs browser SDK

- Sem `sessionStorage` — sessionId gerado por request ou passado
  manualmente.
- Sem `captureWebVitals`, `captureSnapshots`, `mountFeedbackWidget`.
- Adiciona `serviceInstance` (hostname) automaticamente.
- Agrupa por sessionId antes de enviar (permite múltiplas sessões num
  batch).

---

## 7. SDK React Native

Arquivo: `sdk/ndovu-react-native.js`. Análogo ao browser, adaptado:

- Usa `AsyncStorage` em vez de `sessionStorage`.
- Não intercepta `fetch` global (RN tem próprio) — expõe
  `instrumentFetch()` explícito.
- Captura crash JS via `ErrorUtils.setGlobalHandler`.
- Captura network requests via `XMLHttpRequest.onreadystatechange`
  (opcional).
- Snapshot do DOM não faz sentido — omite.

Uso mínimo:

```javascript
import { createNdovu } from 'ndovu-react-native';
export const ndovu = createNdovu({
  endpoint: 'https://ndovu.mycompany.internal',
  apiKey: 'ndk_xxx',
  app: 'app-mobile',
  release: DeviceInfo.getVersion(),
});
ndovu.captureUncaught();

// Em navegação (React Navigation)
useEffect(() => {
  ndovu.pageView(route.name, { feature: route.params?.feature });
}, [route]);
```

---

## 8. Referência da API HTTP

Todos os endpoints prefixados com `/v1`. A URL base é configurada em
`NDOVU_PORT` (default 8080; docker-compose mapeia para 18081).

### 8.1. Health & docs (sem auth)

| Método | Path | Descrição |
|---|---|---|
| GET | `/health` | Retorna 200 OK |
| GET | `/metrics` | Métricas Prometheus |
| GET | `/openapi.yaml` | Spec OpenAPI YAML |
| GET | `/docs` | Swagger UI |

### 8.2. Autenticação

| Método | Path | Auth | Body / Query | Resposta |
|---|---|---|---|---|
| POST | `/v1/auth/login` | — | `{email, password}` | `{token, user}` |
| GET | `/v1/auth/me` | Bearer | — | `{userId, email, role, companyId, isSuper}` |

### 8.3. Ingestão

| Método | Path | Auth | Descrição |
|---|---|---|---|
| POST | `/v1/events` | X-Api-Key | Batch de eventos |
| POST | `/v1/snapshots` | X-Api-Key | Snapshot do DOM (para replay) |
| POST | `/v1/feedbacks` | X-Api-Key | Feedback do usuário (widget) |

### 8.4. Consultas

| Método | Path | Auth | Descrição |
|---|---|---|---|
| GET | `/v1/events` | Bearer | Lista eventos (filtro + cursor) |
| GET | `/v1/events/{id}` | Bearer | Evento único |
| GET | `/v1/events/{id}/resolved-stack` | Bearer | Stack desminificado |
| GET | `/v1/sessions` | Bearer | Lista sessões |
| GET | `/v1/sessions/{sessionId}` | Bearer | Sessão + timeline |
| GET | `/v1/traces/{traceId}` | Bearer | Timeline W3C |
| GET | `/v1/snapshots/event/{eventId}` | Bearer | Metadata do snapshot |
| GET | `/v1/snapshots/event/{eventId}/html` | Bearer | HTML do snapshot |
| GET | `/v1/stats/overview` | Bearer | KPIs + série temporal |
| GET | `/v1/stats/vitals` | Bearer | Web Vitals por rota |
| GET | `/v1/stats/compare` | Bearer | Comparar releases |
| GET | `/v1/stats/retention` | Bearer | Cohorts D1/D7/D14/D30 |
| GET | `/v1/meta/filters` | Bearer | Valores distintos para filtros |
| GET | `/v1/issues` | Bearer | Erros agrupados |
| PATCH | `/v1/issues/{fingerprint}` | Bearer | Atualiza status/assignee |
| GET | `/v1/issues/{fingerprint}/comments` | Bearer | Comentários |
| POST | `/v1/issues/{fingerprint}/comments` | Bearer | Novo comentário |
| DELETE | `/v1/issues/comments/{id}` | Bearer | Deleta comentário |
| GET | `/v1/releases` | Bearer | Lista releases |
| GET | `/v1/funnels` | Bearer | Lista funis |
| POST | `/v1/funnels` | Bearer | Cria funil |
| PATCH | `/v1/funnels/{id}` | Bearer | Atualiza funil |
| DELETE | `/v1/funnels/{id}` | Bearer | Deleta funil |
| GET | `/v1/funnels/{id}/results` | Bearer | Executa funil |
| GET | `/v1/saved-views` | Bearer | Presets do usuário |
| POST | `/v1/saved-views` | Bearer | Cria preset |
| PATCH | `/v1/saved-views/{id}` | Bearer | Atualiza preset |
| DELETE | `/v1/saved-views/{id}` | Bearer | Deleta preset |

### 8.5. Admin

Todos requerem Bearer JWT com role `admin`.

| Método | Path | Descrição |
|---|---|---|
| GET / POST | `/v1/admin/users` | Lista / cria |
| PATCH | `/v1/admin/users/{id}` | Atualiza |
| GET / PUT / DELETE | `/v1/admin/users/{id}/permissions[/{appId}]` | RBAC granular |
| CRUD | `/v1/admin/companies` | Empresas |
| CRUD | `/v1/admin/apps` | Apps |
| GET / POST / DELETE | `/v1/admin/api-keys` | Chaves |
| GET / POST / DELETE | `/v1/admin/alerts` | Regras de alerta |
| CRUD | `/v1/admin/anomaly-rules` | Regras de anomalia |
| GET | `/v1/admin/anomaly-detections` | Histórico de anomalias |
| CRUD | `/v1/admin/sampling-rules` | Regras de sampling |
| GET / POST / DELETE | `/v1/admin/source-maps` | Source maps |
| GET / PATCH / DELETE | `/v1/admin/feedbacks[/{id}]` | Triagem de feedback |
| POST | `/v1/admin/digest/send-now` | Força envio do digest |
| GET | `/v1/admin/audit-log` | Log de ações admin |
| GET | `/v1/admin/gdpr/user/{userId}/export` | Exporta dados do titular |
| DELETE | `/v1/admin/gdpr/user/{userId}` | Direito ao esquecimento |

---

## 9. Autenticação (login, JWT, chaves)

### 9.1. Login

```bash
curl -sX POST http://localhost:18081/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@ndovu.local","password":"admin12345"}'
```

Resposta:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user": {
    "id": "uuid-do-usuario",
    "email": "admin@ndovu.local",
    "role": "admin",
    "companyId": "uuid-da-empresa",
    "isSuper": true
  }
}
```

O token dura 8h (`NDOVU_AUTH_TOKEN_TTL_HOURS`).

### 9.2. Uso do JWT

```bash
TOKEN="eyJhb..."
curl -s http://localhost:18081/v1/events?limit=10 \
  -H "Authorization: Bearer $TOKEN"
```

### 9.3. `/v1/auth/me`

Retorna identidade do token atual. Útil para o dashboard descobrir
quem é o usuário logado.

### 9.4. Criar chave nova

```bash
curl -sX POST http://localhost:18081/v1/admin/api-keys \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"appId":"uuid-da-app","note":"prod key rotação Ago/26"}'
```

Resposta (a chave é exibida **1 única vez**):
```json
{
  "id": "uuid-da-chave",
  "app": "portal-cliente",
  "key": "ndk_abcdef1234567890",
  "createdAt": "2026-08-23T10:00:00Z"
}
```

### 9.5. Revogar chave

```bash
curl -sX DELETE http://localhost:18081/v1/admin/api-keys/uuid-da-chave \
  -H "Authorization: Bearer $TOKEN"
```

Efeito em até 30s (TTL do cache).

---

## 10. Ingestão de eventos

### 10.1. POST /v1/events

```bash
curl -sX POST http://localhost:18081/v1/events \
  -H "X-Api-Key: dev-ingest-key" \
  -H 'Content-Type: application/json' \
  -d '{
    "app": "portal-cliente",
    "sdkVersion": "1.2.0",
    "session": {
      "sessionId": "550e8400-e29b-41d4-a716-446655440000",
      "userId": "user-123",
      "userAgent": "Mozilla/5.0 ...",
      "attributes": {"plano": "gold", "release": "1.2.3"}
    },
    "events": [
      {
        "eventId": "1",
        "type": "page_view",
        "name": "tela_faturas",
        "screen": "/faturas",
        "timestamp": "2026-08-23T10:00:00Z"
      },
      {
        "eventId": "2",
        "type": "http_request",
        "name": "buscar_faturas",
        "timestamp": "2026-08-23T10:00:01Z",
        "durationMs": 220,
        "http": {
          "method": "GET",
          "url": "/api/faturas",
          "statusCode": 200
        },
        "trace": {"traceId": "abc123...", "spanId": "def456"}
      }
    ]
  }'
```

Resposta: `202 Accepted` + `{"accepted": 2}`.

### 10.2. Batch máximo

Recomendado: até 100 eventos por batch. Limite hard: 1 MB de body
(`NDOVU_MAX_BODY_BYTES`).

### 10.3. Consequências de campos faltando

- `app` faltando: 400.
- `session.sessionId` faltando: 400.
- `event.eventId` faltando: 400 (ou o SDK gera automaticamente).
- `event.type` inválido: 400.
- `event.timestamp` inválido: 400 (aceita ISO 8601 UTC).

---

## 11. Ingestão de snapshots

```bash
curl -sX POST http://localhost:18081/v1/snapshots \
  -H "X-Api-Key: dev-ingest-key" \
  -H 'Content-Type: application/json' \
  -d '{
    "eventId": "uuid-do-evento-que-quebrou",
    "sessionId": "uuid-da-sessao",
    "app": "portal-cliente",
    "html": "<!doctype html><html>...",
    "url": "https://app.com/checkout",
    "viewportW": 1920,
    "viewportH": 1080,
    "takenAt": "2026-08-23T10:00:05Z"
  }'
```

Recomenda-se gzip do HTML antes de mandar (o SDK faz isso). O sistema
aceita ambos (raw e gzip).

### 11.1. Onde é armazenado

- Metadata em Postgres (`session_snapshots`).
- Blob em MinIO / S3, key: `snapshots/{app}/{yyyy-mm-dd}/{eventId}.html.gz`.

### 11.2. Visualização

```bash
# metadata
curl -s http://localhost:18081/v1/snapshots/event/uuid-do-evento \
  -H "Authorization: Bearer $TOKEN"

# HTML (para renderizar em iframe)
curl -s http://localhost:18081/v1/snapshots/event/uuid-do-evento/html \
  -H "Authorization: Bearer $TOKEN"
```

---

## 12. Ingestão de feedback

Enviado pelo widget do SDK (`sdk.mountFeedbackWidget()` ou
`sdk.feedback(...)` programaticamente).

```bash
curl -sX POST http://localhost:18081/v1/feedbacks \
  -H "X-Api-Key: dev-ingest-key" \
  -H 'Content-Type: application/json' \
  -d '{
    "app": "portal-cliente",
    "sessionId": "uuid-da-sessao",
    "eventId": "uuid-do-ultimo-evento",
    "type": "bug",
    "message": "O botão de pagamento não funciona",
    "email": "cliente@example.com",
    "url": "https://app.com/checkout",
    "viewportW": 1920,
    "viewportH": 1080
  }'
```

Campos obrigatórios: `app`, `sessionId`, `message` (max 5000 chars).

Tipos válidos: `bug | suggestion | praise | other`.

Resposta: 200 + o objeto criado com `id` e `status: "new"`.

---

## 13. Consultas: eventos, sessões, traces

### 13.1. Listar eventos

```bash
curl -s "http://localhost:18081/v1/events?from=2026-08-22T00:00:00Z&to=2026-08-23T00:00:00Z&app=portal-cliente&type=error&limit=50" \
  -H "Authorization: Bearer $TOKEN" | jq
```

Parâmetros aceitos:
- `from`, `to` — ISO 8601 UTC
- `app`, `userId`, `sessionId`, `type`, `feature`, `name`, `screen`
- `route` — URL da chamada HTTP (contains)
- `statusMin`, `statusMax` — status HTTP
- `onlyErrors` — booleano
- `search` — full-text em body/message
- `limit` (default 50, max 200)
- `cursor` — keyset paginação (retornado na resposta)

Resposta:
```json
{
  "events": [...],
  "nextCursor": "eyJvY2N1cnJlZEF0IjoiMjAyNi0wOC0yM1QwOTo1OTo1OS45OTlaIiwiaWQiOiJ4eXoifQ=="
}
```

### 13.2. Evento único (com payloads)

```bash
curl -s http://localhost:18081/v1/events/uuid-do-evento \
  -H "Authorization: Bearer $TOKEN" | jq
```

### 13.3. Stack desminificado

```bash
curl -s http://localhost:18081/v1/events/uuid-do-evento/resolved-stack \
  -H "Authorization: Bearer $TOKEN" | jq
```

Resposta:
```json
{
  "frames": [
    {
      "file": "app.min.js",
      "line": 1, "column": 12345,
      "original": true,
      "source": "src/checkout.jsx",
      "originalLine": 42,
      "function": "handlePagamento"
    }
  ]
}
```

Requer source map correspondente uploaded (`POST /v1/admin/source-maps`).

### 13.4. Sessões

```bash
curl -s "http://localhost:18081/v1/sessions?app=portal-cliente&onlyErrors=true&limit=20" \
  -H "Authorization: Bearer $TOKEN"

curl -s http://localhost:18081/v1/sessions/uuid-da-sessao \
  -H "Authorization: Bearer $TOKEN"
```

### 13.5. Trace W3C

```bash
curl -s http://localhost:18081/v1/traces/abc123def456... \
  -H "Authorization: Bearer $TOKEN"
```

Retorna todos os eventos (frontend + backend, se instrumentado) com
aquele traceId.

---

## 14. Consultas: overview, vitals, releases

### 14.1. Overview

```bash
curl -s "http://localhost:18081/v1/stats/overview?from=2026-08-16T00:00:00Z&to=2026-08-23T00:00:00Z&app=portal-cliente" \
  -H "Authorization: Bearer $TOKEN"
```

Retorna:
```json
{
  "totalEvents": 123456,
  "totalSessions": 8901,
  "totalUsers": 2340,
  "errorRate": 0.023,
  "buckets": [
    {"t": "2026-08-16T00:00:00Z", "events": 5000, "errors": 42, "sessions": 300},
    ...
  ],
  "topRoutes": [
    {"route": "/api/faturas", "count": 2340, "p95": 220},
    ...
  ],
  "topErrors": [
    {"code": "AUTH_401", "count": 88},
    ...
  ]
}
```

### 14.2. Web Vitals

```bash
curl -s "http://localhost:18081/v1/stats/vitals?from=...&to=...&app=..." \
  -H "Authorization: Bearer $TOKEN"
```

Retorna estatísticas p75 e p95 de LCP, CLS, INP, FID, TTFB, FCP por
rota, com contagem de good/needs-improvement/poor.

### 14.3. Comparar releases

```bash
curl -s "http://localhost:18081/v1/stats/compare?app=portal-cliente&releaseA=1.2.2&releaseB=1.2.3&from=...&to=..." \
  -H "Authorization: Bearer $TOKEN"
```

### 14.4. Retention

```bash
curl -s "http://localhost:18081/v1/stats/retention?app=portal-cliente&from=...&to=..." \
  -H "Authorization: Bearer $TOKEN"
```

---

## 15. Consultas: issues, funnels, retention

### 15.1. Issues

```bash
curl -s "http://localhost:18081/v1/issues?app=portal-cliente&status=open" \
  -H "Authorization: Bearer $TOKEN"

# Atualizar status
curl -sX PATCH http://localhost:18081/v1/issues/<fingerprint> \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"status":"investigating","assigneeUserId":"uuid-do-dev"}'

# Comentar
curl -sX POST http://localhost:18081/v1/issues/<fingerprint>/comments \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"body":"Investigando — parece um race condition"}'
```

### 15.2. Funnels

```bash
# Criar
curl -sX POST http://localhost:18081/v1/funnels \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "app": "portal-cliente",
    "name": "Onboarding B2C",
    "steps": [
      {"type": "page_view", "name": "tela_cadastro"},
      {"type": "action", "name": "clicou_confirmar"},
      {"type": "custom", "name": "primeira_compra"}
    ],
    "windowSeconds": 86400
  }'

# Executar
curl -s "http://localhost:18081/v1/funnels/<id>/results?from=...&to=..." \
  -H "Authorization: Bearer $TOKEN"
```

---

## 16. Endpoints administrativos

### 16.1. Criar usuário

```bash
curl -sX POST http://localhost:18081/v1/admin/users \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "email": "dev1@empresa.com",
    "password": "temporaria123",
    "name": "Fulano",
    "role": "viewer",
    "companyId": "uuid-da-empresa",
    "active": true
  }'
```

### 16.2. Conceder permissão granular

```bash
curl -sX PUT http://localhost:18081/v1/admin/users/<userId>/permissions/<appId> \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"role":"viewer"}'
```

### 16.3. Criar app

```bash
curl -sX POST http://localhost:18081/v1/admin/apps \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "app-cobranca-mobile",
    "technology": "react-native",
    "companyId": "uuid-da-empresa",
    "responsible": "Time Cobrança"
  }'
```

Retorna a `key` inicial (exibida 1x).

### 16.4. Regra de alerta

```bash
curl -sX POST http://localhost:18081/v1/admin/alerts \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "500s no checkout",
    "app": "portal-cliente",
    "errorCode": "HTTP_500",
    "threshold": 10,
    "windowSeconds": 300,
    "channel": "slack",
    "targetUrl": "https://hooks.slack.com/services/...",
    "silenceSeconds": 1800,
    "active": true
  }'
```

### 16.5. Regra de anomalia

```bash
curl -sX POST http://localhost:18081/v1/admin/anomaly-rules \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "spike-erro-portal",
    "app": "portal-cliente",
    "metric": "error_count",
    "windowMinutes": 15,
    "baselineWeeks": 4,
    "sensitivity": 3,
    "direction": "above",
    "silenceSeconds": 1800,
    "channel": "slack",
    "targetUrl": "https://hooks.slack.com/...",
    "active": true
  }'
```

### 16.6. Regra de sampling

```bash
curl -sX POST http://localhost:18081/v1/admin/sampling-rules \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "app": "portal-cliente",
    "eventType": "page_view",
    "sampleRate": 0.5,
    "keepErrors": true,
    "active": true,
    "note": "reduzir volume de page_view em 50%"
  }'
```

### 16.7. Upload source map

```bash
curl -sX POST http://localhost:18081/v1/admin/source-maps \
  -H "Authorization: Bearer $TOKEN" \
  -F 'app=portal-cliente' \
  -F 'release=1.2.3' \
  -F 'filename=app.min.js' \
  -F 'sourcemap=@./dist/app.min.js.map'
```

---

## 17. LGPD: export e forget

### 17.1. Exportar dados de titular

```bash
curl -s http://localhost:18081/v1/admin/gdpr/user/<userId>/export \
  -H "Authorization: Bearer $TOKEN" > titular-<userId>.json
```

Retorna JSON com **todos** os eventos daquele `userId` (independente
de app, período, tipo).

### 17.2. Direito ao esquecimento

```bash
curl -sX DELETE http://localhost:18081/v1/admin/gdpr/user/<userId> \
  -H "Authorization: Bearer $TOKEN"
```

Executa `ALTER TABLE trace_events DELETE WHERE user_id = <userId>` no
ClickHouse. É apagamento efetivo (não soft delete). A ação fica no
audit log.

---

## 18. Rate limit, quotas e erros esperados

### 18.1. Rate limit

- Default: 50 req/s por chave (`NDOVU_INGEST_RATE_RPS`).
- Excedeu: 429 com header `Retry-After` em segundos.
- Rate limit é **por chave**, não por IP.

### 18.2. Payload gigante

- Limite: 1 MB (`NDOVU_MAX_BODY_BYTES`).
- Excedeu: 413.

### 18.3. Erros comuns e o que fazer

| Erro | Causa provável | Solução |
|---|---|---|
| 400 `app_mismatch` | `envelope.app != key.app` | Verificar qual chave está usando |
| 400 `invalid_timestamp` | timestamp não é ISO 8601 UTC | Usar `new Date().toISOString()` |
| 401 `unauthorized` | Chave inválida ou revogada | Gerar nova chave |
| 401 `token_expired` | JWT expirou (>8h) | Fazer login novamente |
| 403 `forbidden` | Role insuficiente | Precisa ser admin |
| 413 `payload_too_large` | Batch > 1 MB | Reduzir batch (max ~100 eventos) |
| 429 `rate_limit_exceeded` | > 50 req/s por chave | Backoff + retry |
| 500 `internal` | NATS fora ou stream cheio | Aguardar e tentar de novo (idempotente) |

---

## 19. Como estender o backend

### 19.1. Adicionar novo endpoint (padrão)

Exemplo: novo endpoint `GET /v1/stats/webhooks-errors`.

**Passo 1:** definir o tipo no domain.

```go
// backend/internal/domain/ports.go
type WebhookErrorStat struct {
    URL string
    Count int
    LastSeen time.Time
}

type EventReader interface {
    // ... existentes
    FindWebhookErrors(ctx context.Context, app string, from, to time.Time) ([]WebhookErrorStat, error)
}
```

**Passo 2:** implementar no adapter.

```go
// backend/internal/adapter/clickhouse/repository.go
func (r *Repo) FindWebhookErrors(...) ([]domain.WebhookErrorStat, error) {
    query := `SELECT http_url, count() as c, max(occurred_at) FROM trace_events
              WHERE app=@app AND occurred_at BETWEEN @from AND @to
                AND event_type='http_request' AND http_status >= 500
              GROUP BY http_url ORDER BY c DESC LIMIT 20`
    // ...
}
```

**Passo 3:** expor no service.

```go
// backend/internal/usecase/query.go
func (s *QueryService) WebhookErrors(ctx context.Context, scope AppScope, app string, from, to time.Time) (...) {
    // enforce scope
    if !scope.Includes(app) { return nil, ErrForbidden }
    return s.reader.FindWebhookErrors(ctx, app, from, to)
}
```

**Passo 4:** handler + rota.

```go
// backend/internal/adapter/httpapi/handlers.go
func (h *Handlers) GetWebhookErrors(w http.ResponseWriter, r *http.Request) {
    scope := tenantScopeFromCtx(r.Context())
    app := r.URL.Query().Get("app")
    from, _ := parseTime(r.URL.Query().Get("from"))
    to, _ := parseTime(r.URL.Query().Get("to"))
    stats, err := h.query.WebhookErrors(r.Context(), scope, app, from, to)
    if err != nil { writeError(w, err); return }
    writeJSON(w, 200, stats)
}

// backend/internal/adapter/httpapi/router.go
r.Get("/v1/stats/webhooks-errors", h.GetWebhookErrors)
```

**Passo 5:** wiring (nenhuma mudança — `handlers.query` já existe).

**Passo 6:** testes.

```go
// backend/internal/usecase/query_test.go
func TestWebhookErrors_EnforceScope(t *testing.T) { ... }
```

### 19.2. Adicionar tabela nova no Postgres

**Passo 1:** criar migração.

```
backend/internal/adapter/ctlpostgres/migrations/000016_minha_tabela.up.sql
backend/internal/adapter/ctlpostgres/migrations/000016_minha_tabela.down.sql
```

**Passo 2:** definir Store em domain.

```go
// backend/internal/domain/controlplane.go
type MinhaCoisa struct { ID, Name string; CreatedAt time.Time }
type MinhaCoisaStore interface {
    Create(ctx context.Context, c MinhaCoisa) error
    List(ctx context.Context) ([]MinhaCoisa, error)
    Delete(ctx context.Context, id string) error
}
```

**Passo 3:** implementar no `ctlpostgres/repository.go`.

**Passo 4:** service em `usecase/`.

**Passo 5:** handlers + rotas.

**Passo 6:** wiring em `cmd/api/main.go`.

**Passo 7:** rebuild.

```bash
cd backend && go build ./... && go test ./...
docker compose build api && docker compose up -d --force-recreate api
```

A migração roda automaticamente no startup (via `golang-migrate`
embedado).

### 19.3. Adicionar campo ao ClickHouse

**Não** requer migração formal — o schema é executado em cada startup
com `IF NOT EXISTS`. Para colunas novas, use `ALTER TABLE`:

```go
// backend/internal/adapter/clickhouse/repository.go
// no EnsureSchema, adicionar após CREATE TABLE:
_, _ = r.conn.Exec(ctx, `ALTER TABLE trace_events ADD COLUMN IF NOT EXISTS
    novo_campo LowCardinality(String) DEFAULT ''`)
```

### 19.4. Adicionar novo canal de alerta (ex.: Teams)

Editar `backend/internal/usecase/dispatcher.go`:

```go
func (d *AlertDispatcher) Dispatch(ctx context.Context, ch domain.AlertChannel, url string, payload map[string]any) error {
    switch ch {
    case domain.AlertChannelSlack:
        return d.sendSlack(ctx, url, payload)
    case domain.AlertChannelWebhook:
        return d.sendWebhook(ctx, url, payload)
    case domain.AlertChannelTeams:  // NOVO
        return d.sendTeams(ctx, url, payload)
    }
    return fmt.Errorf("canal desconhecido: %s", ch)
}
```

Adicionar `sendTeams` ~50 linhas com o formato de payload que o Teams
espera. Atualizar CHECK constraint na tabela `alert_rules`.

---

## 20. Como estender o dashboard

### 20.1. Adicionar nova página

**Passo 1:** criar arquivo em `dashboard/src/app/nova-pagina/page.tsx`.

```tsx
'use client';
import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';

export default function NovaPaginaPage() {
  const q = useQuery({ queryKey: ['nova-coisa'], queryFn: api.listNovaCoisa });
  if (q.isLoading) return <div>...</div>;
  return <ul>{q.data?.map(c => <li key={c.id}>{c.name}</li>)}</ul>;
}
```

**Passo 2:** adicionar item no menu (`dashboard/src/components/AppShell.tsx`).

```tsx
const NAV: NavItem[] = [
  // ...
  { href: '/nova-pagina', label: 'Nova coisa', icon: Icon.nova },
];
```

**Passo 3:** adicionar método em `api.ts`.

```ts
listNovaCoisa: () => request<{items: NovaCoisa[]}>('GET', '/v1/nova-coisa'),
```

### 20.2. Componentes reutilizáveis

Existem componentes prontos em `dashboard/src/components/`:

- `<Button variant="primary|danger|ghost" size="sm|md">`
- `<Input>`, `<Select>`, `<Field label="X" hint="Y">`
- `<Modal open onClose title>` + `<ModalActions>`
- `<LoadingState>`, `<ErrorState message>`
- `<SnapshotViewer eventId>` — renderiza replay em iframe
- `<JsonView data>` — expandir/colapsar JSON

Use-os para consistência visual.

### 20.3. Padrão de mutation

```tsx
const qc = useQueryClient();
const create = useMutation({
  mutationFn: (input: X) => api.createX(input),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['xs'] }),
});

<Button onClick={() => create.mutate(input)} loading={create.isPending}>
  criar
</Button>
```

---

## 21. Como escrever um SDK novo

Requisitos mínimos para conformidade com o contrato v1:

1. **HTTP client:** POST JSON para `{endpoint}/v1/events`.
2. **Header:** `X-Api-Key: <key>` e `Content-Type: application/json`.
3. **Envelope:** `{app, sdkVersion, session, events}`.
4. **Batching:** acumular N eventos em memória, enviar a cada M
   segundos ou N eventos (o que vier primeiro).
5. **Retry com idempotência:** se falha de rede, tentar novamente com
   o mesmo `eventId` (idempotência garantida pelo ClickHouse).
6. **`eventId` como UUID v4:** garantir unicidade.
7. **`timestamp` em ISO 8601 UTC:** `YYYY-MM-DDTHH:mm:ss.sssZ`.
8. **`type` válido:** `page_view|action|http_request|error|custom`.
9. **PII redaction:** remover `password|token|cvv|card|senha` antes de
   enviar (regex é responsabilidade do SDK).

Referências: ver os 4 SDKs oficiais em `sdk/`.

Exemplo mínimo em Python (~30 linhas):

```python
import json, uuid, requests, threading, time
from datetime import datetime, timezone

class Ndovu:
    def __init__(self, endpoint, api_key, app):
        self.endpoint = endpoint.rstrip('/')
        self.headers = {'X-Api-Key': api_key, 'Content-Type': 'application/json'}
        self.app = app
        self.session_id = str(uuid.uuid4())
        self.queue = []
        threading.Thread(target=self._flush_loop, daemon=True).start()

    def track(self, type, name, **kwargs):
        self.queue.append({
            'eventId': str(uuid.uuid4()),
            'type': type,
            'name': name,
            'timestamp': datetime.now(timezone.utc).isoformat(),
            **kwargs,
        })

    def _flush_loop(self):
        while True:
            time.sleep(5)
            if not self.queue: continue
            events = self.queue[:]
            self.queue.clear()
            envelope = {
                'app': self.app,
                'sdkVersion': 'py-0.1',
                'session': {'sessionId': self.session_id},
                'events': events,
            }
            try:
                requests.post(f'{self.endpoint}/v1/events',
                              headers=self.headers, json=envelope, timeout=5)
            except Exception:
                self.queue = events + self.queue  # devolve na fila
```

---

## 22. Testes automatizados

### 22.1. Backend

```bash
cd backend
go build ./...
go vet ./...
go test -count=1 ./...
```

Cobertura atual:

- `domain/fingerprint_test.go` — hash de agrupamento.
- `platform/mailer_factory_test.go` — factory auto-detect.
- `usecase/ingest_test.go` — validação de contrato.
- `usecase/app_test.go` — CRUD apps.
- `usecase/auth_test.go` — login + hash + validação.
- `usecase/anomaly_test.go` — z-score e silence window.
- `usecase/sampling_test.go` — precedência de regras.
- `adapter/natsstream/stream_test.go` — dedup, reentrega, replay
  (usa server JetStream embedded).
- `adapter/httpapi/handlers_test.go` — handlers com fake stores.

### 22.2. Dashboard

```bash
cd dashboard
npx tsc --noEmit    # typecheck
```

Não há testes unitários JS/TS ainda — item de roadmap.

### 22.3. Teste manual smoke

```bash
# Após qualquer mudança:
docker compose build api dashboard
docker compose up -d --force-recreate api dashboard
sleep 5

# Fumaça: login + ingest + list
curl -s http://localhost:18081/health
curl -sX POST http://localhost:18081/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@ndovu.local","password":"admin12345"}' | jq .token
```

---

## 23. Deploy em produção

### 23.1. Requisitos

- Docker + Docker Compose OU Kubernetes (Helm chart em roadmap).
- Reverse proxy TLS: Caddy, Nginx, Traefik.
- Postgres, ClickHouse, NATS, S3 — pode ser em cluster ou managed
  service (RDS, Aiven, Confluent, AWS S3).

### 23.2. Passos mínimos

1. **Troque todos os secrets** em variáveis de ambiente:
   - `NDOVU_AUTH_SECRET`
   - `NDOVU_ADMIN_EMAIL` + `NDOVU_ADMIN_PASSWORD`
   - `NDOVU_BOOTSTRAP_INGEST_KEY`
   - Passwords de Postgres, ClickHouse, MinIO/S3.

2. **Configure TLS** no reverse proxy. Endpoints públicos:
   - `https://ndovu-api.mycompany.com` → API (porta interna 8080)
   - `https://ndovu.mycompany.com` → dashboard (porta interna 3000)

3. **Ajuste retenção** no `schema.sql` se 90 dias não for suficiente
   (mudança futura requer `ALTER TABLE ... MODIFY TTL`).

4. **Configure email** (SendGrid ou SMTP corporativo) para digest.

5. **Configure alertas** e monitore `/metrics` externamente.

6. **Backup**:
   - Postgres: `pg_dump` diário → S3.
   - ClickHouse hot/warm: `BACKUP DATABASE` semanal → S3.

### 23.3. Recomendações de rede

- API não deve ser exposta diretamente à internet. Passar por reverse
  proxy que faz rate limit por IP + termina TLS.
- Ingestão pode ser rede pública (chave X-Api-Key valida) OU rede
  interna (VPN).
- Dashboard pode ser interno (VPN, SSO) ou público (JWT valida).

### 23.4. Hardening

- Trocar user root do Postgres e do ClickHouse por usuários com
  permissão mínima.
- Ativar TLS entre serviços (não só borda).
- Rotação de `NDOVU_AUTH_SECRET` a cada 90 dias (invalida todos os
  JWTs — force logout do time).
- Alerta em `/metrics` para picos incomuns de erro 5xx.
- Log estruturado enviado a SIEM (Splunk, Elastic, Datadog logs).

---

## 24. Troubleshooting

### 24.1. Ingestão retorna 202 mas evento não aparece no dashboard

**Verificar em ordem:**

1. NATS está saudável?
   ```bash
   docker logs ndovu-nats-1 --tail 20
   ```

2. Writer está consumindo?
   ```bash
   docker logs ndovu-writer-1 --tail 50
   # Deve ter linhas "insertion succeeded"
   ```

3. ClickHouse está OK?
   ```bash
   docker exec ndovu-clickhouse-1 clickhouse-client -q \
     "SELECT count() FROM trace_events WHERE app='portal-teste' LIMIT 1"
   ```

4. Filtro na UI está certo? (verificar período, app, tipo).

### 24.2. Login falha com "unauthorized"

- Confirme email + password (bootstrap: `admin@ndovu.local` /
  `admin12345`).
- Se trocou `NDOVU_ADMIN_PASSWORD` sem apagar o volume, o admin
  antigo persiste. Para resetar:
  ```bash
  docker compose down -v
  docker compose up -d
  ```

### 24.3. Chave X-Api-Key retorna 401

- Confirme que a chave existe no dashboard `/admin/keys`.
- Confirme que o `envelope.app` bate com o app da chave.
- Se acabou de revogar e criar nova, aguarde 30s (cache).

### 24.4. Snapshot não aparece na issue

- Verifique se `captureSnapshots: true` no SDK.
- Verifique se MinIO/S3 está saudável:
  ```bash
  docker logs ndovu-minio-1 --tail 20
  ```
- Confirme que `NDOVU_S3_ENDPOINT` está configurado na API.

### 24.5. Alertas não disparam

- Regra está `active`?
- Threshold está condizente com volume real?
- URL do webhook está acessível a partir do container do writer?
  ```bash
  docker exec ndovu-writer-1 wget -qO- https://hooks.slack.com/.../teste
  ```

### 24.6. Anomalias não disparam

- Baseline requer histórico. Se o sistema tem só 1 semana de dados
  e a regra pede `baselineWeeks: 4`, não há baseline suficiente —
  a regra fica silenciosa.
- Confirme métrica escolhida faz sentido para o app (`error_rate`
  precisa ter erros e eventos no denominador).

### 24.7. Dashboard mostra "Erro ao carregar"

- Confirme `NEXT_PUBLIC_NDOVU_API` no ambiente do dashboard.
- Verifique CORS na API (se dashboard em domínio diferente).
- Abra DevTools → Network → veja o status real da requisição.

### 24.8. Digest não chega

- Confirme `NDOVU_MAILER_PROVIDER` + credenciais corretas.
- Confirme `NDOVU_DIGEST_RECIPIENTS` (CSV).
- Force envio imediato:
  ```bash
  curl -sX POST http://localhost:18081/v1/admin/digest/send-now \
    -H "Authorization: Bearer $TOKEN"
  ```
- Em dev, MailHog UI está em http://localhost:18025.

---

## Apêndices

### A.1. Estrutura de repositório

```
ndovu/
├── backend/                    # Go 1.25
│   ├── api/openapi.yaml
│   ├── cmd/{api,writer}/
│   ├── go.mod / go.sum
│   └── internal/
│       ├── adapter/{httpapi,clickhouse,ctlpostgres,natsstream,blobstore}/
│       ├── config/
│       ├── domain/
│       ├── platform/
│       └── usecase/
├── dashboard/                  # Next.js 15
│   ├── package.json
│   ├── next.config.mjs
│   ├── tailwind.config.js
│   └── src/
│       ├── app/                # rotas
│       ├── components/         # UI
│       └── lib/                # api.ts, auth.ts, types.ts, time.ts
├── sdk/
│   ├── ndovu-browser.js        # v1.2.0
│   ├── ndovu-next.js
│   ├── ndovu-node.js           # v1.1.0
│   └── ndovu-react-native.js
├── tools/
│   └── seed/seed.mjs           # simulador de sessões
├── infra/
│   └── clickhouse/storage.xml  # storage policy tiered
├── docs/
│   ├── EXECUTIVA.md            # este doc
│   ├── FUNCIONAL.md
│   ├── ARQUITETURA.md
│   ├── TECNICA.md
│   ├── ARCHITECTURE.md         # legado
│   ├── CONTRACT.md
│   └── INTEGRATION.md
├── docker-compose.yml
└── README.md
```

### A.2. Comandos úteis de operação

```bash
# Ver logs em tempo real
docker compose logs -f api writer

# Restart limpo
docker compose restart api writer

# Reset total (perde volumes)
docker compose down -v && docker compose up -d

# Console SQL do ClickHouse
docker exec -it ndovu-clickhouse-1 clickhouse-client

# Console SQL do Postgres
docker exec -it ndovu-postgres-1 psql -U ndovu -d ndovu

# Ver stream do NATS
docker exec ndovu-nats-1 nats stream info NDOVU

# Ver mensagens pendentes
docker exec ndovu-nats-1 nats stream ls
```

### A.3. Documentação relacionada

- **[EXECUTIVA.md](EXECUTIVA.md)** — visão de valor.
- **[FUNCIONAL.md](FUNCIONAL.md)** — funcionalidades.
- **[ARQUITETURA.md](ARQUITETURA.md)** — decisões de design.
- **[CONTRACT.md](CONTRACT.md)** — spec do contrato v1.
- **[INTEGRATION.md](INTEGRATION.md)** — walkthrough de integração.
- **Swagger UI:** http://localhost:18081/docs (spec OpenAPI live).

---

Documento vivo. Última atualização: agosto/2026.
Cobre o produto após a Fase 4 concluída.
