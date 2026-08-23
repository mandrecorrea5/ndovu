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
 */

import { randomUUID } from 'node:crypto';
import { hostname } from 'node:os';

const SDK_VERSION = '1.1.0-node';
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

  function push(event, session) {
    queue.push({
      session,
      event: { eventId: randomUUID(), timestamp: new Date().toISOString(), ...event },
    });
    if (queue.length >= maxBatch) void flush();
    schedule();
  }

  const sdk = {
    processSessionId,

    action(name, { feature, screen, metadata, session } = {}) {
      push({ type: 'action', name, feature, screen, metadata }, session);
    },

    error(name, { code, message, body, feature, screen, durationMs, metadata, session } = {}) {
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
      );
    },

    httpRequest(name, opts = {}) {
      const { method, url, statusCode, requestBody, responseBody, durationMs, feature, screen, session, error } = opts;
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
      );
    },

    custom(name, { metadata, session } = {}) {
      push({ type: 'custom', name, metadata }, session);
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
     * pelo SDK browser (X-Ndovu-Session-Id) quando presente.
     */
    middleware({ nameFor = (req) => `${req.method} ${req.route?.path ?? req.path ?? req.url}` } = {}) {
      return (req, res, next) => {
        const started = process.hrtime.bigint();
        const sessionFromClient = req.headers['x-ndovu-session-id'];
        const userIdFromClient = req.headers['x-ndovu-user-id'];
        const session = sessionFromClient
          ? { sessionId: String(sessionFromClient), userId: userIdFromClient ? String(userIdFromClient) : undefined }
          : undefined;

        res.on('finish', () => {
          const durationMs = Number((process.hrtime.bigint() - started) / 1_000_000n);
          const failed = res.statusCode >= 500;
          this.httpRequest(nameFor(req), {
            method: req.method,
            url: req.originalUrl ?? req.url,
            statusCode: res.statusCode,
            durationMs,
            session,
            error: failed
              ? { code: `HTTP_${res.statusCode}`, message: res.statusMessage || 'server_error' }
              : undefined,
          });
        });
        next();
      };
    },

    flush,
  };

  return sdk;
}
