# SDK Browser (`sdk/ndovu-browser.js`)

## Para que serve

É a biblioteca oficial que o frontend inclui para instrumentar
automaticamente o próprio produto — captura page views, cliques,
chamadas fetch, erros globais, Web Vitals e snapshots do DOM. Também
monta o widget de feedback do usuário final.

Zero dependências, publicado como ESM. Implementa o contrato v1
(`docs/CONTRACT.md`) — se preferir outra tecnologia, envie o mesmo
JSON e você está integrado.

## Onde fica

- Fonte: `sdk/ndovu-browser.js` (SDK_VERSION `1.2.0`).
- Endpoint destino: `POST /v1/events`, `POST /v1/snapshots`,
  `POST /v1/feedbacks` (todos com `X-Api-Key`).
- Contrato do envelope: `docs/CONTRACT.md` §1.2 e §1.3.
- Env do backend que impactam o SDK:
  - `NDOVU_MAX_BODY_BYTES` (default 1 MB) — se estourar, corte
    `maxBatch`.
  - `NDOVU_INGEST_RATE_RPS` (default 50) — o SDK **não** faz backoff
    automático em 429; ajuste o rate no servidor ou reduza batch.
  - `NDOVU_CORS_ORIGINS` — o browser precisa que a API libere o
    domínio da SPA.

## Como usar

Instalação — copie o arquivo para o seu bundle, ou aponte via CDN
interna. Exemplo em SPA React:

```ts
// src/telemetry.ts
import { createNdovu } from './vendor/ndovu-browser.js';

export const ndovu = createNdovu({
  endpoint: 'https://ndovu.suaempresa.com',
  apiKey: import.meta.env.VITE_NDOVU_KEY, // uma por app
  app: 'portal-cliente',
  getUserId: () => window.__currentUserId ?? null,
  release: import.meta.env.VITE_APP_VERSION, // habilita release tracking
  captureGlobals: true,     // window.onerror, unhandledrejection, console.error
  captureWebVitals: true,   // LCP, CLS, INP, FID, TTFB, FCP
  breadcrumbs: true,        // ring buffer anexado a cada erro
  captureSnapshots: true,   // POST /v1/snapshots em cada error()
});

ndovu.instrumentFetch();               // captura todas as chamadas fetch
ndovu.mountFeedbackWidget();           // botão flutuante de feedback
```

Uso ao longo do app:

```ts
ndovu.pageView('/faturas');
ndovu.action('clicou_segunda_via', { feature: 'faturas' });
ndovu.error('falha_pagamento', {
  code: 'PAYMENT_TIMEOUT',
  message: 'gateway não respondeu em 8s',
  feature: 'checkout',
});
ndovu.custom('conversao', { valor: 189.9 });
```

Em Next.js App Router — inicialize no client component raiz
(`app/layout.tsx` com um `<TelemetryProvider>` `use client`), passando
`getUserId` que lê o contexto de auth.

## O que acontece

- **Envelope**: SDK monta `{ app, sdkVersion, session:{sessionId,
  userId, userAgent, attributes}, events:[...] }`. `sessionId` fica
  em `sessionStorage` — sobrevive a reloads da mesma aba.
- **Batching**: enfileira `push()` e faz flush a cada
  `flushIntervalMs` (5s default) ou quando atinge `maxBatch`
  (20 default). No `pagehide` chama `flush(true)` com
  `fetch keepalive: true` (equivalente ao `sendBeacon` porém enviando
  header `X-Api-Key`, que o beacon não permite).
- **Retry idempotente**: se o `fetch` falhar, o lote volta pra fila.
  Reenvio é seguro — `eventId` (UUID por evento) dedup no ClickHouse
  + `Nats-Msg-Id` dedup na fila (janela de 2 min).
- **Redaction (PII)**: `redact()` percorre `requestBody`,
  `responseBody` e `error.body` e substitui por `***` qualquer chave
  que combine com `/pass(word)?|senha|token|secret|authorization|cvv|card|cart[aã]o/i`.
  Texto plano em `metadata`/`name` **não** é redigido.
- **Breadcrumbs**: ring buffer dos últimos 30 eventos (pageview,
  action, http_request) anexado como `metadata.breadcrumbs` em cada
  erro enviado. Ajuda a reconstruir o caminho até a falha.
- **Traceparent W3C**: `instrumentFetch()` gera `traceId/spanId`,
  adiciona `traceparent: 00-<32hex>-<16hex>-01` na requisição
  outgoing e grava os mesmos IDs no evento. Backend com OpenTelemetry
  usa o mesmo `trace_id` — frontend↔backend correlacionados sem lift.
- **Snapshot on error**: `error()` gera `eventId`, envia o evento e
  dispara `sendSnapshot()` fire-and-forget para `POST /v1/snapshots`
  com HTML sanitizado (scripts removidos, inputs sensíveis mascarados,
  elementos com `data-ndovu-mask` substituídos por `***`).
- **Widget**: `mountFeedbackWidget()` injeta um botão flutuante +
  modal (zero dependência, estilos inline) que chama
  `sdk.feedback(message, {type, email})` → `POST /v1/feedbacks`.
  Anexa o **último `eventId`** visto — deep-link para a falha que
  motivou o feedback.

## Como demonstrar

> "É um script de 700 linhas, zero dependência, que você importa e
> ganha: erros globais, Web Vitals, tracing distribuído W3C, snapshot
> visual em erro, widget de feedback e batching resiliente. Se preferir
> não usar, o mesmo JSON pode ser enviado por qualquer HTTP client."

Roteiro de 90s:
1. Abra o app do cliente em outra aba e mostre um `pageView`
   aparecendo na visão geral em 30s.
2. Rode `throw new Error('demo')` no console — o
   `installGlobalHandlers` captura, adiciona breadcrumbs e envia um
   evento `error`. Abra o issue no dashboard e mostre o **snapshot
   HTML** do DOM no momento do erro.
3. Faça um `fetch('/api/pedidos')` no console — mostre no dashboard o
   evento `http_request` com o mesmo `trace_id` que o backend usou
   (se estiver instrumentado com OTel).
4. Clique no botão "Feedback", envie um "bug" — mostre chegando em
   `/admin/feedbacks` já vinculado ao último `eventId`.

## Papéis (RBAC)

O SDK usa `X-Api-Key` — não tem role. A chave pertence a **um app**
(criada em `/admin/apps`); os três endpoints (`/v1/events`,
`/v1/snapshots`, `/v1/feedbacks`) validam que o campo `app` do body
bate com o app da chave (cross-tenant guard). Uma chave vazada não
pode "assinar" tráfego de outro frontend.

## Dependências

- Browser moderno com `fetch`, `PerformanceObserver`, `crypto.randomUUID`,
  `sessionStorage`. IE11 não suportado.
- CORS liberado no backend (`NDOVU_CORS_ORIGINS`) para o domínio do
  frontend.
- O endpoint precisa aceitar `Content-Type: application/json` +
  header `X-Api-Key`.

## Perguntas frequentes

- **"E se o usuário fechar a aba antes do flush?"** O listener
  `pagehide` chama `flush(true)` com `fetch keepalive: true` — o
  browser deixa a requisição terminar em background.
- **"Como marco um campo como sensível fora de input?"** Adicione
  `data-ndovu-mask` no elemento (`<span data-ndovu-mask>CPF</span>`).
  O snapshot substitui o `textContent` por `***`.
- **"Posso desligar o snapshot em produção?"** Sim,
  `captureSnapshots: false`. Nada mais precisa mudar no backend.
- **"Como versiono o app no evento?"** Passe `release: '1.2.3'` no
  `createNdovu`. Vai como `session.attributes.release`; o backend
  extrai automaticamente para a coluna `release` — habilita a tela
  Releases.
- **"O SDK envia PII se eu não configurar nada?"** Não faz Auto-PII
  discovery. Redige por nome de chave (senha/token/cvv/card). Você
  ainda deve não colocar CPF/RG em `metadata` — texto plano não é
  redigido.

## Referências

- SDK: `sdk/ndovu-browser.js`
- Contrato: `docs/CONTRACT.md`
- Feature relacionada: [ingest-eventos.md](ingest-eventos.md),
  [ingest-snapshots.md](ingest-snapshots.md),
  [widget-feedback.md](widget-feedback.md)
- Guia de integração por framework: `docs/USO.md`
