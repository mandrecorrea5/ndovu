# Ndovu — Guia de Integração

**Versão do documento:** 05/11/2026 · **Contrato de ingestão:** v1 · **SDKs:** browser 1.4.0 · React Native 1.1.0-rn · Node 1.2.0-node (pacote `@ndovu/sdk` 1.5.0) · Go

O Ndovu registra a jornada dos seus usuários — telas, ações, chamadas HTTP e erros — amarrada a `userId` + `sessionId`, e mostra o rastro completo de cada sessão em um dashboard. Este guia explica como enviar dados para ele.

---

## Sumário

1. [Visão geral](#1-visão-geral)
2. [O que você recebe de nós](#2-o-que-você-recebe-de-nós)
3. [Endereços](#3-endereços)
4. [Escolha o caminho de integração](#4-escolha-o-caminho-de-integração)
5. [Web — React, Vue, Angular, JS puro](#5-web--react-vue-angular-js-puro)
6. [Next.js (App Router)](#6-nextjs-app-router)
7. [React Native](#7-react-native)
8. [Backend Node.js](#8-backend-nodejs)
9. [Backend Go](#9-backend-go)
10. [Backend em qualquer linguagem (OpenTelemetry)](#10-backend-em-qualquer-linguagem-opentelemetry)
11. [Integração direta pela API (sem SDK)](#11-integração-direta-pela-api-sem-sdk)
12. [Ligando frontend e backend no mesmo trace](#12-ligando-frontend-e-backend-no-mesmo-trace)
13. [Opções de configuração](#13-opções-de-configuração)
14. [Comportamento de envio, retry e limites](#14-comportamento-de-envio-retry-e-limites)
15. [Privacidade, LGPD e consentimento](#15-privacidade-lgpd-e-consentimento)
16. [Validando a integração](#16-validando-a-integração)
17. [Problemas comuns](#17-problemas-comuns)
18. [Novidades desde a versão anterior](#18-novidades-desde-a-versão-anterior)

---

## 1. Visão geral

```
Seu frontend ──SDK──▶ api.ndovu.solutions /v1/events ─┐
Seu backend ──SDK Node/Go──▶ api.ndovu.solutions       ├─▶ Ndovu ─▶ Dashboard
Seu backend ──OpenTelemetry──▶ otlp.ndovu.solutions ───┘
```

- A ingestão responde em milissegundos (`202`) e grava de forma assíncrona. Seu app nunca espera o banco do Ndovu.
- Os SDKs não têm dependências. São conveniência: o que realmente integra é o **contrato v1** (seção 11) — qualquer cliente HTTP que envie o JSON esperado está integrado.
- Cada **app** tem a sua própria chave de API. Uma chave = um app.

## 2. O que você recebe de nós

| Item | Descrição |
|---|---|
| **Nome do app** | Identificador do seu frontend/serviço (ex.: `portal-cliente`). Vai no campo `app` e **precisa ser idêntico** ao cadastrado. |
| **Chave de API** | Formato `ndk_...`. Enviada uma única vez; guarde em local seguro. |
| **Acesso ao dashboard** | Para você conferir os eventos chegando. |
| **Arquivos do SDK** | `ndovu-browser.js`, `ndovu-next.js`, `ndovu-node.js`, `ndovu-react-native.js` (ou o pacote `@ndovu/sdk`; Go via módulo). |

**O que precisamos de você para cadastrar o app:**

- Nome desejado do app e tecnologia (React, Vue, Angular, React Native, Node, Go…).
- **Origens do site** (apps web): uma por linha, apenas protocolo + domínio, HTTPS, sem caminho nem wildcard. Ex.: `https://cliente.com` e `https://www.cliente.com`. **A chave só aceita eventos de browser vindos dessas origens** — qualquer outra recebe `403`. Se você tem ambientes de homologação em outro domínio, informe também.
- Se o app é de **servidor** e vai exportar OpenTelemetry, avise: habilitamos “aceita spans OTLP” só nesse tipo de app.

### Segurança da chave

- Em frontend web a chave fica no bundle, ou seja, é pública por natureza. Por isso ela só permite **ingestão** (nunca consulta nem administração) e fica presa ao `app` e às origens cadastradas.
- Não versione a chave no repositório; injete por variável de ambiente do build.
- Se vazar, peça a revogação: vale em até 30 s e o SDK antigo passa a receber `401`. Geramos outra.
- **Chave de servidor (Node/Go/OTLP) nunca vai para o browser.** Use um app separado para o backend.

## 3. Endereços

| Uso | URL |
|---|---|
| API de ingestão (`endpoint` dos SDKs) | `https://api.ndovu.solutions` |
| Receptor OpenTelemetry (OTLP/HTTP) | `https://otlp.ndovu.solutions` |
| Dashboard | `https://ndovu.solutions` |
| Health check | `GET https://api.ndovu.solutions/health` → `{"status":"ok"}` |
| Especificação OpenAPI / Swagger | `https://api.ndovu.solutions/openapi.yaml` · `/docs` |

> `endpoint` é a URL base, **sem barra final e sem `/v1/events`**.

## 4. Escolha o caminho de integração

| Seu cenário | Use | Seção |
|---|---|---|
| Site/SPA (React, Vue, Angular, JS) | `ndovu-browser` | 5 |
| Next.js App Router | `ndovu-browser` + `ndovu-next` | 6 |
| App mobile React Native | `ndovu-react-native` | 7 |
| API Node.js | `ndovu-node` | 8 |
| API Go | módulo `sdk/go` | 9 |
| API em Java, Python, .NET, etc. | OpenTelemetry (OTLP) | 10 |
| Outra tecnologia (desktop, TV, kiosk…) | API direta | 11 |

O ideal para ver o fluxo completo é **frontend com SDK + backend com OpenTelemetry (ou SDK Node/Go)**, ligados pelo `traceparent` (seção 12).

---

## 5. Web — React, Vue, Angular, JS puro

### 5.1 Instalar

Copie `ndovu-browser.js` para o projeto (ex.: `src/lib/ndovu-browser.js`). É um ES module puro, sem dependências. Com o pacote: `import { createNdovu } from '@ndovu/sdk/browser'`.

### 5.2 Inicializar

Crie a instância **uma vez, em escopo de módulo** (nunca dentro de um componente — o SDK não tem `destroy()`, e cada render criaria uma nova sessão, fila e timer).

```javascript
// src/lib/ndovu.js
import { createNdovu } from './ndovu-browser.js';

export const ndovu = createNdovu({
  endpoint: 'https://api.ndovu.solutions',
  apiKey:   import.meta.env.VITE_NDOVU_API_KEY,   // injete no build
  app:      'portal-cliente',                      // idêntico ao app cadastrado
  release:  import.meta.env.VITE_APP_VERSION,      // opcional: habilita release tracking
  getUserId: () => window.__CURRENT_USER__?.id ?? null,
});

// Cada fetch vira um evento http_request e envia o header traceparent
ndovu.instrumentFetch();
```

Variáveis por bundler: Vite `import.meta.env.VITE_*` · CRA `process.env.REACT_APP_*` · Next `process.env.NEXT_PUBLIC_*` · Angular `environment.ts`.

### 5.3 Rastrear navegação

**React Router**

```jsx
const location = useLocation();
useEffect(() => { ndovu.pageView(location.pathname); }, [location.pathname]);
```

**Vue Router**

```javascript
router.afterEach((to) => ndovu.pageView(to.path, { feature: to.meta?.feature }));
```

**Angular**

```typescript
this.router.events.pipe(filter(e => e instanceof NavigationEnd))
  .subscribe((e: NavigationEnd) => this.ndovu.pageView(e.urlAfterRedirects));
```

**JS puro / pushState**

```javascript
ndovu.pageView(location.pathname);
const push = history.pushState.bind(history);
history.pushState = (s, t, u) => { push(s, t, u); ndovu.pageView(location.pathname); };
addEventListener('popstate', () => ndovu.pageView(location.pathname));
```

> **Rotas dinâmicas:** `/pedido/123` e `/pedido/456` viram duas telas diferentes no dashboard. Passe o **template** da rota como nome de tela (`/pedido/[id]`) para não multiplicar a cardinalidade.

### 5.4 Erros de framework

O SDK já captura `window.onerror`, `unhandledrejection` e `console.error`. Para erros de renderização:

```jsx
// React — Error Boundary
componentDidCatch(error, info) {
  ndovu.error('react_error_boundary', {
    code: 'REACT_RENDER', message: error.message,
    body: { stack: error.stack, componentStack: info.componentStack },
  });
}
```

```javascript
// Vue
app.config.errorHandler = (err, instance, info) =>
  ndovu.error('vue_error_handler', { code: 'VUE_RENDER', message: err.message, body: { stack: err.stack, info } });
```

```typescript
// Angular — ErrorHandler customizado
handleError(error: any) {
  this.ndovu.error('angular_error_handler', { code: 'ANGULAR_ERROR', message: error.message ?? String(error), body: { stack: error.stack } });
  console.error(error);
}
```

### 5.5 Ações e eventos de negócio

```javascript
ndovu.action('clicou_segunda_via', { feature: 'faturas', metadata: { mes: '2026-10' } });
ndovu.custom('plano_alterado', { de: 'basic', para: 'premium' });
ndovu.error('falha_pagamento', { code: 'PAY_DECLINED', message: 'Cartão recusado' });
```

Para `XMLHttpRequest` ou bibliotecas que não usam `fetch`, registre manualmente com `ndovu.httpRequest(name, {...})`.

### 5.6 Sem módulos (site legado)

```bash
esbuild ndovu-browser.js --bundle --format=iife --global-name=NdovuSdk --outfile=public/js/ndovu-browser.iife.js
```

```html
<script src="/js/ndovu-browser.iife.js"></script>
<script>
  var ndovu = NdovuSdk.createNdovu({ endpoint: 'https://api.ndovu.solutions', apiKey: '...', app: 'site-legado' });
  ndovu.instrumentFetch();
  ndovu.pageView(location.pathname);
</script>
```

### 5.7 TypeScript / Angular

O SDK é JS puro. Ative `"allowJs": true` no `tsconfig.json` ou declare os tipos em um `ndovu-browser.d.ts` (`createNdovu(config): NdovuSdk` com `pageView`, `action`, `error`, `httpRequest`, `custom`, `instrumentFetch`, `feedback`, `mountFeedbackWidget`, `flush`, `sessionId`).

### 5.8 Widget de feedback (opcional)

```javascript
const unmount = ndovu.mountFeedbackWidget();   // botão flutuante; retorna função para desmontar
await ndovu.feedback('O botão de pagamento não funciona', { type: 'bug' });
```

O feedback chega atrelado à sessão e ao último evento visto.

---

## 6. Next.js (App Router)

Copie `ndovu-browser.js` e `ndovu-next.js` para `src/lib/`.

```javascript
// src/lib/ndovu.js
import { createNdovu } from './ndovu-browser.js';
export const ndovu = createNdovu({
  endpoint: 'https://api.ndovu.solutions',
  apiKey:   process.env.NEXT_PUBLIC_NDOVU_API_KEY,
  app:      'portal-next',
  release:  process.env.NEXT_PUBLIC_APP_VERSION,
});
ndovu.instrumentFetch();
```

```tsx
// app/layout.tsx
'use client';
import { NdovuProvider, NdovuAutoRouter } from '@/lib/ndovu-next.js';
import { ndovu } from '@/lib/ndovu';

const SCREENS = [
  [/^\/produto\/[^/]+$/, '/produto/[slug]'],
  [/^\/clubes\/[^/]+$/, '/clubes/[id]'],
];
const screenName = (p) => (SCREENS.find(([re]) => re.test(p)) ?? [])[1] ?? p;

export default function RootLayout({ children }) {
  return (
    <html><body>
      <NdovuProvider ndovu={ndovu}>
        <NdovuAutoRouter screenName={screenName} />
        {children}
      </NdovuProvider>
    </body></html>
  );
}
```

- `screenName` é **obrigatório na prática** em qualquer rota com ID ou slug; sem ele, o path cru vira nome de tela.
- `includeSearch` dispara um novo `pageView` a cada mudança de querystring — ligue só se a query for de fato uma tela nova.
- Em componente client: `const ndovu = useNdovu(); ndovu.action('clicou_finalizar_compra', { feature: 'checkout' });`

---

## 7. React Native

Copie `ndovu-react-native.js`. Dependências do seu app: `react-native-get-random-values` e, recomendado, `@react-native-async-storage/async-storage`.

> **`createNdovu` é assíncrono neste SDK** (lê a sessão do storage). Use `await`.

```javascript
// src/lib/ndovu.js
import 'react-native-get-random-values';          // antes de qualquer outro import
import AsyncStorage from '@react-native-async-storage/async-storage';
import { AppState, Platform } from 'react-native';
import { createNdovu } from './ndovu-react-native.js';

export const ndovu = await createNdovu({
  endpoint: 'https://api.ndovu.solutions',
  apiKey:   Config.NDOVU_API_KEY,                 // react-native-config ou equivalente
  app:      'app-mobile',
  release:  '1.4.0-build.42',
  storage:  AsyncStorage,                         // sessão persiste entre execuções
  appState: AppState,                             // flush ao ir para background
  platform: { os: Platform.OS, version: String(Platform.Version) },
  getUserId: () => global.__CURRENT_USER__?.id ?? null,
});
```

Navegação (React Navigation): chame `ndovu.pageView(routeName)` em `onReady` e em `onStateChange` quando a rota mudar. `ndovu.instrumentFetch({ ignore: [...] })` é opt-in no RN.

---

## 8. Backend Node.js

Use um **app separado** do frontend, com chave própria (Node 20+).

```javascript
import { createNdovu } from './ndovu-node.js';

export const ndovu = createNdovu({
  endpoint: 'https://api.ndovu.solutions',
  apiKey:   process.env.NDOVU_API_KEY,
  app:      'api-pedidos',
  release:  process.env.APP_VERSION,
});
ndovu.captureUncaught();            // uncaughtException / unhandledRejection

app.use(ndovu.middleware());        // Express — antes das rotas
```

- O `middleware()` lê o `traceparent` enviado pelo browser SDK: o evento `http_request` do servidor sai com o **mesmo `traceId`** e `parentSpanId` = span do fetch. Clique e request aparecem juntos.
- `action`/`error`/`custom` chamados dentro de um handler herdam trace e sessão do request automaticamente, inclusive depois de `await`.
- Chamadas de saída para outro serviço: `fetch(url, { headers: { traceparent: ndovu.traceparent() } })`.
- Sem header `traceparent` (ou inválido), abre-se um trace novo.

---

## 9. Backend Go

```bash
go get github.com/mandrecorrea5/ndovu/sdk/go
```

```go
client, err := ndovu.New(ndovu.Config{
    Endpoint:       "https://api.ndovu.solutions",
    APIKey:         os.Getenv("NDOVU_API_KEY"),
    App:            "api-pedidos",
    Release:        "1.2.3",
    FlushInterval:  5 * time.Second,
    RequestTimeout: 10 * time.Second,
    MaxBatch:       20,
    OnError:        func(err error) { log.Printf("ndovu: %v", err) },
})
if err != nil { log.Fatal(err) }

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
defer client.Close(ctx)   // flush final no encerramento gracioso

client.Action("pedido_criado", ndovu.EventOptions{
    Feature:  "pedidos",
    Metadata: map[string]any{"orderId": "ord-123"},
})
```

Métodos: `PageView`, `Action`, `Custom`, `Error`, `HTTPRequest`, `Flush`, `Close`. `EventOptions.Session` associa o evento a uma sessão/usuário específico; sem ele, usa-se uma sessão por processo. **Chame `Close` no shutdown** — é ele que envia o que sobrou na fila.

---

## 10. Backend em qualquer linguagem (OpenTelemetry)

Para ver **dentro** do backend (handler, SQL, chamada a outro serviço, fila) em Java, Python, .NET, Go, Node, etc., exporte OpenTelemetry direto para o Ndovu — não há SDK nosso nesse caso, é o exporter OTLP padrão.

1. Peça um app de servidor com **“aceita spans OTLP”** habilitado (nunca no app de browser).
2. Configure as variáveis padrão do OTel:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp.ndovu.solutions
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_HEADERS=x-api-key=ndk_...        # chave do app de servidor
OTEL_SERVICE_NAME=api-pedidos
OTEL_TRACES_SAMPLER=parentbased_traceidratio        # recomendado
OTEL_TRACES_SAMPLER_ARG=0.25
```

**gRPC não é aceito** — use `http/protobuf` (ou `http/json`). Só traces são recebidos.

| | |
|---|---|
| Redação automática | atributos com `password`, `token`, `secret`, `authorization`, `cookie`, `api-key`, `card`… viram `***`; query string é removida de `url.full`/`http.url`/`http.target` |
| Sampling | decidido por trace (o trace inteiro fica ou sai); trace com erro no mesmo lote sempre fica |
| Cota | 500 mil spans/dia por app. Acima disso, o app grava só 10% dos traces (erros continuam) até a meia-noite de Brasília |
| Retenção | 30 dias |
| LGPD | o esquecimento de um usuário apaga também os spans com `enduser.id` dele |

**Respostas do receptor:** `403` = app sem OTLP habilitado · `429`/`503` = o exporter reenvia sozinho (`Retry-After`) · `200` com `partial_success` = alguns spans recusados (sem ids, ou `startTime` fora da retenção — confira o relógio do servidor).

---

## 11. Integração direta pela API (sem SDK)

Todos os endpoints usam `X-Api-Key: ndk_...` e `Content-Type: application/json`.

### 11.1 `POST /v1/events`

Corpo: um envelope de sessão + lista de eventos (lote).

```json
{
  "app": "portal-cliente",
  "sdkVersion": "custom-1.0",
  "session": {
    "sessionId": "b7f9c2e0-4d1a-4f3b-9c8d-2e5a7b1c3d4f",
    "userId": "user-12345",
    "userAgent": "Mozilla/5.0 ...",
    "attributes": { "plano": "premium", "release": "1.2.3" }
  },
  "events": [{
    "eventId": "0d9e0a44-8c2b-4c33-a1f2-7e6b5d4c3b2a",
    "type": "http_request",
    "name": "gerar_fatura",
    "feature": "faturas",
    "screen": "/faturas/segunda-via",
    "timestamp": "2026-10-31T14:32:11.482-03:00",
    "durationMs": 843,
    "http": {
      "method": "POST", "url": "/api/v2/faturas/segunda-via", "statusCode": 200,
      "requestBody": { "contratoId": "789" }, "responseBody": { "faturaId": "F-789" }
    },
    "error": null,
    "trace": { "traceId": "0af7651916cd43dd8448eb211c80319c", "spanId": "b7ad6b7169203331", "parentSpanId": null },
    "metadata": { "tentativa": 1 }
  }]
}
```

**Envelope**

| Campo | Obrig. | Descrição |
|---|---|---|
| `app` | sim | Deve ser idêntico ao app da chave |
| `sdkVersion` | não | Versão da sua instrumentação (gravada em `session.attributes.sdkVersion`) |
| `session.sessionId` | sim | UUID único da sessão |
| `session.userId` | não | Recomendado; eventos anônimos são aceitos |
| `session.userAgent` | não | User agent / device |
| `session.attributes` | não | Metadados livres. `attributes.release` habilita release tracking |

**Evento**

| Campo | Obrig. | Descrição |
|---|---|---|
| `eventId` | sim | UUID do evento — **chave de idempotência** |
| `type` | sim | `page_view` · `action` · `http_request` · `error` · `custom` |
| `name` | sim | Nome de negócio (`login_success`, `gerar_fatura`) |
| `feature` | não | Agrupador funcional |
| `screen` | não | Tela/rota (use o template, não a URL com ids) |
| `timestamp` | sim | RFC 3339 **com fuso**, relógio do cliente |
| `durationMs` | não | Duração em ms |
| `http` | não | `method`, `url` (path, sem host nem query com PII), `statusCode`, `requestBody`, `responseBody` |
| `error` | não | `code`, `message`, `body` (stack ou response body) |
| `trace` | não | `traceId` (32 hex), `spanId` (16 hex), `parentSpanId` opcional — correlação W3C |
| `metadata` | não | JSON livre |

**Respostas**

| Status | Significado |
|---|---|
| `202` `{"accepted": N}` | Lote validado e enfileirado |
| `400` | Contrato violado (`details[]` aponta evento e campo) ou `app` ≠ app da chave |
| `401` | Chave ausente, inválida ou revogada |
| `403` | Origem do browser não autorizada para esta chave |
| `413` | Lote acima de 1 MB |
| `429` | Rate limit (50 req/s por chave); header `Retry-After` |

### 11.2 `POST /v1/snapshots` (opcional — session replay)

HTML da tela no momento de um erro. Campos: `eventId` (de um evento `error` já enviado), `sessionId`, `app`, `html` (**sanitizado por você**), `url`, `viewportW`, `viewportH`, `takenAt`. Aceita `application/gzip`. Limite próprio maior que o da ingestão de eventos. Antes de enviar: remova `<script>` e `<iframe>`, mascare `value` de inputs sensíveis (`password`, `email`, `tel`, `cc-number`, `cc-csc`) e o texto de elementos com `data-ndovu-mask`. Resposta `200`.

### 11.3 `POST /v1/feedbacks` (opcional)

`app`, `sessionId`, `type` (`bug` · `suggestion` · `praise` · `other`), `message` (máx. 5000 caracteres) obrigatórios; `eventId`, `email`, `url`, `viewportW`, `viewportH` opcionais. Resposta `200` com `id`, `status: "new"`, `createdAt`.

### 11.4 Regras gerais

- **Idempotência:** reenviar o mesmo `eventId` (ou o lote inteiro) não duplica. Retry ingênuo é seguro.
- **Tolerância:** campos desconhecidos são ignorados; campos opcionais ausentes usam default. Evolução incompatível só por versão de path (`/v2`).
- **Sampling no servidor:** regras configuradas por nós podem descartar eventos aceitos (erros podem ser preservados a 100%). É transparente para o cliente.
- **Imutabilidade:** a ingestão só insere; não existe update/delete público.

---

## 12. Ligando frontend e backend no mesmo trace

O `ndovu.instrumentFetch()` do browser gera `traceId`/`spanId` por request e envia o header W3C `traceparent` (`00-<traceId>-<spanId>-01`). O backend instrumentado (SDK Node/Go via `traceparent`, ou OpenTelemetry) continua o mesmo trace. No dashboard, o evento ganha o link **trace**, com a cascata dos dois lados.

**Atenção a CORS no seu backend.** Se o frontend e a API ficam em origens diferentes, a API precisa permitir o header `traceparent` (`Access-Control-Allow-Headers: ..., traceparent`), senão o browser bloqueia a requisição no preflight. Para desligar a propagação: `instrumentFetch({ propagateTraceparent: false })`.

## 13. Opções de configuração

Browser (`createNdovu`):

| Opção | Default | Descrição |
|---|---|---|
| `endpoint` | — | **Obrigatório.** URL base, sem barra final |
| `apiKey` | — | **Obrigatório.** Chave do app |
| `app` | — | **Obrigatório.** Nome do app (idêntico ao cadastrado) |
| `getUserId` | `() => null` | Retorna o userId atual, avaliado a cada envio |
| `release` | — | Versão do app (release tracking) |
| `sessionAttributes` | `{}` | Metadados livres da sessão |
| `flushIntervalMs` | `5000` | Intervalo de envio do lote |
| `maxBatch` | `20` | Envia antes do intervalo se acumular N eventos |
| `captureGlobals` | `true` | `window.onerror`, `unhandledrejection`, `console.error` |
| `captureWebVitals` | `true` | LCP, CLS, INP, FID, TTFB, FCP |
| `breadcrumbs` | `true` | Últimos 30 eventos anexados a cada erro |
| `captureSnapshots` | `true` | `true`/`'error'`: DOM em cada `error()`; `'screen'`: também em cada `pageView()`; `false`/`'off'`: desliga |

`instrumentFetch({ nameFor, ignore, propagateTraceparent })` — por padrão ignora o próprio endpoint do Ndovu. Se sobrescrever `ignore`, inclua o endpoint para não gerar loop.

React Native: mesmas opções (sem `captureWebVitals`/`captureSnapshots`) mais `storage`, `appState`, `platform`. Node: `endpoint`, `apiKey`, `app`, `flushIntervalMs`, `maxBatch`, `release`, `serviceInstance`.

**Menos volume:** `captureWebVitals: false, breadcrumbs: false, captureSnapshots: false`.
**Entrega mais rápida:** `flushIntervalMs: 1000, maxBatch: 5` (cuidado com o rate limit).

## 14. Comportamento de envio, retry e limites

Como os SDKs (browser e React Native 1.3+) reagem à resposta da ingestão:

| Resposta | Comportamento |
|---|---|
| 2xx | Enviado |
| 400, 401, 403, 404, 413 | **Lote descartado** + um `console.warn` com a causa provável. É erro de configuração ou contrato — reenviar não resolve, e o lote recusado não volta |
| 429, 5xx | Volta para a fila com o mesmo `eventId` (sem duplicar) |
| Erro de rede | Volta para a fila |

- A fila de reenvio tem **teto de 400 eventos**; acima disso descarta os mais antigos (com aviso). O SDK nunca deve ser a causa de queda do seu app.
- O aviso sai uma única vez por causa, no `console.warn`.
- **Corrigiu uma chave errada?** Eventos já descartados não são reprocessados.

| Limite | Valor |
|---|---|
| Payload por request (`/v1/events`) | 1 MB |
| Rate limit por chave | 50 req/s |
| Atraso típico até aparecer no dashboard | 1–5 s |
| Cache de validação de chave (revogação) | até 30 s |
| Retenção de eventos | 90 dias |
| Retenção de spans OTLP | 30 dias |

## 15. Privacidade, LGPD e consentimento

O Ndovu coleta dados de jornada que **podem** conter dados pessoais. O consentimento do usuário final é responsabilidade da sua aplicação.

**Inicialize só após o consentimento** (banner de cookies):

```javascript
let ndovu = null;
export const getNdovu = () => ndovu;
export function onConsent() {
  ndovu ??= createNdovu({ endpoint: 'https://api.ndovu.solutions', apiKey: KEY, app: 'portal-cliente' });
  ndovu.instrumentFetch();
  ndovu.pageView(location.pathname);
}
// nos pontos de uso:  getNdovu()?.action('clicou_algo', { feature: 'x' });
```

**Redação automática no cliente**, antes do envio: valores de chaves que casem com `/pass(word)?|senha|token|secret|authorization|cvv|card|cart[aã]o/i` viram `***`, recursivamente em `requestBody`, `responseBody` e `error.body`.

**Cuidados**

- A redação só vale para objetos com **chaves nominais**. Não coloque dados sensíveis em texto livre, em `metadata` solto nem no `name` do evento (errado: `login_cpf_123...`; certo: `name: "login_success"`).
- No snapshot do DOM, marque PII fora de inputs com `data-ndovu-mask`: `<span data-ndovu-mask>{{ user.cpf }}</span>`.
- Envie `userId` só se o usuário consentiu com identificação; sem ele, a sessão continua correlacionável.
- Direito ao esquecimento: o Ndovu apaga os dados de um usuário sob solicitação (eventos e spans com `enduser.id`). Fale com a gente.

## 16. Validando a integração

1. **Saúde da API:** `curl https://api.ndovu.solutions/health` → `{"status":"ok"}`.
2. **Chave e app:** lote vazio deve retornar `{"accepted":0}`:

```bash
curl -sX POST https://api.ndovu.solutions/v1/events \
  -H "X-Api-Key: $NDOVU_API_KEY" -H "Content-Type: application/json" \
  -d '{"app":"<SEU_APP>","session":{"sessionId":"smoke-1"},"events":[]}'
```

3. **Evento de teste:**

```bash
curl -sX POST https://api.ndovu.solutions/v1/events \
  -H "X-Api-Key: $NDOVU_API_KEY" -H "Content-Type: application/json" \
  -d '{"app":"<SEU_APP>","session":{"sessionId":"smoke-1"},"events":[{
        "eventId":"'$(uuidgen | tr A-Z a-z)'","type":"action","name":"smoke_test",
        "timestamp":"'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"}]}'
# => {"accepted":1}
```

   (O curl não envia `Origin`, então não passa pela checagem de origens do browser.)

4. **No browser:** DevTools → Network, filtre `/v1/events`. A cada 5 s (ou 20 eventos) deve haver um `POST` com `202`.
5. **No dashboard:** Explorador → filtre `app = <seu app>` e “última 1 hora”.
6. **OTLP:** o trace do backend aparece no waterfall do evento que o originou (link *trace*).

## 17. Problemas comuns

| Sintoma | Causa provável | Solução |
|---|---|---|
| `401` | Chave ausente, inválida ou revogada | Confira o header `X-Api-Key` e peça nova chave se revogada |
| `400` (divergência de app) | `app` do envelope ≠ app da chave | Use exatamente o nome cadastrado (atenção a maiúsculas e hífens) |
| `400` com `details[]` | Contrato violado: `eventId` não-UUID, `type` inválido, `timestamp` ausente/sem fuso | Leia `details[]`; ele indica evento e campo |
| `403` “origem não autorizada para esta chave” | Site rodando em domínio não cadastrado (inclui `localhost` e homologação) | Peça para adicionarmos a origem exata (protocolo + domínio, sem barra final) |
| Erro de CORS no preflight para sua API | Backend não aceita o header `traceparent` | Libere `traceparent` em `Access-Control-Allow-Headers` ou desligue `propagateTraceparent` |
| `413` | Lote > 1 MB | Reduza `maxBatch`; evite bodies enormes em `metadata` |
| `429` | > 50 req/s na chave | Aumente `flushIntervalMs` ou `maxBatch` |
| OTLP `403` | App sem “aceita spans OTLP” | Peça a habilitação |
| OTLP: spans recusados (`partial_success`) | Spans sem ids ou `startTime` fora da retenção | Corrija o relógio do servidor |
| `fetch` não é capturado | Chamada via `XMLHttpRequest` ou lib sem `fetch` | Use `ndovu.httpRequest(...)` |
| Loop de eventos | `ignore` sobrescrito sem o endpoint do Ndovu | Inclua o endpoint no `ignore` |
| Eventos não aparecem | Delay (1–5 s), filtro errado ou lote descartado | Veja o `console.warn` do SDK e o status do `POST` na aba Network |
| React Native: `ndovu` é `Promise` | Faltou `await` em `createNdovu` | `await createNdovu(...)` |
| Tela duplicada por id na URL | Rota dinâmica sem template | Passe `screenName` (Next) ou o template na chamada de `pageView` |

## 18. Novidades desde a versão anterior

- **Domínios de produção**: API em `api.ndovu.solutions`, OTLP em `otlp.ndovu.solutions`.
- **Origens permitidas por app (obrigatório no browser):** a chave só aceita eventos vindos das origens cadastradas; outras recebem `403`.
- **SDK browser/RN 1.3.0:** o envio deixa de descartar lote em silêncio — avisa no console, reenvia 5xx/429 e limita a fila a 400 eventos. **4xx agora é descarte explícito.**
- **SDK Node 1.2.0 (pacote 1.4.0):** o middleware entra no mesmo trace do browser via `traceparent`; eventos manuais herdam trace e sessão do request.
- **SDK Go** disponível.
- **Receptor OpenTelemetry (OTLP/HTTP)**: spans do backend em qualquer linguagem, com trace unificado no dashboard.
- **`sdkVersion`** do envelope passa a ser gravado em `session.attributes.sdkVersion`.
- **Next.js:** `screenName` passa a ser tratado como obrigatório para rotas dinâmicas.
- Os arquivos do SDK agora ficam em `sdk/src/` (antes `sdk/`).

---

Dúvidas ou pedidos (nova chave, novas origens, habilitar OTLP, esquecimento de usuário): fale com o time Ndovu.
