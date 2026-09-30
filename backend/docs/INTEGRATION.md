# Ndovu — Guia de Integração

Guia passo-a-passo para **instalar e configurar** o Ndovu SDK em um frontend
novo. O objetivo é que qualquer desenvolvedor consiga fazer a integração
sozinho, sem precisar de alguém do time Ndovu explicando.

Este documento cobre **como colocar o SDK para funcionar** em cada tecnologia
suportada. Para **como usar** o SDK depois de instalado (padrões idiomáticos,
melhores práticas, casos avançados), veja [`USO.md`](USO.md).

---

## Sumário

1. [Como funciona (30 segundos)](#1-como-funciona-30-segundos)
2. [Pré-requisitos](#2-pré-requisitos)
3. [Obtendo uma chave de API](#3-obtendo-uma-chave-de-api)
4. [Integração — React JS](#4-integração--react-js)
5. [Integração — React Native](#5-integração--react-native)
6. [Integração — Vue JS](#6-integração--vue-js)
7. [Integração — Angular](#7-integração--angular)
8. [Integração — JavaScript Vanilla](#8-integração--javascript-vanilla)
9. [Integração — Next.js (App Router)](#9-integração--nextjs-app-router)
10. [Confirmar que está funcionando](#10-confirmar-que-está-funcionando)
11. [Opções de configuração](#11-opções-de-configuração)
12. [LGPD e consentimento](#12-lgpd-e-consentimento)
13. [Erros comuns](#13-erros-comuns)

---

## 1. Como funciona (30 segundos)

O Ndovu SDK é um **arquivo ES module** (`ndovu-browser.js`) sem
dependências. Você:

1. Copia o arquivo para o seu projeto (ou publica como pacote interno).
2. Cria uma instância passando `endpoint`, `apiKey` e `app`.
3. Chama métodos como `pageView(...)`, `action(...)`, `error(...)` nos
   pontos-chave da jornada.
4. O SDK faz o resto: acumula eventos em batch, envia a cada 5s ou 20
   eventos, faz retry idempotente em falha, redige PII automaticamente,
   captura erros globais, mede Web Vitals, tira snapshot do DOM em cada
   erro.

O SDK responde por 3 endpoints da API Ndovu:

- `POST /v1/events` — eventos (rota principal).
- `POST /v1/snapshots` — snapshot do DOM (session replay).
- `POST /v1/feedbacks` — widget de feedback.

---

## 2. Pré-requisitos

Antes de começar, tenha em mãos:

| Item | Como obter |
|---|---|
| **Endpoint da API Ndovu** | URL onde o Ndovu está rodando. Dev local: `http://localhost:18081`. Produção: `https://ndovu.suaempresa.com` |
| **Nome do app** | Identificador único do seu frontend. Ex.: `portal-cliente`, `app-mobile-ios`. Uma chave = um app |
| **Chave de API** | Gerada no dashboard admin (seção 3) |

---

## 3. Obtendo uma chave de API

As chaves são geridas pelo **backoffice** do Ndovu, por um usuário com
papel `admin`, através do cadastro de Apps.

1. Acesse o dashboard (`http://localhost:13000` em dev) e faça login.
2. Vá em **Apps** → **Cadastrar app**.
3. Preencha:
   - **Nome** (ex.: `portal-cliente`) — este valor é o que você usará no
     `app:` da configuração do SDK.
   - **Tecnologia** (ex.: `React`, `Vue`, `Angular`, `React Native`).
   - **Empresa** — a empresa dona do app.
   - **Responsável** — contato da integração.
4. Ao salvar, o Ndovu **gera automaticamente a chave de API**. Ela é
   exibida uma única vez em texto claro na tela.
5. **Copie imediatamente** e guarde em local seguro. Depois disso só o
   prefixo (`ndk_...`) ficará visível na listagem para identificação.

### Segurança da chave

- Armazenada como bcrypt no banco. Ninguém (nem admin) vê de novo depois
  da criação.
- Em frontend web ela é exposta no browser por natureza — por isso ela só
  pode fazer **ingestão** (nunca consulta/admin).
- Em produção, injete via variável de ambiente do build. **Nunca**
  versione no repositório.
- **Revogação é imediata** (até 30s por causa do cache): se vazar, revogue
  e gere outra. O SDK antigo passa a receber `401` na hora.

### Vinculação app ↔ chave

A ingestão exige que o campo `app` do envelope seja idêntico ao `app` da
chave apresentada em `X-Api-Key`. Isso significa: cada frontend usa a
própria chave e não consegue rotular eventos em nome de outro app (mesmo
que a chave vaze, ela só vale para o app dono).

Para enviar eventos de outro frontend, gere uma chave própria para ele.

---

## 4. Integração — React JS

### 4.1 Instalar o SDK

**Opção A — cópia direta (mais simples):**

Copie `sdk/ndovu-browser.js` para o seu projeto:

```
src/
  lib/
    ndovu-browser.js   ← copie o arquivo aqui
    ndovu.js            ← seu módulo de integração (criará abaixo)
```

**Opção B — pacote interno (npm workspace/monorepo):**

Se sua organização tem monorepo, publique como `@suaempresa/ndovu-sdk` no
seu registry privado. `sdk/ndovu-browser.js` é ES module puro, sem
dependências, empacota trivialmente.

### 4.2 Criar o módulo de integração

`src/lib/ndovu.js`:

```javascript
import { createNdovu } from './ndovu-browser.js';

export const ndovu = createNdovu({
  endpoint: import.meta.env.VITE_NDOVU_ENDPOINT ?? process.env.REACT_APP_NDOVU_ENDPOINT,
  apiKey:   import.meta.env.VITE_NDOVU_API_KEY  ?? process.env.REACT_APP_NDOVU_API_KEY,
  app:      'portal-cliente',
  release:  import.meta.env.VITE_APP_VERSION,        // opcional: release tracking
  getUserId: () => window.__CURRENT_USER__?.id ?? null,
});

// Auto-captura de fetch (recomendado): toda chamada HTTP vira um evento
// http_request automaticamente, sem tocar no código das telas.
ndovu.instrumentFetch();
```

Adicione as variáveis no `.env` (Vite) ou `.env.local` (CRA):

```
# Vite
VITE_NDOVU_ENDPOINT=http://localhost:18081
VITE_NDOVU_API_KEY=ndk_...
VITE_APP_VERSION=1.0.0

# ou Create React App
REACT_APP_NDOVU_ENDPOINT=http://localhost:18081
REACT_APP_NDOVU_API_KEY=ndk_...
REACT_APP_APP_VERSION=1.0.0
```

### 4.3 Rastrear navegação (React Router)

`src/App.jsx`:

```jsx
import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { ndovu } from './lib/ndovu';

export default function App() {
  const location = useLocation();

  useEffect(() => {
    ndovu.pageView(location.pathname);
  }, [location.pathname]);

  return <Routes />;
}
```

### 4.4 Error Boundary (opcional mas recomendado)

`src/components/NdovuErrorBoundary.jsx`:

```jsx
import { Component } from 'react';
import { ndovu } from '../lib/ndovu';

export class NdovuErrorBoundary extends Component {
  componentDidCatch(error, info) {
    ndovu.error('react_error_boundary', {
      code: 'REACT_RENDER',
      message: error.message,
      body: { stack: error.stack, componentStack: info.componentStack },
    });
  }
  render() {
    return this.props.children;
  }
}
```

Usar no `App.jsx`:

```jsx
<NdovuErrorBoundary>
  <App />
</NdovuErrorBoundary>
```

Pronto. Rode o app, navegue algumas telas, abra o dashboard e confirme
que os eventos apareceram.

---

## 5. Integração — React Native

### 5.1 Instalar o SDK

Copie `sdk/ndovu-react-native.js` para o seu projeto:

```
src/
  lib/
    ndovu-react-native.js
    ndovu.js
```

### 5.2 Criar o módulo de integração

`src/lib/ndovu.js`:

```javascript
import { createNdovu } from './ndovu-react-native.js';
import DeviceInfo from 'react-native-device-info';
import { Platform } from 'react-native';

export const ndovu = createNdovu({
  endpoint: 'https://ndovu.suaempresa.com',
  apiKey:   'ndk_...',                              // idealmente do env de build
  app:      'app-mobile-ios',                       // ou 'app-mobile-android'
  release:  DeviceInfo.getVersion(),
  sessionAttributes: {
    platform: Platform.OS,                          // ios | android
    osVersion: Platform.Version,
    deviceModel: DeviceInfo.getModel(),
  },
  getUserId: () => global.__CURRENT_USER__?.id ?? null,
});

// Instala handler global de crash JS
ndovu.captureUncaught();
```

Se sua organização usa `react-native-config` ou `@env` para gestão de
variáveis, injete `endpoint`/`apiKey` por lá.

### 5.3 Rastrear navegação (React Navigation)

```jsx
// App.jsx
import { NavigationContainer } from '@react-navigation/native';
import { useRef } from 'react';
import { ndovu } from './lib/ndovu';

export default function App() {
  const navigationRef = useRef();
  const routeNameRef = useRef();

  return (
    <NavigationContainer
      ref={navigationRef}
      onReady={() => {
        routeNameRef.current = navigationRef.current.getCurrentRoute().name;
        ndovu.pageView(routeNameRef.current);
      }}
      onStateChange={() => {
        const previous = routeNameRef.current;
        const current = navigationRef.current.getCurrentRoute().name;
        if (previous !== current) {
          routeNameRef.current = current;
          ndovu.pageView(current);
        }
      }}
    >
      {/* ... suas Stacks ... */}
    </NavigationContainer>
  );
}
```

### 5.4 Instrumentar fetch (opcional)

Ao contrário do browser SDK, o React Native SDK **não** intercepta `fetch`
por padrão (respeita convenções do RN). Se quiser, chame:

```javascript
ndovu.instrumentFetch({
  ignore: ['https://ndovu.suaempresa.com', 'https://analytics.example.com'],
});
```

Pronto.

---

## 6. Integração — Vue JS

### 6.1 Instalar o SDK

Copie `sdk/ndovu-browser.js` para o seu projeto:

```
src/
  lib/
    ndovu-browser.js
    ndovu.js
```

### 6.2 Criar o módulo de integração

`src/lib/ndovu.js`:

```javascript
import { createNdovu } from './ndovu-browser.js';

export const ndovu = createNdovu({
  endpoint: import.meta.env.VITE_NDOVU_ENDPOINT,
  apiKey:   import.meta.env.VITE_NDOVU_API_KEY,
  app:      'portal-cliente',
  release:  import.meta.env.VITE_APP_VERSION,
  getUserId: () => window.__CURRENT_USER__?.id ?? null,
});

ndovu.instrumentFetch();
```

### 6.3 Expor como plugin do Vue

`src/plugins/ndovu.js`:

```javascript
import { ndovu } from '../lib/ndovu';

export default {
  install(app) {
    app.config.globalProperties.$ndovu = ndovu;
    app.provide('ndovu', ndovu);
  },
};
```

`src/main.js`:

```javascript
import { createApp } from 'vue';
import App from './App.vue';
import router from './router';
import ndovuPlugin from './plugins/ndovu';

const app = createApp(App);
app.use(router);
app.use(ndovuPlugin);
app.mount('#app');
```

### 6.4 Rastrear navegação (Vue Router)

`src/router/index.js`:

```javascript
import { createRouter, createWebHistory } from 'vue-router';
import { ndovu } from '../lib/ndovu';

const router = createRouter({
  history: createWebHistory(),
  routes: [ /* suas rotas */ ],
});

router.afterEach((to) => {
  ndovu.pageView(to.path, { feature: to.meta?.feature });
});

export default router;
```

### 6.5 Error handler global

`src/main.js`:

```javascript
app.config.errorHandler = (err, instance, info) => {
  ndovu.error('vue_error_handler', {
    code: 'VUE_RENDER',
    message: err.message,
    body: { stack: err.stack, info, component: instance?.$options?.name },
  });
};
```

Pronto. Rode `npm run dev`, navegue e confira no dashboard.

---

## 7. Integração — Angular

### 7.1 Instalar o SDK

Copie `sdk/ndovu-browser.js` para o seu projeto:

```
src/
  app/
    lib/
      ndovu-browser.js
```

### 7.2 Criar service Angular

`src/app/lib/ndovu.service.ts`:

```typescript
import { Injectable } from '@angular/core';
import { createNdovu } from './ndovu-browser';
import { environment } from '../../environments/environment';

@Injectable({ providedIn: 'root' })
export class NdovuService {
  private ndovu = createNdovu({
    endpoint: environment.ndovuEndpoint,
    apiKey:   environment.ndovuApiKey,
    app:      'portal-cliente',
    release:  environment.appVersion,
    getUserId: () => (window as any).__CURRENT_USER__?.id ?? null,
  });

  constructor() {
    this.ndovu.instrumentFetch();
  }

  pageView(screen: string, extra: any = {}) {
    this.ndovu.pageView(screen, extra);
  }

  action(name: string, opts: any = {}) {
    this.ndovu.action(name, opts);
  }

  error(name: string, opts: any = {}) {
    this.ndovu.error(name, opts);
  }

  custom(name: string, metadata: any = {}) {
    this.ndovu.custom(name, metadata);
  }
}
```

Como o SDK é JS puro, adicione ao `tsconfig.json`:

```json
{
  "compilerOptions": {
    "allowJs": true
  }
}
```

Ou tipe com um `.d.ts`:

`src/app/lib/ndovu-browser.d.ts`:

```typescript
export function createNdovu(config: {
  endpoint: string;
  apiKey: string;
  app: string;
  release?: string;
  getUserId?: () => string | null;
  captureGlobals?: boolean;
  captureWebVitals?: boolean;
  breadcrumbs?: boolean;
  captureSnapshots?: boolean;
  flushIntervalMs?: number;
  maxBatch?: number;
  sessionAttributes?: Record<string, any>;
}): NdovuSdk;

export interface NdovuSdk {
  pageView(screen: string, extra?: any): void;
  action(name: string, opts?: any): void;
  error(name: string, opts?: any): void;
  httpRequest(name: string, opts: any): void;
  custom(name: string, metadata?: any): void;
  instrumentFetch(opts?: any): void;
  feedback(message: string, opts?: any): Promise<any>;
  mountFeedbackWidget(opts?: any): () => void;
  flush(): Promise<void>;
  sessionId: string;
}
```

### 7.3 Environment

`src/environments/environment.ts`:

```typescript
export const environment = {
  production: false,
  ndovuEndpoint: 'http://localhost:18081',
  ndovuApiKey: 'ndk_...',
  appVersion: '1.0.0',
};
```

### 7.4 Rastrear navegação (Angular Router)

`src/app/app.component.ts`:

```typescript
import { Component, OnInit } from '@angular/core';
import { Router, NavigationEnd } from '@angular/router';
import { filter } from 'rxjs/operators';
import { NdovuService } from './lib/ndovu.service';

@Component({
  selector: 'app-root',
  template: '<router-outlet></router-outlet>',
})
export class AppComponent implements OnInit {
  constructor(private router: Router, private ndovu: NdovuService) {}

  ngOnInit() {
    this.router.events
      .pipe(filter(e => e instanceof NavigationEnd))
      .subscribe((e: NavigationEnd) => {
        this.ndovu.pageView(e.urlAfterRedirects);
      });
  }
}
```

### 7.5 Error handler global

`src/app/lib/ndovu-error-handler.ts`:

```typescript
import { ErrorHandler, Injectable } from '@angular/core';
import { NdovuService } from './ndovu.service';

@Injectable()
export class NdovuErrorHandler implements ErrorHandler {
  constructor(private ndovu: NdovuService) {}

  handleError(error: any) {
    this.ndovu.error('angular_error_handler', {
      code: 'ANGULAR_ERROR',
      message: error.message ?? String(error),
      body: { stack: error.stack },
    });
    console.error(error);  // mantém o log padrão
  }
}
```

`src/app/app.module.ts`:

```typescript
import { ErrorHandler, NgModule } from '@angular/core';
import { NdovuErrorHandler } from './lib/ndovu-error-handler';

@NgModule({
  // ...
  providers: [
    { provide: ErrorHandler, useClass: NdovuErrorHandler },
  ],
})
export class AppModule {}
```

Pronto. `ng serve`, navegue e confira.

---

## 8. Integração — JavaScript Vanilla

Para apps sem framework (sites estáticos, apps legados jQuery, kiosks,
etc.), o SDK é ainda mais direto.

### 8.1 Opção A — script tag (mais simples)

Sirva o SDK como arquivo estático (na sua CDN interna ou via `<script>`
inline):

```html
<!DOCTYPE html>
<html>
<head>
  <script type="module">
    import { createNdovu } from '/js/ndovu-browser.js';

    window.ndovu = createNdovu({
      endpoint: 'https://ndovu.suaempresa.com',
      apiKey:   'ndk_...',
      app:      'site-institucional',
      release:  '2.0.0',
      getUserId: () => window.currentUserId ?? null,
    });

    window.ndovu.instrumentFetch();
    window.ndovu.pageView(location.pathname);
  </script>
</head>
<body>
  <!-- resto do site -->
  <button onclick="window.ndovu.action('clicou_saber_mais', { feature: 'landing' })">
    Saber mais
  </button>
</body>
</html>
```

### 8.2 Opção B — bundler (webpack/rollup/esbuild)

`src/ndovu.js`:

```javascript
import { createNdovu } from '../vendor/ndovu-browser.js';

export const ndovu = createNdovu({
  endpoint: process.env.NDOVU_ENDPOINT,
  apiKey:   process.env.NDOVU_API_KEY,
  app:      'meu-site',
});

ndovu.instrumentFetch();
```

`src/index.js`:

```javascript
import { ndovu } from './ndovu.js';

// Pageview no load
ndovu.pageView(location.pathname);

// Se seu roteador é `history.pushState`, escute:
const push = history.pushState.bind(history);
history.pushState = function (state, title, url) {
  push(state, title, url);
  ndovu.pageView(location.pathname);
};

window.addEventListener('popstate', () => {
  ndovu.pageView(location.pathname);
});
```

### 8.3 Sem módulos (script clássico)

Se o site é muito legado (sem `type="module"`), envolva o SDK em um IIFE
antes de servir. Ou use uma versão empacotada com `esbuild`:

```bash
esbuild sdk/ndovu-browser.js --bundle --format=iife --global-name=NdovuSdk \
  --outfile=public/js/ndovu-browser.iife.js
```

```html
<script src="/js/ndovu-browser.iife.js"></script>
<script>
  var ndovu = NdovuSdk.createNdovu({
    endpoint: 'https://ndovu.suaempresa.com',
    apiKey:   'ndk_...',
    app:      'site-legado',
  });
  ndovu.instrumentFetch();
  ndovu.pageView(location.pathname);
</script>
```

---

## 9. Integração — Next.js (App Router)

Existe um SDK específico (`sdk/ndovu-next.js`) que auto-instrumenta o
router.

### 9.1 Instalar

Copie `sdk/ndovu-next.js` e `sdk/ndovu-browser.js` para:

```
src/
  lib/
    ndovu-browser.js
    ndovu-next.js
    ndovu.js
```

### 9.2 Criar instância

`src/lib/ndovu.js`:

```javascript
import { createNdovu } from './ndovu-browser.js';

export const ndovu = createNdovu({
  endpoint: process.env.NEXT_PUBLIC_NDOVU_ENDPOINT,
  apiKey:   process.env.NEXT_PUBLIC_NDOVU_API_KEY,
  app:      'portal-next',
  release:  process.env.NEXT_PUBLIC_APP_VERSION,
});

ndovu.instrumentFetch();
```

### 9.3 Layout root

`app/layout.tsx`:

```tsx
'use client';
import { NdovuProvider, NdovuAutoRouter } from '@/lib/ndovu-next.js';
import { ndovu } from '@/lib/ndovu';

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
```

`NdovuAutoRouter` observa `usePathname()` + `useSearchParams()` e dispara
`pageView()` automaticamente a cada mudança de rota. Sem código no app.

### 9.4 Usar em componente client

```tsx
'use client';
import { useNdovu } from '@/lib/ndovu-next.js';

export function BotaoCheckout() {
  const ndovu = useNdovu();
  return (
    <button onClick={() => ndovu.action('clicou_finalizar_compra', {
      feature: 'checkout',
    })}>
      Finalizar compra
    </button>
  );
}
```

Pronto.

---

## 10. Confirmar que está funcionando

Depois da integração, valide em 3 passos:

### 10.1 Verificar no browser (DevTools → Network)

Filtre por `/v1/events`. A cada 5s (ou 20 eventos) deve aparecer um
`POST` para `{endpoint}/v1/events` retornando `202 Accepted` com body
`{"accepted": N}`.

Se ver `401`: chave errada.
Se ver `400`: contrato violado (leia `details[]` da resposta).
Se ver `429`: rate limit excedido (raro em dev).
Se não ver nada: SDK não foi inicializado.

### 10.2 Verificar no dashboard

1. Faça login em `http://localhost:13000` (dev) ou na URL de produção.
2. Vá em **Explorador**.
3. Filtre por `app = <nome-do-seu-app>` e período **última 1 hora**.
4. Deve ver seus eventos aparecendo (delay típico: 1-5 segundos).

### 10.3 Smoke test manual (opcional)

Do seu terminal:

```bash
curl -sX POST http://localhost:18081/v1/events \
  -H "X-Api-Key: <SUA_CHAVE>" \
  -H "Content-Type: application/json" \
  -d '{
    "app": "<nome-do-seu-app>",
    "session": {"sessionId": "smoke-test-1"},
    "events": [{
      "eventId": "e-1",
      "type": "action",
      "name": "smoke_test",
      "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"
    }]
  }'
```

Esperado: `{"accepted": 1}`.

---

## 11. Opções de configuração

Reference completa dos parâmetros que `createNdovu({...})` aceita:

| Opção | Tipo | Default | Descrição |
|---|---|---|---|
| `endpoint` | string | — | **Obrigatório.** URL base da API Ndovu, sem barra final |
| `apiKey` | string | — | **Obrigatório.** Chave de ingestão do app |
| `app` | string | — | **Obrigatório.** Nome único do app (deve bater com a chave) |
| `getUserId` | () => string \| null | `() => null` | Callback que retorna o userId atual (chamado a cada evento) |
| `release` | string | `undefined` | Versão do app. Habilita release tracking |
| `sessionAttributes` | object | `{}` | Metadados livres da sessão (plano, canal, etc.) |
| `flushIntervalMs` | number | `5000` | Intervalo (ms) do batch |
| `maxBatch` | number | `20` | Envia se acumular N eventos antes do interval |
| `captureGlobals` | boolean | `true` | Instala handlers: `window.onerror`, `unhandledrejection`, `console.error` |
| `captureWebVitals` | boolean | `true` | Coleta LCP, CLS, INP, FID, TTFB, FCP |
| `breadcrumbs` | boolean | `true` | Mantém ring buffer dos últimos 30 eventos; anexa em cada erro |
| `captureSnapshots` | boolean | `true` | Captura DOM (session replay) em cada `error()` |

### Casos comuns

**Reduzir volume de eventos** (só o essencial):

```javascript
createNdovu({
  ...,
  captureGlobals: true,      // erros: sempre
  captureWebVitals: false,   // desliga LCP/CLS/etc
  breadcrumbs: false,
  captureSnapshots: false,
});
```

**Modo apenas erros** (útil para POC):

```javascript
createNdovu({
  ...,
  captureGlobals: true,
  captureWebVitals: false,
  breadcrumbs: false,
  captureSnapshots: true,
});
// Não chame pageView() nem action() em código.
```

**Batch mais agressivo** (aplicações críticas):

```javascript
createNdovu({
  ...,
  flushIntervalMs: 1000,   // envia a cada 1s
  maxBatch: 5,             // ou a cada 5 eventos
});
```

---

## 12. LGPD e consentimento

O Ndovu coleta dados de jornada que **podem** conter PII (mesmo com
redação automática). Você é responsável por respeitar o consentimento do
usuário (LGPD/GDPR).

### 12.1 Padrão recomendado — só inicializar após consentimento

```javascript
// src/lib/ndovu.js
import { createNdovu } from './ndovu-browser.js';

let ndovu = null;

export function initNdovu() {
  if (ndovu) return ndovu;  // idempotente
  ndovu = createNdovu({
    endpoint: import.meta.env.VITE_NDOVU_ENDPOINT,
    apiKey:   import.meta.env.VITE_NDOVU_API_KEY,
    app:      'portal-cliente',
  });
  ndovu.instrumentFetch();
  return ndovu;
}

export function getNdovu() {
  return ndovu;  // pode retornar null se ainda não consentiu
}

// Chame quando o usuário aceitar o banner de cookies:
export function onConsent() {
  initNdovu();
  ndovu.pageView(location.pathname);
}
```

Nos pontos de uso, faça guard:

```javascript
import { getNdovu } from './lib/ndovu';

getNdovu()?.action('clicou_algo', { feature: 'x' });
```

### 12.2 Redação de PII

O SDK redige automaticamente valores associados a chaves que combinam com:

```
/pass(word)?|senha|token|secret|authorization|cvv|card|cart[aã]o/i
```

Aplicado recursivamente em `requestBody`, `responseBody`, `error.body`.

**Cuidados:**

- **Não** coloque dados sensíveis em `metadata` — a redação só funciona em
  objetos com chaves nominais.
- **Não** coloque PII no `name` do evento (ex.: `login_cpf_12345678900`).
  Use metadata: `name: "login_success", metadata: { userId }`.
- No snapshot do DOM, marque elementos com PII fora de inputs usando
  `data-ndovu-mask`:

```html
<span data-ndovu-mask>{{ user.cpf }}</span>
```

### 12.3 userId

Envie `userId` apenas se o usuário consentiu com identificação. Eventos
anônimos (sem `userId`) são aceitos e correlacionáveis pela sessão.

---

## 13. Erros comuns

| Sintoma | Causa provável | Solução |
|---|---|---|
| `401 unauthorized` na ingestão | Chave ausente, inválida ou revogada | Verifique `X-Api-Key`; gere/revogue em **Chaves de API** |
| `400 app_mismatch` | Campo `app` do envelope ≠ `app` da chave | Confirme que `app:` no `createNdovu` bate exatamente com o nome do app cadastrado |
| `400` com `details[]` | Contrato violado (ex.: eventId não-UUID, type inválido, timestamp ausente) | Leia `details[]` — aponta o evento e o campo exato |
| `413 payload too large` | Batch > 1 MB (raro; costuma ser payload gigante) | Reduza `maxBatch`, remova bodies grandes do `metadata` |
| `429 rate limit` | > 50 req/s por chave (default) | Aumente `flushIntervalMs` ou `maxBatch` (menos requests, mais eventos por request) |
| Eventos não aparecem no dashboard | Delay assíncrono (1-5s), filtro errado ou lote rejeitado | Aguarde; confirme app+período; abra DevTools e veja se o `POST` teve sucesso |
| `fetch` não é capturado | Chamada via `XMLHttpRequest` ou lib com adapter não-fetch | Use `ndovu.httpRequest(...)` manualmente ou adapte o interceptor da lib |
| Loop infinito de eventos | `instrumentFetch` capturando o próprio endpoint do Ndovu | Por default o SDK ignora o próprio endpoint; se sobrescreveu `ignore`, garanta incluir |
| Erro `Cannot find module 'ndovu-browser'` | Path errado | Confirme a localização do arquivo copiado |
| `import.meta.env.VITE_NDOVU_...` undefined | Falta reiniciar `npm run dev` após editar `.env` | Reinicie o dev server |

### Verificação rápida da API

```bash
# 1) API viva?
curl http://localhost:18081/health
# => {"status":"ok"}

# 2) Chave funciona?
curl -sX POST http://localhost:18081/v1/events \
  -H "X-Api-Key: <SUA_CHAVE>" \
  -H "Content-Type: application/json" \
  -d '{"app":"<SEU_APP>","session":{"sessionId":"test"},"events":[]}'
# => {"accepted": 0}  (ok, aceitou lote vazio)
```

---

## Próximos passos

Após integrado, aprenda os padrões idiomáticos de uso:

- **[USO.md](USO.md)** — como capturar pageview, actions, errors e HTTP
  de forma idiomática em cada framework, com exemplos avançados
  (widget de feedback, error boundary, correlação com backend, etc.).

Documentação técnica complementar:

- **[CONTRACT.md](CONTRACT.md)** — spec completa do contrato v1.
- **[TECNICA.md](TECNICA.md)** — referência completa da API, endpoints
  admin, deploy.
- **[ARQUITETURA.md](ARQUITETURA.md)** — como o Ndovu funciona por
  dentro.

---

Documento vivo. Última atualização: agosto/2026.
Cobre integração para React JS, React Native, Vue JS, Angular,
JavaScript Vanilla e Next.js.
