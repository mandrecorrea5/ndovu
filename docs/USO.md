# Ndovu — Guia de Uso

Guia prático de **como usar** o Ndovu SDK depois de instalado — quando
capturar cada tipo de evento, com que nomes, como estruturar código para
manter o dashboard útil.

Este documento assume que você já fez a integração descrita em
[`INTEGRATION.md`](INTEGRATION.md). Aqui o foco é **padrões de uso
idiomáticos** para cada tecnologia (React JS, React Native, Vue JS,
Angular, JavaScript Vanilla).

---

## Sumário

1. [Tipos de evento — quando usar cada um](#1-tipos-de-evento--quando-usar-cada-um)
2. [Convenções de nomenclatura](#2-convenções-de-nomenclatura)
3. [Padrão A — Rastrear navegação (pageView)](#3-padrão-a--rastrear-navegação-pageview)
4. [Padrão B — Rastrear ações de negócio (action)](#4-padrão-b--rastrear-ações-de-negócio-action)
5. [Padrão C — Rastrear erros (error)](#5-padrão-c--rastrear-erros-error)
6. [Padrão D — Rastrear chamadas HTTP (http_request)](#6-padrão-d--rastrear-chamadas-http-http_request)
7. [Padrão E — Eventos customizados (custom)](#7-padrão-e--eventos-customizados-custom)
8. [Padrão F — Widget de feedback do usuário](#8-padrão-f--widget-de-feedback-do-usuário)
9. [Padrão G — Correlação com backend (OpenTelemetry)](#9-padrão-g--correlação-com-backend-opentelemetry)
10. [Padrão H — Consentimento LGPD](#10-padrão-h--consentimento-lgpd)
11. [Padrão I — Contexto de usuário e sessão](#11-padrão-i--contexto-de-usuário-e-sessão)
12. [Padrão J — Release tracking](#12-padrão-j--release-tracking)
13. [Padrão K — Instrumentação de bibliotecas específicas (axios, XHR, Apollo)](#13-padrão-k--instrumentação-de-bibliotecas-específicas)
14. [Anti-padrões (o que evitar)](#14-anti-padrões-o-que-evitar)
15. [Checklist de qualidade da instrumentação](#15-checklist-de-qualidade-da-instrumentação)

---

## 1. Tipos de evento — quando usar cada um

O SDK tem **5 tipos** de evento. Use cada um para o propósito certo — isso
mantém o dashboard organizado e as queries eficientes.

| Tipo | Quando usar | Exemplo de `name` |
|---|---|---|
| `page_view` | Abertura de tela/rota | `tela_/faturas`, `tela_home` |
| `action` | Ação de negócio do usuário (clique importante) | `login_success`, `clicou_segunda_via`, `boleto_gerado` |
| `http_request` | Chamada a uma API (sucesso ou erro) — **prefira auto-captura via `instrumentFetch()`** | `GET /faturas`, `POST /pagamento` |
| `error` | Falha (de negócio, rede, exceção JS) | `login_failed`, `network_error`, `js_error` |
| `custom` | Métrica ou evento que não se encaixa | `feature_flag_ativada`, `experimento_ab_atribuido` |

### Regra prática

- **`page_view`**: 1 por tela visitada. Nunca mais que isso na mesma tela.
- **`action`**: para o que **importa medir depois** (conversão, funil,
  engajamento). Não capture cada micro-clique.
- **`http_request`**: deixe o `instrumentFetch()` fazer. Use manual só
  para chamadas via lib que não é fetch.
- **`error`**: automático via `captureGlobals: true`. Use `sdk.error()`
  manual para erros de negócio (não-exceção).
- **`custom`**: última escolha. Use quando nenhum dos outros couber.

---

## 2. Convenções de nomenclatura

O que você põe em `name`, `feature` e `screen` **determina** a qualidade
do dashboard. Convenções ruins → dashboard poluído e difícil de filtrar.

### 2.1 `name`

- **Formato:** `snake_case`, verbo + substantivo.
- **Estável:** não mude a cada refactor. Se mudar, perde histórico
  comparável.
- **Específico:** `login_success` (bom) vs. `success` (ruim).
- **Sem IDs dinâmicos:** use `boleto_gerado` + `metadata: {boletoId: X}`,
  **não** `boleto_12345_gerado`.

Exemplos bons: `login_success`, `login_failed`, `checkout_iniciado`,
`checkout_finalizado`, `carrinho_atualizado`, `filtro_aplicado`,
`export_csv_solicitado`.

### 2.2 `feature`

- Agrupador funcional **estável**. Use o mesmo em toda a área.
- Exemplos: `login`, `faturas`, `checkout`, `carrinho`, `perfil`,
  `busca`, `relatorios`.
- Uma feature deve alinhar com um "time responsável" — facilita RBAC
  granular depois.

### 2.3 `screen`

- Rota/tela atual, no formato do seu router.
- SPA: `/faturas`, `/checkout/pagamento`, `/perfil/senha`.
- **Normalize IDs**: `/atletas/:id/editar` (bom) vs. `/atletas/abc-123/editar`
  (ruim — polui o dashboard com um "screen" por atleta).

### 2.4 `metadata`

- JSON livre para contexto que você quer **filtrar/analisar depois**.
- Mantenha pequeno (< 5 chaves normalmente).
- **Sem PII em texto plano** — a redação automática só cobre chaves
  nominais.

Bom: `metadata: { atletaId: '123', modalidade: 'RUN', formato: 'PDF' }`.
Ruim: `metadata: { descricaoCompleta: 'Fulano de Tal, CPF 999...' }`.

---

## 3. Padrão A — Rastrear navegação (pageView)

Regra: **um `pageView` por mudança de rota**. Nunca deixe a tela sem
`pageView` (senão os eventos subsequentes ficam órfãos).

### 3.1 React JS (React Router)

```jsx
import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { ndovu } from '@/lib/ndovu';

function useNdovuPageview() {
  const location = useLocation();
  useEffect(() => {
    ndovu.pageView(location.pathname, {
      feature: featureFromPath(location.pathname),  // opcional
    });
  }, [location.pathname]);
}

// Chame no App:
function App() {
  useNdovuPageview();
  return <Routes />;
}

function featureFromPath(path) {
  if (path.startsWith('/faturas')) return 'faturas';
  if (path.startsWith('/checkout')) return 'checkout';
  return undefined;
}
```

### 3.2 React Native (React Navigation)

```jsx
<NavigationContainer
  onStateChange={() => {
    const current = navigationRef.current.getCurrentRoute();
    ndovu.pageView(current.name, {
      feature: current.params?.feature,
      metadata: current.params,
    });
  }}
>
```

### 3.3 Vue JS (Vue Router)

```javascript
router.afterEach((to) => {
  ndovu.pageView(to.path, {
    feature: to.meta?.feature,
  });
});

// Nas rotas, marque a feature no meta:
{
  path: '/faturas',
  component: FaturasView,
  meta: { feature: 'faturas' },
}
```

### 3.4 Angular

```typescript
this.router.events
  .pipe(filter(e => e instanceof NavigationEnd))
  .subscribe((e: NavigationEnd) => {
    this.ndovu.pageView(e.urlAfterRedirects, {
      feature: this.featureFromUrl(e.urlAfterRedirects),
    });
  });

private featureFromUrl(url: string): string | undefined {
  if (url.startsWith('/faturas')) return 'faturas';
  if (url.startsWith('/checkout')) return 'checkout';
  return undefined;
}
```

### 3.5 JavaScript Vanilla

Se usa `history.pushState`:

```javascript
const push = history.pushState.bind(history);
history.pushState = function (state, title, url) {
  push(state, title, url);
  ndovu.pageView(location.pathname);
};
window.addEventListener('popstate', () => ndovu.pageView(location.pathname));

// Primeira carga
ndovu.pageView(location.pathname);
```

Se é MPA (sem SPA router), basta chamar no load:

```html
<script type="module">
  import { ndovu } from '/js/ndovu.js';
  ndovu.pageView(location.pathname);
</script>
```

---

## 4. Padrão B — Rastrear ações de negócio (action)

Regra: **para o que você quer medir depois** (conversão, funil,
engajamento).

### 4.1 React JS

```jsx
import { ndovu } from '@/lib/ndovu';

function BotaoFinalizarCompra({ carrinho }) {
  const handleClick = async () => {
    ndovu.action('clicou_finalizar_compra', {
      feature: 'checkout',
      screen: '/checkout',
      metadata: { itens: carrinho.length, valor: carrinho.total },
    });
    await finalizarPedido(carrinho);
    ndovu.action('pedido_finalizado', {
      feature: 'checkout',
      metadata: { valor: carrinho.total },
    });
  };
  return <button onClick={handleClick}>Finalizar compra</button>;
}
```

### 4.2 React Native

```jsx
<Pressable
  onPress={() => {
    ndovu.action('clicou_gerar_boleto', {
      feature: 'boletos',
      screen: 'BoletosScreen',
    });
    navigation.navigate('GerarBoleto');
  }}
>
  <Text>Gerar boleto</Text>
</Pressable>
```

### 4.3 Vue JS

Composables ficam elegantes:

```javascript
// src/composables/useNdovu.js
import { inject } from 'vue';
export function useNdovu() { return inject('ndovu'); }
```

```vue
<script setup>
import { useNdovu } from '@/composables/useNdovu';
const ndovu = useNdovu();

function finalizar() {
  ndovu.action('pedido_finalizado', {
    feature: 'checkout',
    metadata: { valor: total.value },
  });
}
</script>

<template>
  <button @click="finalizar">Finalizar</button>
</template>
```

### 4.4 Angular

```typescript
@Component({...})
export class CheckoutComponent {
  constructor(private ndovu: NdovuService) {}

  finalizar() {
    this.ndovu.action('pedido_finalizado', {
      feature: 'checkout',
      metadata: { valor: this.total },
    });
  }
}
```

### 4.5 JavaScript Vanilla

```javascript
document.querySelector('#btn-checkout').addEventListener('click', () => {
  ndovu.action('clicou_finalizar_compra', {
    feature: 'checkout',
    screen: location.pathname,
  });
});
```

### 4.6 Padrão "capture o resultado, não só o clique"

Prefira 2 eventos (intenção + resultado) para features críticas:

```javascript
// Intenção
ndovu.action('clicou_finalizar_compra', {feature: 'checkout'});

try {
  const pedido = await finalizarPedido();
  // Resultado (sucesso)
  ndovu.action('pedido_finalizado', {
    feature: 'checkout',
    metadata: {pedidoId: pedido.id, valor: pedido.total},
  });
} catch (e) {
  // Resultado (falha)
  ndovu.error('pedido_falhou', {
    feature: 'checkout',
    code: 'CHECKOUT_ERROR',
    message: e.message,
    body: {stack: e.stack},
  });
}
```

Isso alimenta funis: `clicou_finalizar_compra → pedido_finalizado` mostra
conversão real.

---

## 5. Padrão C — Rastrear erros (error)

### 5.1 Automático (recomendado)

Se `captureGlobals: true` (default), o SDK captura automaticamente:

- `window.onerror` (erros JS não-tratados)
- `unhandledrejection` (Promises rejeitadas sem catch)
- `console.error` (chamadas explícitas)

Você **não precisa fazer nada**. Vão aparecer no dashboard como:

- `code: JS_ERROR` — window.onerror
- `code: UNHANDLED_REJECTION` — promise rejection
- `code: CONSOLE_ERROR` — console.error

Cada um vem com `stack`, `source`, `line`, `column`.

### 5.2 Manual — erros de negócio

Para falhas que **não** são exceção JS (ex.: backend retornou erro de
negócio), use `sdk.error()` explícito:

```javascript
try {
  const fatura = await api.gerarFatura(contratoId);
  if (fatura.error) {
    ndovu.error('gerar_fatura_negocio_falhou', {
      code: fatura.error.code,          // ex.: 'FATURA_INDISPONIVEL'
      message: fatura.error.message,
      feature: 'faturas',
      screen: '/faturas/segunda-via',
      body: fatura.error,               // payload completo
    });
    return;
  }
  // sucesso...
} catch (e) {
  ndovu.error('gerar_fatura_exception', {
    code: 'NETWORK_ERROR',
    message: e.message,
    feature: 'faturas',
    body: {stack: e.stack},
  });
}
```

### 5.3 React JS — Error Boundary

Ver [`INTEGRATION.md` §4.4](INTEGRATION.md#44-error-boundary-opcional-mas-recomendado).

### 5.4 React Native — Error Utils

`ndovu.captureUncaught()` (chamado na init) instala automaticamente. Não
precisa fazer nada.

### 5.5 Vue JS — errorHandler

`app.config.errorHandler` (ver [`INTEGRATION.md` §6.5](INTEGRATION.md#65-error-handler-global)).

### 5.6 Angular — ErrorHandler

Custom `ErrorHandler` (ver
[`INTEGRATION.md` §7.5](INTEGRATION.md#75-error-handler-global)).

### 5.7 JavaScript Vanilla

`captureGlobals: true` já cuida. Se quiser adicionar mais:

```javascript
// Fetch API que retorna 5xx (não gera exception por padrão)
async function apiCall(url) {
  const r = await fetch(url);
  if (r.status >= 500) {
    ndovu.error('api_5xx', {
      code: `HTTP_${r.status}`,
      message: r.statusText,
      body: {url, status: r.status},
    });
  }
  return r;
}
```

### 5.8 Session replay em erros

Com `captureSnapshots: true` (default), **todo** `sdk.error()` dispara
uma captura do DOM. Quando o admin abrir a issue no dashboard, vê a
foto exata da tela.

---

## 6. Padrão D — Rastrear chamadas HTTP (http_request)

### 6.1 Automático (recomendado)

```javascript
ndovu.instrumentFetch();
```

Isso substitui `window.fetch` por um wrapper que captura **cada** chamada
como `http_request` com método, URL, status, request/response body, duração
e (opcionalmente) `trace_id`/`span_id`.

Cobre 100% do que passa por `fetch`. Sem código no app.

### 6.2 Configurar auto-captura

```javascript
ndovu.instrumentFetch({
  // Como derivar o `name` da URL. Default: `u => u.split('?')[0]`
  nameFor: (url) => {
    if (url.includes('/atletas/')) return url.replace(/\/atletas\/[^/]+/, '/atletas/:id');
    if (url.includes('/faturas/')) return url.replace(/\/faturas\/[^/]+/, '/faturas/:id');
    return url.split('?')[0];
  },
  // URLs a ignorar. Default: [endpoint do Ndovu]
  ignore: [
    'https://ndovu.suaempresa.com',
    'https://analytics.example.com',
    '/health',
  ],
  // Se deve propagar W3C traceparent no header outgoing. Default: true
  propagateTraceparent: true,
});
```

### 6.3 Manual

Se sua lib HTTP não é fetch (ex.: `XMLHttpRequest` legado, `axios` com
adapter XHR), use `sdk.httpRequest()`:

```javascript
axios.interceptors.response.use(
  (response) => {
    ndovu.httpRequest(response.config.url, {
      method: response.config.method.toUpperCase(),
      url: response.config.url,
      statusCode: response.status,
      requestBody: response.config.data,
      responseBody: response.data,
      durationMs: Date.now() - response.config.__startTime,
      screen: location.pathname,
    });
    return response;
  },
  (error) => {
    ndovu.httpRequest(error.config?.url ?? 'unknown', {
      method: error.config?.method?.toUpperCase(),
      url: error.config?.url,
      statusCode: error.response?.status,
      error: {
        code: `HTTP_${error.response?.status ?? 'unknown'}`,
        message: error.message,
        body: error.response?.data,
      },
    });
    return Promise.reject(error);
  },
);

axios.interceptors.request.use((config) => {
  config.__startTime = Date.now();
  return config;
});
```

Ver seção 13 para exemplos mais completos.

---

## 7. Padrão E — Eventos customizados (custom)

Use `sdk.custom()` para métricas ou marcos que não se encaixam nos outros
tipos:

```javascript
// Marcar quando uma feature flag é atribuída
ndovu.custom('feature_flag_atribuida', {
  metadata: {flag: 'new-checkout-ui', variant: 'B'},
});

// Marcar quando um experimento A/B começa
ndovu.custom('experimento_ab_atribuido', {
  metadata: {experimento: 'homepage-hero-v2', grupo: 'tratamento'},
});

// Timing customizado
ndovu.custom('tempo_ate_conversao', {
  metadata: {segundos: 47, funil: 'onboarding-b2c'},
});
```

Aparecem no dashboard filtrando por `type = custom`.

---

## 8. Padrão F — Widget de feedback do usuário

O SDK oferece um widget flutuante ("Feedback") que o usuário final clica
para reportar problemas, sugestões ou elogios.

### 8.1 Ativar em qualquer tecnologia

Basta chamar uma vez após a instância criada:

```javascript
ndovu.mountFeedbackWidget();
```

Retorna uma função para desmontar (útil quando o widget deve sumir em
telas específicas):

```javascript
const unmount = ndovu.mountFeedbackWidget();
// Em outra tela:
unmount();
```

### 8.2 Customizar posição e labels

```javascript
ndovu.mountFeedbackWidget({
  position: 'bottom-right',      // ou 'bottom-left'
  label: 'Feedback',              // texto do botão
  title: 'Envie seu feedback',    // título do modal
  placeholder: 'Descreva o que aconteceu…',
  successMessage: 'Recebemos, obrigado!',
});
```

### 8.3 Usar programaticamente (sem widget)

```javascript
// De qualquer lugar do app
async function enviarFeedback(mensagem) {
  await ndovu.feedback(mensagem, {
    type: 'bug',                          // bug | suggestion | praise | other
    email: 'cliente@example.com',         // opcional
  });
}
```

### 8.4 React

```jsx
import { useEffect } from 'react';
import { ndovu } from '@/lib/ndovu';

function App() {
  useEffect(() => {
    const unmount = ndovu.mountFeedbackWidget({
      position: 'bottom-right',
      label: 'Falar com a gente',
    });
    return unmount;  // desmonta ao desmontar App
  }, []);

  return <Routes />;
}
```

### 8.5 Vue

```javascript
// src/App.vue
import { onMounted, onBeforeUnmount } from 'vue';
import { useNdovu } from '@/composables/useNdovu';

const ndovu = useNdovu();
let unmount;

onMounted(() => {
  unmount = ndovu.mountFeedbackWidget();
});
onBeforeUnmount(() => unmount?.());
```

### 8.6 Angular

```typescript
export class AppComponent implements OnInit, OnDestroy {
  private unmount: (() => void) | null = null;

  constructor(private ndovu: NdovuService) {}

  ngOnInit() {
    // Angular NdovuService encapsula, você pode expor um wrap:
    this.unmount = this.ndovu['ndovu'].mountFeedbackWidget();
  }
  ngOnDestroy() {
    this.unmount?.();
  }
}
```

Feedback aparece em `/admin/feedbacks` no dashboard, com deep-link para
a sessão que originou.

---

## 9. Padrão G — Correlação com backend (OpenTelemetry)

Se seu backend já é instrumentado com OpenTelemetry (Jaeger, Tempo,
Honeycomb, Datadog APM), o Ndovu SDK propaga automaticamente o
`traceparent` W3C — os traces frontend↔backend ficam correlacionados
pelo mesmo `trace_id`.

### 9.1 Ativar (default)

Só precisa deixar `propagateTraceparent: true` (default) no
`instrumentFetch`:

```javascript
ndovu.instrumentFetch({
  propagateTraceparent: true,   // default
});
```

Cada `fetch()` sai com header:
```
traceparent: 00-{traceId 32 hex}-{spanId 16 hex}-01
```

O evento `http_request` no Ndovu grava o mesmo `traceId` + `spanId` na
coluna `trace`.

### 9.2 Ver correlação no dashboard

Rota `/traces` no dashboard aceita filtro por `trace_id`. Também:

```bash
curl -s http://localhost:18081/v1/traces/{traceId} \
  -H "Authorization: Bearer $TOKEN"
```

Retorna **todos** os eventos (frontend + backend, se ambos instrumentados)
com aquele trace_id — timeline W3C completa.

### 9.3 Opt-out

Se seu backend não é instrumentado (e o header extra atrapalha), desligue:

```javascript
ndovu.instrumentFetch({ propagateTraceparent: false });
```

---

## 10. Padrão H — Consentimento LGPD

Ver [`INTEGRATION.md` §12](INTEGRATION.md#12-lgpd-e-consentimento).

Resumo: só inicialize o SDK **depois** do consentimento. Use um wrapper
`getNdovu()` que retorna `null` se não consentiu, e faça guard nos pontos
de uso:

```javascript
getNdovu()?.action('clicou_algo', { feature: 'x' });
```

Isso funciona em qualquer framework.

---

## 11. Padrão I — Contexto de usuário e sessão

### 11.1 userId

Quando o usuário faz login, o `getUserId` callback passa a retornar o id
— o SDK anexa em todos os eventos subsequentes automaticamente. Nada mais
precisa ser feito.

```javascript
// No login
window.__CURRENT_USER__ = { id: 'user-123', plano: 'gold' };

// getUserId callback já pega da global:
createNdovu({
  ..., getUserId: () => window.__CURRENT_USER__?.id ?? null,
});
```

### 11.2 Session attributes

Metadados da sessão inteira (planos, canal, versão do app). Ficam no
envelope, aparecem em toda query:

```javascript
createNdovu({
  ...,
  sessionAttributes: {
    plano: window.__CURRENT_USER__?.plano,   // avaliado uma vez
    canal: 'web-app',
    ambiente: process.env.NODE_ENV,
  },
});
```

Se um atributo muda durante a sessão, você teria que reiniciar o SDK. Em
geral, use `metadata` do evento para dados dinâmicos.

### 11.3 Anônimo antes do login

Sessões anônimas (sem `userId`) são aceitas — o `sessionId` correlaciona
os eventos. Após login, se você identifica o usuário, os eventos passam a
ter `userId` — a sessão continua a mesma, mas agora a maior parte dos
eventos vai identificada. Isso permite ver a jornada pré-login → login →
pós-login em uma única sessão.

---

## 12. Padrão J — Release tracking

Enviar a versão do app habilita:

- Coluna `release` no ClickHouse.
- Módulo Releases no dashboard (métricas por versão).
- Comparação entre releases (delta de erro_rate, latência).
- Detecção automática de regressão em issues resolvidas.

### 12.1 Configurar no SDK

```javascript
createNdovu({
  ...,
  release: '1.2.3',  // ou process.env.APP_VERSION
});
```

### 12.2 Injetar via build

**Vite:**
```javascript
// vite.config.js
export default defineConfig({
  define: {
    'import.meta.env.VITE_APP_VERSION': JSON.stringify(process.env.npm_package_version),
  },
});
```

**Webpack:**
```javascript
// webpack.config.js
plugins: [
  new DefinePlugin({
    'process.env.APP_VERSION': JSON.stringify(require('./package.json').version),
  }),
],
```

**Angular:**
```typescript
// environment.prod.ts
import packageJson from '../../package.json';
export const environment = {
  ...,
  appVersion: packageJson.version,
};
```

**Next.js:**
```javascript
// next.config.mjs
export default {
  env: {
    NEXT_PUBLIC_APP_VERSION: process.env.npm_package_version,
  },
};
```

### 12.3 Upload de source maps para produção

Se o build minifica JS (React/Vue/Angular em prod), gere e faça upload
do source map. Sem isso, stacks vêm ilegíveis (`a.b.c is not a function`).

```bash
# Após o build de produção
curl -sX POST http://ndovu.suaempresa.com/v1/admin/source-maps \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -F 'app=portal-cliente' \
  -F 'release=1.2.3' \
  -F 'filename=main.abc123.js' \
  -F 'sourcemap=@./dist/main.abc123.js.map'
```

Automatize num pipeline CI (GitHub Actions, GitLab CI, etc.).

---

## 13. Padrão K — Instrumentação de bibliotecas específicas

### 13.1 axios

```javascript
import axios from 'axios';
import { ndovu } from '@/lib/ndovu';

axios.interceptors.request.use((config) => {
  config.__ndovuStart = Date.now();
  return config;
});

axios.interceptors.response.use(
  (response) => {
    ndovu.httpRequest(response.config.url, {
      method: response.config.method?.toUpperCase(),
      url: response.config.url,
      statusCode: response.status,
      requestBody: safeJsonParse(response.config.data),
      responseBody: response.data,
      durationMs: Date.now() - response.config.__ndovuStart,
      screen: location.pathname,
    });
    return response;
  },
  (error) => {
    ndovu.httpRequest(error.config?.url ?? 'unknown', {
      method: error.config?.method?.toUpperCase(),
      url: error.config?.url,
      statusCode: error.response?.status,
      durationMs: Date.now() - (error.config?.__ndovuStart ?? Date.now()),
      screen: location.pathname,
      error: {
        code: `HTTP_${error.response?.status ?? 'network'}`,
        message: error.message,
        body: error.response?.data,
      },
    });
    return Promise.reject(error);
  },
);

function safeJsonParse(v) {
  if (typeof v !== 'string') return v;
  try { return JSON.parse(v); } catch { return v; }
}
```

### 13.2 XMLHttpRequest legado

```javascript
const OrigXHR = window.XMLHttpRequest;
window.XMLHttpRequest = function () {
  const xhr = new OrigXHR();
  const origOpen = xhr.open;
  let method, url, start;
  xhr.open = function (m, u) {
    method = m; url = u;
    return origOpen.apply(xhr, arguments);
  };
  xhr.addEventListener('loadstart', () => { start = Date.now(); });
  xhr.addEventListener('loadend', () => {
    ndovu.httpRequest(url, {
      method: method.toUpperCase(),
      url,
      statusCode: xhr.status,
      durationMs: Date.now() - start,
      screen: location.pathname,
      error: xhr.status >= 400 ? {
        code: `HTTP_${xhr.status}`,
        message: xhr.statusText,
      } : undefined,
    });
  });
  return xhr;
};
```

### 13.3 Apollo Client (GraphQL)

Use um `ApolloLink`:

```javascript
import { ApolloLink } from '@apollo/client';
import { ndovu } from '@/lib/ndovu';

const ndovuLink = new ApolloLink((operation, forward) => {
  const start = Date.now();
  return forward(operation).map((response) => {
    ndovu.httpRequest(`graphql/${operation.operationName}`, {
      method: 'POST',
      url: '/graphql',
      statusCode: response.errors ? 500 : 200,
      durationMs: Date.now() - start,
      requestBody: { operation: operation.operationName, variables: operation.variables },
      responseBody: response.data,
      error: response.errors ? {
        code: 'GRAPHQL_ERROR',
        message: response.errors.map(e => e.message).join('; '),
        body: response.errors,
      } : undefined,
    });
    return response;
  });
});

const client = new ApolloClient({
  link: ApolloLink.from([ndovuLink, /* httpLink */]),
});
```

### 13.4 Angular HttpClient

`src/app/lib/ndovu.interceptor.ts`:

```typescript
import { Injectable } from '@angular/core';
import { HttpInterceptor, HttpRequest, HttpHandler, HttpResponse, HttpErrorResponse } from '@angular/common/http';
import { tap } from 'rxjs/operators';
import { NdovuService } from './ndovu.service';

@Injectable()
export class NdovuHttpInterceptor implements HttpInterceptor {
  constructor(private ndovu: NdovuService) {}

  intercept(req: HttpRequest<any>, next: HttpHandler) {
    const start = Date.now();
    return next.handle(req).pipe(
      tap({
        next: (event) => {
          if (event instanceof HttpResponse) {
            this.ndovu['ndovu'].httpRequest(req.url, {
              method: req.method,
              url: req.url,
              statusCode: event.status,
              requestBody: req.body,
              responseBody: event.body,
              durationMs: Date.now() - start,
              screen: location.pathname,
            });
          }
        },
        error: (err: HttpErrorResponse) => {
          this.ndovu['ndovu'].httpRequest(req.url, {
            method: req.method,
            url: req.url,
            statusCode: err.status,
            durationMs: Date.now() - start,
            screen: location.pathname,
            error: {
              code: `HTTP_${err.status}`,
              message: err.message,
              body: err.error,
            },
          });
        },
      }),
    );
  }
}
```

Registrar em `app.module.ts`:

```typescript
providers: [
  { provide: HTTP_INTERCEPTORS, useClass: NdovuHttpInterceptor, multi: true },
],
```

### 13.5 React Native fetch

O RN SDK **não** intercepta fetch por default (evita conflito com libs
como `apisauce`). Ative explicitamente:

```javascript
ndovu.instrumentFetch({
  ignore: ['/health', 'https://ndovu.suaempresa.com'],
});
```

---

## 14. Anti-padrões (o que evitar)

### 14.1 ❌ Um evento por micro-interação

Não capture cada `onMouseMove`, cada `onFocus`. O dashboard vira lixo.

**Regra**: só capture o que você **realmente** vai querer medir.

### 14.2 ❌ IDs dinâmicos em `name` ou `screen`

```javascript
// RUIM
ndovu.action(`atleta_${id}_editado`);
ndovu.pageView(`/atleta/${id}`);

// BOM
ndovu.action('atleta_editado', { metadata: { atletaId: id } });
ndovu.pageView('/atleta/:id', { metadata: { atletaId: id } });
```

### 14.3 ❌ PII em campos indexados

```javascript
// RUIM (nome vai virar valor único no dashboard)
ndovu.action(`login_${cpf}`);

// BOM
ndovu.action('login_success', { metadata: { userId } });
```

### 14.4 ❌ Instrumentar apenas o "caminho feliz"

```javascript
// RUIM: só rastreia sucesso
try {
  await pagar();
  ndovu.action('pagamento_ok');
} catch (e) {
  // nada?
}

// BOM: rastreia sucesso E falha
try {
  await pagar();
  ndovu.action('pagamento_ok');
} catch (e) {
  ndovu.error('pagamento_falhou', {
    code: e.code ?? 'UNKNOWN',
    message: e.message,
    body: { stack: e.stack },
  });
  throw e;
}
```

### 14.5 ❌ Chamar `pageView` no `useState` inicial

Em React, o primeiro render pode acontecer 2x (StrictMode). Use `useEffect`:

```jsx
// RUIM
function Page() {
  ndovu.pageView('/x');   // pode disparar 2x
  return <div />;
}

// BOM
function Page() {
  useEffect(() => { ndovu.pageView('/x'); }, []);
  return <div />;
}
```

### 14.6 ❌ Criar múltiplas instâncias do SDK

```javascript
// RUIM: cada componente cria uma
function Comp() {
  const ndovu = createNdovu({...});    // não faça isso!
}

// BOM: um módulo singleton
// src/lib/ndovu.js
export const ndovu = createNdovu({...});
```

### 14.7 ❌ Hardcode de chave no repositório

```javascript
// RUIM
createNdovu({ apiKey: 'ndk_abc123...' });

// BOM
createNdovu({ apiKey: import.meta.env.VITE_NDOVU_API_KEY });
```

### 14.8 ❌ Bloquear a UI esperando o flush

O SDK é async e fire-and-forget. `sdk.flush()` retorna Promise, mas não
espere ele para renderizar UI:

```javascript
// RUIM: bloqueia navegação
await ndovu.flush();
navigate('/next');

// BOM: dispara e segue
ndovu.flush();
navigate('/next');
```

O `sendBeacon` no `pagehide` já garante que os últimos eventos sejam
enviados no unload — não é necessário aguardar manualmente.

---

## 15. Checklist de qualidade da instrumentação

Depois de instrumentado, revise com este checklist:

### 15.1 Cobertura

- [ ] `pageView` disparado em toda mudança de rota (SPA) ou load (MPA).
- [ ] `instrumentFetch()` ativado — todas chamadas HTTP viram eventos.
- [ ] Se usa lib não-fetch (axios/XHR/Apollo), tem interceptor próprio.
- [ ] `captureGlobals: true` (default) — erros JS não-tratados capturados.
- [ ] Error boundary do framework instalado (React/Vue/Angular).
- [ ] Blocos `catch` de negócio chamam `sdk.error()` com contexto.

### 15.2 Qualidade dos nomes

- [ ] `name` em `snake_case`, verbo + substantivo, sem IDs dinâmicos.
- [ ] `feature` estável e consistente entre eventos da mesma área.
- [ ] `screen` normalizado (`/x/:id` em vez de `/x/abc-123`).
- [ ] Nomes fazem sentido pra quem lê 6 meses depois.

### 15.3 Dados sensíveis

- [ ] Nenhuma PII em texto plano em `name`, `metadata`.
- [ ] Confirmou que redação automática cobre chaves usadas (`password`,
      `token`, `cvv`, `card`, `senha`, `authorization`, `secret`).
- [ ] Snapshots do DOM: campos com PII fora de inputs marcados com
      `data-ndovu-mask`.
- [ ] SDK só inicializa após consentimento LGPD.

### 15.4 Release e correlação

- [ ] `release` configurado no SDK.
- [ ] Source maps enviados no pipeline de deploy.
- [ ] `propagateTraceparent: true` (default) se backend usa OpenTelemetry.

### 15.5 Widget de feedback

- [ ] Widget montado nas telas relevantes (ou globalmente).
- [ ] Posição não sobrepõe elementos críticos.
- [ ] Label em português (ou idioma do app).

### 15.6 Ambiente e config

- [ ] `endpoint` e `apiKey` vindos de env var, não hardcoded.
- [ ] Chaves diferentes por ambiente (dev, staging, prod).
- [ ] `sessionAttributes` inclui `ambiente` para separar tráfego real do
      de QA.

### 15.7 Validação final

- [ ] Testou navegação — eventos `page_view` no dashboard.
- [ ] Testou clique em botão-chave — `action` no dashboard.
- [ ] Testou uma chamada HTTP — `http_request` no dashboard.
- [ ] Forçou um erro (ex.: rota inexistente) — `error` no dashboard.
- [ ] Timeline de uma sessão mostra jornada completa.

Se tudo marcado: pronto para produção.

---

## Referências

- **Como integrar (setup):** [`INTEGRATION.md`](INTEGRATION.md)
- **Contrato de ingestão v1:** [`CONTRACT.md`](CONTRACT.md)
- **Referência técnica completa:** [`TECNICA.md`](TECNICA.md)
- **Funcionalidades do dashboard:** [`FUNCIONAL.md`](FUNCIONAL.md)
- **Arquitetura detalhada:** [`ARQUITETURA.md`](ARQUITETURA.md)

---

Documento vivo. Última atualização: agosto/2026.
Cobre padrões idiomáticos para React JS, React Native, Vue JS, Angular
e JavaScript Vanilla.
