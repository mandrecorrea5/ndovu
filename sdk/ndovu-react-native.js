/**
 * Ndovu React Native SDK (contrato v1, zero dependências obrigatórias).
 *
 * Mesma superfície do ndovu-browser.js, sem depender de DOM:
 *   - fetch é global no RN
 *   - crypto.getRandomValues via `react-native-get-random-values` (o app importa)
 *   - storage é opcional e injetado (recomendado: AsyncStorage)
 *   - captureGlobals usa ErrorUtils.setGlobalHandler (RN nativo)
 *   - flush em background usa AppState (opcional)
 *
 * Uso mínimo:
 *   import 'react-native-get-random-values';
 *   import AsyncStorage from '@react-native-async-storage/async-storage';
 *   import { AppState, Platform } from 'react-native';
 *   import { createNdovu } from '@your-org/ndovu-react-native';
 *
 *   export const ndovu = await createNdovu({
 *     endpoint: 'https://ndovu.acme.com',
 *     apiKey: 'ndk_...',
 *     app: 'mobile-cliente',
 *     release: '1.4.0-build.42',
 *     storage: AsyncStorage,
 *     appState: AppState,
 *     platform: { os: Platform.OS, version: String(Platform.Version) },
 *   });
 *
 *   ndovu.pageView('Home');
 *   ndovu.action('clicou_pagar', { feature: 'checkout' });
 */

const SDK_VERSION = '1.0.0-rn';
const REDACT_KEYS = /pass(word)?|senha|token|secret|authorization|cvv|card|cart[aã]o/i;
const BREADCRUMB_LIMIT = 30;
const SESSION_KEY = 'ndovu.sessionId';

function uuid() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID();
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16);
  });
}

function randHex(bytes) {
  const arr = new Uint8Array(bytes);
  if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
    crypto.getRandomValues(arr);
  } else {
    for (let i = 0; i < bytes; i++) arr[i] = Math.floor(Math.random() * 256);
  }
  return Array.from(arr, (b) => b.toString(16).padStart(2, '0')).join('');
}

function traceparent(traceId, spanId) {
  return `00-${traceId}-${spanId}-01`;
}

export function redact(value, depth = 0) {
  if (value == null || depth > 6) return value;
  if (Array.isArray(value)) return value.map((v) => redact(v, depth + 1));
  if (typeof value === 'object') {
    const out = {};
    for (const [k, v] of Object.entries(value)) {
      out[k] = REDACT_KEYS.test(k) ? '***' : redact(v, depth + 1);
    }
    return out;
  }
  return value;
}

/**
 * Cria o SDK. É async porque a leitura do sessionId do storage é async.
 * Se `storage` não for passado, a sessão vive só em memória (perde ao matar
 * o app) — funciona, mas menos útil para retenção.
 */
export async function createNdovu({
  endpoint,
  apiKey,
  app,
  getUserId = () => null,
  flushIntervalMs = 5000,
  maxBatch = 20,
  sessionAttributes = {},
  captureGlobals = true,
  breadcrumbs = true,
  release,
  storage, // { getItem(key): Promise<string|null>, setItem(key, val): Promise<void> }
  appState, // opcional: AppState do react-native — flush ao ir pra background
  platform, // { os, version, model? }
}) {
  if (!endpoint || !apiKey || !app) {
    throw new Error('ndovu-rn: endpoint, apiKey e app são obrigatórios');
  }

  // Sessão persistida entre execuções do app se houver storage — comportamento
  // similar ao sessionStorage do web (mesma vida do "app aberto").
  let sessionId;
  if (storage) {
    try {
      sessionId = (await storage.getItem(SESSION_KEY)) || uuid();
      await storage.setItem(SESSION_KEY, sessionId);
    } catch {
      sessionId = uuid();
    }
  } else {
    sessionId = uuid();
  }

  const queue = [];
  const crumbs = [];
  let timer = null;

  const userAgent = platform
    ? `${platform.os}/${platform.version}${platform.model ? ' (' + platform.model + ')' : ''}`
    : 'react-native';

  function envelope(events) {
    return {
      app,
      sdkVersion: SDK_VERSION,
      session: {
        sessionId,
        userId: getUserId() ?? undefined,
        userAgent,
        attributes: { ...sessionAttributes, ...(release ? { release } : {}) },
      },
      events,
    };
  }

  async function flush() {
    if (queue.length === 0) return;
    const events = queue.splice(0, queue.length);
    const body = JSON.stringify(envelope(events));
    const url = `${endpoint.replace(/\/$/, '')}/v1/events`;
    try {
      await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Api-Key': apiKey },
        body,
      });
    } catch {
      // Falha de rede: devolve à fila — eventId garante idempotência no reenvio.
      queue.unshift(...events);
    }
  }

  function schedule() {
    if (timer != null) return;
    timer = setInterval(() => {
      void flush();
    }, flushIntervalMs);
    if (typeof timer?.unref === 'function') timer.unref();
  }

  function addCrumb(kind, data) {
    if (!breadcrumbs) return;
    crumbs.push({ t: new Date().toISOString(), kind, ...data });
    if (crumbs.length > BREADCRUMB_LIMIT) crumbs.splice(0, crumbs.length - BREADCRUMB_LIMIT);
  }

  function attachBreadcrumbs(event) {
    if (!breadcrumbs || event.type !== 'error' || crumbs.length === 0) return event;
    const existing = event.metadata ?? {};
    return { ...event, metadata: { ...existing, breadcrumbs: crumbs.slice() } };
  }

  function push(event) {
    const enriched = attachBreadcrumbs({
      eventId: uuid(),
      timestamp: new Date().toISOString(),
      ...event,
    });
    queue.push(enriched);
    if (enriched.type !== 'error') {
      addCrumb(enriched.type, {
        name: enriched.name,
        screen: enriched.screen,
        url: enriched.http?.url,
        status: enriched.http?.statusCode,
      });
    }
    if (queue.length >= maxBatch) void flush();
    schedule();
  }

  const sdk = {
    sessionId,

    pageView(screen, extra = {}) {
      push({ type: 'page_view', name: `tela_${screen}`, screen, ...extra });
    },

    action(name, { feature, screen, metadata } = {}) {
      push({ type: 'action', name, feature, screen, metadata });
    },

    error(name, { code, message, body, feature, screen, durationMs, metadata, trace } = {}) {
      push({
        type: 'error',
        name,
        feature,
        screen,
        durationMs,
        metadata,
        trace,
        error: { code, message, body: redact(body) },
      });
    },

    httpRequest(name, {
      method, url, statusCode, requestBody, responseBody,
      durationMs, feature, screen, error, trace,
    }) {
      push({
        type: 'http_request',
        name,
        feature,
        screen,
        durationMs,
        trace,
        http: {
          method,
          url,
          statusCode,
          requestBody: redact(requestBody),
          responseBody: redact(responseBody),
        },
        error,
      });
    },

    custom(name, metadata = {}) {
      push({ type: 'custom', name, metadata });
    },

    /**
     * Instrumenta fetch globalmente — toda chamada vira http_request
     * (com trace_id/span_id + header traceparent propagado se pedido).
     * O endpoint do próprio Ndovu é excluído para não recursionar.
     */
    instrumentFetch({
      nameFor = (u) => (u.split('?')[0] || u),
      ignore = [endpoint],
      propagateTraceparent = true,
    } = {}) {
      const original = global.fetch;
      const self = this;
      global.fetch = async (input, init = {}) => {
        const url = typeof input === 'string' ? input : input.url;
        if (ignore.some((i) => url.startsWith(i))) return original(input, init);

        const started = Date.now();
        const method = (init.method ?? 'GET').toUpperCase();
        let requestBody;
        try {
          requestBody = init.body ? JSON.parse(init.body) : undefined;
        } catch {
          requestBody = undefined;
        }

        let trace;
        let nextInit = init;
        if (propagateTraceparent) {
          trace = { traceId: randHex(16), spanId: randHex(8) };
          const headers = { ...(init.headers ?? {}) };
          if (!('traceparent' in headers) && !('Traceparent' in headers)) {
            headers.traceparent = traceparent(trace.traceId, trace.spanId);
          }
          nextInit = { ...init, headers };
        }

        try {
          const res = await original(input, nextInit);
          const durationMs = Date.now() - started;
          let responseBody;
          try {
            responseBody = await res.clone().json();
          } catch {
            responseBody = undefined;
          }
          const failed = !res.ok;
          self.httpRequest(nameFor(url), {
            method,
            url,
            statusCode: res.status,
            requestBody,
            responseBody,
            durationMs,
            trace,
            error: failed
              ? { code: `HTTP_${res.status}`, message: 'request failed', body: responseBody }
              : undefined,
          });
          return res;
        } catch (err) {
          const durationMs = Date.now() - started;
          self.error(nameFor(url), {
            code: 'NETWORK_ERROR',
            message: String(err),
            durationMs,
            trace,
          });
          throw err;
        }
      };
    },

    flush,
  };

  if (captureGlobals && typeof ErrorUtils !== 'undefined' && ErrorUtils.setGlobalHandler) {
    installErrorUtilsHandler(sdk);
  }
  if (appState && typeof appState.addEventListener === 'function') {
    appState.addEventListener('change', (state) => {
      if (state === 'background' || state === 'inactive') void flush();
    });
  }

  schedule();
  return sdk;
}

// ---------------------------------------------------------------------------
// ErrorUtils global handler — captura JS errors não tratados do RN sem
// perder o handler default (que faz o app crashar visualmente em dev).
// ---------------------------------------------------------------------------

function installErrorUtilsHandler(sdk) {
  const previous = ErrorUtils.getGlobalHandler();
  ErrorUtils.setGlobalHandler((err, isFatal) => {
    try {
      sdk.error('js_uncaught', {
        code: isFatal ? 'FATAL_JS_ERROR' : 'JS_ERROR',
        message: err?.message ?? String(err),
        body: { stack: err?.stack, name: err?.name, isFatal: !!isFatal },
      });
      void sdk.flush();
    } catch {
      // não bloqueia o crash pipeline nativo
    }
    if (previous) previous(err, isFatal);
  });
}
