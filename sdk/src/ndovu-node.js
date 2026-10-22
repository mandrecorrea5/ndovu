/**
 * Ndovu Node SDK (referência, zero dependências).
 *
 * Implementa o contrato v1 (docs/CONTRACT.md) para APIs Node.js.
 * Diferente do browser SDK, aqui não temos "sessão do usuário" persistida:
 * cada request pode ser sua própria sessão (via requestId) ou herdar a do
 * frontend (X-Ndovu-Session-Id vindo do SDK browser).
 *
 * Uso básico:
 *   import { createNdovu } from './ndovu-node.js';
 *   const ndovu = createNdovu({
 *     endpoint: 'http://localhost:8080',
 *     apiKey: process.env.NDOVU_API_KEY,
 *     app: 'api-cobranca',
 *   });
 *   ndovu.captureUncaught(); // instala handlers de uncaughtException/unhandledRejection
 *
 * Middleware Express:
 *   app.use(ndovu.middleware());
 *
 * Manual:
 *   ndovu.action('processou_boleto', { feature: 'faturas', metadata: { valor: 100 } });
 *   ndovu.error('falha_gateway', { code: 'GW_500', message: 'timeout' });
 *
 * Trace W3C: o middleware lê o header `traceparent` (enviado pelo browser SDK
 * via instrumentFetch) e grava o mesmo traceId nos eventos do servidor — o
 * request aparece em GET /v1/traces/{traceId} junto com o clique que o
 * originou. Sem header, o request abre um trace novo. Eventos manuais
 * emitidos dentro do handler herdam o trace e a sessão do request sozinhos
 * (AsyncLocalStorage). Para propagar a chamadas de saída:
 *   fetch(url, { headers: { traceparent: ndovu.traceparent() } });
 */

import { AsyncLocalStorage } from 'node:async_hooks';
import { randomBytes, randomUUID } from 'node:crypto';
import { hostname } from 'node:os';

const SDK_VERSION = '1.2.0-node';
const TRACEPARENT = /^([0-9a-f]{2})-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})$/;
const ZERO_TRACE = '0'.repeat(32);
const ZERO_SPAN = '0'.repeat(16);

/**
 * Decompõe um header W3C `traceparent`. Devolve `{ traceId, spanId, sampled }`
 * ou `null` se o header estiver ausente ou inválido — a spec manda ignorar
 * (e abrir trace novo) em vez de propagar lixo.
 */
export function parseTraceparent(header) {
  if (typeof header !== 'string') return null;
  const m = TRACEPARENT.exec(header.trim().toLowerCase());
  if (!m) return null;
  const [, version, traceId, spanId, flags] = m;
  if (version === 'ff' || traceId === ZERO_TRACE || spanId === ZERO_SPAN) return null;
  return { traceId, spanId, sampled: (parseInt(flags, 16) & 1) === 1 };
}

/** Monta um header `traceparent` a partir de traceId + spanId. */
export function formatTraceparent({ traceId, spanId, sampled = true }) {
  return `00-${traceId}-${spanId}-${sampled ? '01' : '00'}`;
}

const newTraceId = () => randomBytes(16).toString('hex');
const newSpanId = () => randomBytes(8).toString('hex');
const REDACT_KEYS = /pass(word)?|senha|token|secret|authorization|cvv|card|cart[aã]o/i;

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

export function createNdovu({
  endpoint,
  apiKey,
  app,
  flushIntervalMs = 5000,
  maxBatch = 20,
  release,
  serviceInstance = hostname(),
}) {
  if (!endpoint || !apiKey || !app) {
    throw new Error('ndovu-node: endpoint, apiKey e app são obrigatórios');
  }

  // No servidor, a "sessão" default é o próprio processo — cada evento manual
  // sem sessionId cai aqui. Requests HTTP normalmente sobrescrevem via middleware.
  const processSessionId = randomUUID();
  // Contexto do request corrente: { session, traceId, spanId, sampled }.
  const requestContext = new AsyncLocalStorage();

  const queue = [];
  let timer = null;

  function envelope(events, sessionOverride) {
    return {
      app,
      sdkVersion: SDK_VERSION,
      session: {
        sessionId: sessionOverride?.sessionId ?? processSessionId,
        userId: sessionOverride?.userId,
        userAgent: sessionOverride?.userAgent ?? `node/${process.version}`,
        attributes: {
          serviceInstance,
          ...(release ? { release } : {}),
          ...(sessionOverride?.attributes ?? {}),
        },
      },
      events,
    };
  }

  async function sendEnvelope(env) {
    const url = `${endpoint.replace(/\/$/, '')}/v1/events`;
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Api-Key': apiKey },
      body: JSON.stringify(env),
    });
    if (!res.ok && res.status !== 202) {
      throw new Error(`ndovu-node: envio recusado (HTTP ${res.status})`);
    }
  }

  async function flush() {
    if (queue.length === 0) return;
    // Agrupa por sessionId para poder enviar em vários envelopes se preciso
    const items = queue.splice(0, queue.length);
    const bySession = new Map();
    for (const it of items) {
      const key = it.session?.sessionId ?? processSessionId;
      if (!bySession.has(key)) bySession.set(key, { session: it.session, events: [] });
      bySession.get(key).events.push(it.event);
    }
    for (const { session, events } of bySession.values()) {
      try {
        await sendEnvelope(envelope(events, session));
      } catch {
        // devolve para a fila — eventId garante idempotência no reenvio
        for (const event of events) {
          queue.push({ session, event });
        }
      }
    }
  }

  function schedule() {
    if (timer == null) {
      timer = setInterval(() => flush().catch(() => {}), flushIntervalMs);
      if (typeof timer?.unref === 'function') timer.unref();
    }
  }

  // Sem trace explícito, um evento emitido dentro de um request vira span
  // filho do span do request; fora de request, sai sem trace (como antes).
  function push(event, session, trace) {
    const ctx = requestContext.getStore();
    const resolvedTrace =
      trace ?? (ctx ? { traceId: ctx.traceId, spanId: newSpanId(), parentSpanId: ctx.spanId } : undefined);
    queue.push({
      session: session ?? ctx?.session,
      event: {
        eventId: randomUUID(),
        timestamp: new Date().toISOString(),
        ...event,
        ...(resolvedTrace ? { trace: resolvedTrace } : {}),
      },
    });
    if (queue.length >= maxBatch) void flush();
    schedule();
  }

  const sdk = {
    processSessionId,

    action(name, { feature, screen, metadata, session, trace } = {}) {
      push({ type: 'action', name, feature, screen, metadata }, session, trace);
    },

    error(name, { code, message, body, feature, screen, durationMs, metadata, session, trace } = {}) {
      push(
        {
          type: 'error',
          name,
          feature,
          screen,
          durationMs,
          metadata,
          error: { code, message, body: redact(body) },
        },
        session,
        trace,
      );
    },

    httpRequest(name, opts = {}) {
      const { method, url, statusCode, requestBody, responseBody, durationMs, feature, screen, session, error, trace } =
        opts;
      push(
        {
          type: 'http_request',
          name,
          feature,
          screen,
          durationMs,
          http: {
            method,
            url,
            statusCode,
            requestBody: redact(requestBody),
            responseBody: redact(responseBody),
          },
          error,
        },
        session,
        trace,
      );
    },

    custom(name, { metadata, session, trace } = {}) {
      push({ type: 'custom', name, metadata }, session, trace);
    },

    /**
     * Header `traceparent` para uma chamada de saída feita dentro do request
     * corrente — o serviço chamado continua o mesmo trace. Fora de request,
     * devolve `undefined` (não inventa trace solto).
     */
    traceparent() {
      const ctx = requestContext.getStore();
      if (!ctx) return undefined;
      return formatTraceparent({ traceId: ctx.traceId, spanId: ctx.spanId, sampled: ctx.sampled });
    },

    /** `{ traceId, spanId }` do request corrente, ou `undefined` fora de request. */
    currentTrace() {
      const ctx = requestContext.getStore();
      return ctx ? { traceId: ctx.traceId, spanId: ctx.spanId } : undefined;
    },

    /** Instala handlers de uncaughtException e unhandledRejection. */
    captureUncaught() {
      process.on('uncaughtException', (err) => {
        this.error('uncaught_exception', {
          code: 'UNCAUGHT_EXCEPTION',
          message: err?.message ?? String(err),
          body: { stack: err?.stack, name: err?.name },
        });
        // best-effort de flush antes de o processo cair
        void flush();
      });
      process.on('unhandledRejection', (reason) => {
        this.error('unhandled_rejection', {
          code: 'UNHANDLED_REJECTION',
          message: reason?.message ?? String(reason),
          body: { stack: reason?.stack, reason: reason ? String(reason) : undefined },
        });
        void flush();
      });
    },

    /**
     * Middleware Express/Connect: registra um http_request por request com
     * método, rota, status, duração — herdando sessionId do header enviado
     * pelo SDK browser (X-Ndovu-Session-Id) quando presente, e o traceId do
     * header `traceparent`. O span do request é filho do span do fetch do
     * browser; o resto do handler roda dentro desse contexto.
     */
    middleware({ nameFor = (req) => `${req.method} ${req.route?.path ?? req.path ?? req.url}` } = {}) {
      return (req, res, next) => {
        const started = process.hrtime.bigint();
        const sessionFromClient = req.headers['x-ndovu-session-id'];
        const userIdFromClient = req.headers['x-ndovu-user-id'];
        const session = sessionFromClient
          ? { sessionId: String(sessionFromClient), userId: userIdFromClient ? String(userIdFromClient) : undefined }
          : undefined;
        const incoming = parseTraceparent(req.headers.traceparent);
        const ctx = {
          session,
          traceId: incoming?.traceId ?? newTraceId(),
          spanId: newSpanId(),
          sampled: incoming?.sampled ?? true,
        };
        const trace = { traceId: ctx.traceId, spanId: ctx.spanId, parentSpanId: incoming?.spanId };

        res.on('finish', () => {
          const durationMs = Number((process.hrtime.bigint() - started) / 1_000_000n);
          const failed = res.statusCode >= 500;
          this.httpRequest(nameFor(req), {
            method: req.method,
            url: req.originalUrl ?? req.url,
            statusCode: res.statusCode,
            durationMs,
            session,
            trace,
            error: failed
              ? { code: `HTTP_${res.statusCode}`, message: res.statusMessage || 'server_error' }
              : undefined,
          });
        });
        requestContext.run(ctx, next);
      };
    },

    flush,
  };

  return sdk;
}
