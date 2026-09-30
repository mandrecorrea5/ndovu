/**
 * Ndovu Browser SDK (referência, ~zero dependências).
 *
 * Implementa o contrato v1 (docs/CONTRACT.md): qualquer frontend que envie o
 * mesmo JSON está integrado — este arquivo é só uma conveniência para web.
 *
 * Uso:
 *   import { createNdovu } from './ndovu-browser.js';
 *   const ndovu = createNdovu({
 *     endpoint: 'http://localhost:8080',
 *     apiKey: 'dev-ingest-key',
 *     app: 'portal-cliente',
 *     getUserId: () => window.currentUserId ?? null,
 *     captureGlobals: true,   // window.onerror, unhandledrejection, console.error
 *     captureWebVitals: true, // LCP, CLS, INP, FID, TTFB, FCP
 *     breadcrumbs: true,      // ring buffer dos últimos N eventos anexado a erros
 *   });
 *   ndovu.pageView('/faturas');
 *   ndovu.action('clicou_segunda_via', { feature: 'faturas' });
 *   ndovu.instrumentFetch(); // captura fetch() automaticamente
 */

const SDK_VERSION = '1.2.0';
const REDACT_KEYS = /pass(word)?|senha|token|secret|authorization|cvv|card|cart[aã]o/i;
const BREADCRUMB_LIMIT = 30;

function uuid() {
  return crypto.randomUUID
    ? crypto.randomUUID()
    : 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
        const r = (Math.random() * 16) | 0;
        return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16);
      });
}

// W3C traceparent: gera 16 bytes hex (traceId de 32) ou 8 (spanId de 16).
function randHex(bytes) {
  const arr = new Uint8Array(bytes);
  (crypto.getRandomValues ? crypto : { getRandomValues: (a) => a.forEach((_, i, r) => (r[i] = Math.floor(Math.random() * 256))) })
    .getRandomValues(arr);
  return Array.from(arr, (b) => b.toString(16).padStart(2, '0')).join('');
}

// Formata `traceparent` no formato: 00-<32hex traceId>-<16hex spanId>-01
function traceparent(traceId, spanId) {
  return `00-${traceId}-${spanId}-01`;
}

/** Remove valores sensíveis antes de sair do browser (responsabilidade do SDK). */
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
  getUserId = () => null,
  flushIntervalMs = 5000,
  maxBatch = 20,
  sessionAttributes = {},
  captureGlobals = true,
  captureWebVitals = true,
  breadcrumbs = true,
  captureSnapshots = true, // envia snapshot do DOM em cada error()
  release,
}) {
  if (!endpoint || !apiKey || !app) {
    throw new Error('ndovu: endpoint, apiKey e app são obrigatórios');
  }

  // Sessão sobrevive a reloads na mesma aba
  const KEY = 'ndovu.sessionId';
  let sessionId;
  try {
    sessionId = sessionStorage.getItem(KEY) ?? uuid();
    sessionStorage.setItem(KEY, sessionId);
  } catch {
    sessionId = uuid();
  }

  let queue = [];
  let timer = null;
  // Ring buffer local — anexado como metadata.breadcrumbs em cada erro enviado
  const crumbs = [];
  // Último eventId gerado — widget de feedback usa pra amarrar ao evento problemático.
  let lastEventId = null;

  function envelope(events) {
    return {
      app,
      sdkVersion: SDK_VERSION,
      session: {
        sessionId,
        userId: getUserId() ?? undefined,
        userAgent: navigator.userAgent,
        attributes: { ...sessionAttributes, ...(release ? { release } : {}) },
      },
      events,
    };
  }

  async function flush(useBeacon = false) {
    if (queue.length === 0) return;
    const events = queue.splice(0, queue.length);
    const body = JSON.stringify(envelope(events));
    const url = `${endpoint.replace(/\/$/, '')}/v1/events`;
    try {
      if (useBeacon && navigator.sendBeacon) {
        // sendBeacon não envia headers custom; em produção use um endpoint
        // com chave na query ou aceite o POST normal com keepalive:
        await fetch(url, {
          method: 'POST',
          keepalive: true,
          headers: { 'Content-Type': 'application/json', 'X-Api-Key': apiKey },
          body,
        });
      } else {
        await fetch(url, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-Api-Key': apiKey },
          body,
        });
      }
    } catch {
      // Falha de rede: devolve para a fila (eventId garante idempotência no reenvio)
      queue = events.concat(queue);
    }
  }

  function schedule() {
    if (timer == null) {
      timer = setInterval(() => flush(), flushIntervalMs);
      window.addEventListener('pagehide', () => flush(true));
    }
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
    lastEventId = enriched.eventId;
    // Erros não são um breadcrumb: são o alvo. Todos os outros eventos viram trilha.
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
      // Gera o eventId aqui pra amarrar o snapshot ao mesmo evento.
      const eventId = uuid();
      push({
        eventId,
        type: 'error',
        name,
        feature,
        screen,
        durationMs,
        metadata,
        trace,
        error: { code, message, body: redact(body) },
      });
      // Snapshot é fire-and-forget: se falhar, não bloqueia o evento.
      if (captureSnapshots && typeof document !== 'undefined') {
        void sendSnapshot(endpoint, apiKey, app, sessionId, eventId);
      }
    },

    /** Registra manualmente uma chamada HTTP já feita pelo app. */
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

    /** Registra manualmente uma métrica de performance (Web Vitals ou custom). */
    custom(name, metadata = {}) {
      push({ type: 'custom', name, metadata });
    },

    /**
     * Instrumentação automática de fetch(): toda chamada vira um trace com
     * payloads, status e duração — sem tocar no código das telas.
     *
     * Se `propagateTraceparent` for true (default), o SDK gera um W3C
     * traceparent para cada requisição e o adiciona como header outgoing —
     * quando o backend também estiver instrumentado com OpenTelemetry, os
     * traces frontend↔backend ficam correlacionados pelo mesmo trace_id.
     */
    instrumentFetch({
      nameFor = (u) => u.split('?')[0],
      ignore = [endpoint],
      propagateTraceparent = true,
    } = {}) {
      const original = window.fetch.bind(window);
      const self = this;
      window.fetch = async (input, init = {}) => {
        const url = typeof input === 'string' ? input : input.url;
        if (ignore.some((i) => url.startsWith(i))) return original(input, init);

        const started = performance.now();
        const method = (init.method ?? 'GET').toUpperCase();
        let requestBody;
        try {
          requestBody = init.body ? JSON.parse(init.body) : undefined;
        } catch {
          requestBody = undefined;
        }

        // Gera trace_id/span_id e propaga como header outgoing (opt-out).
        let trace;
        let nextInit = init;
        if (propagateTraceparent) {
          trace = { traceId: randHex(16), spanId: randHex(8) };
          const headers = new Headers(init.headers ?? {});
          if (!headers.has('traceparent')) {
            headers.set('traceparent', traceparent(trace.traceId, trace.spanId));
          }
          nextInit = { ...init, headers };
        }

        try {
          const res = await original(input, nextInit);
          const durationMs = Math.round(performance.now() - started);
          let responseBody;
          try {
            responseBody = await res.clone().json();
          } catch {
            responseBody = undefined;
          }
          const failed = !res.ok;
          self.httpRequest(nameFor(url), {
            method,
            url: new URL(url, location.origin).pathname,
            statusCode: res.status,
            requestBody,
            responseBody,
            durationMs,
            screen: location.pathname,
            trace,
            error: failed
              ? { code: `HTTP_${res.status}`, message: res.statusText, body: responseBody }
              : undefined,
          });
          return res;
        } catch (err) {
          const durationMs = Math.round(performance.now() - started);
          self.error(nameFor(url), {
            code: 'NETWORK_ERROR',
            message: String(err),
            durationMs,
            screen: location.pathname,
            trace,
          });
          throw err;
        }
      };
    },

    flush,

    /**
     * Envia feedback do usuário (bug|suggestion|praise|other), atrelado à
     * sessão atual e ao último eventId visto — quando o admin abrir o feedback
     * no dashboard, consegue navegar direto pro erro/sessão que motivou.
     */
    async feedback(message, { type = 'bug', email, eventId } = {}) {
      if (!message || !message.trim()) throw new Error('ndovu.feedback: message obrigatório');
      const body = JSON.stringify({
        app,
        sessionId,
        type,
        message: message.trim().slice(0, 5000),
        email,
        eventId: eventId ?? lastEventId ?? undefined,
        url: typeof location !== 'undefined' ? location.href : undefined,
        viewportW: typeof window !== 'undefined' ? window.innerWidth : undefined,
        viewportH: typeof window !== 'undefined' ? window.innerHeight : undefined,
      });
      const res = await fetch(`${endpoint.replace(/\/$/, '')}/v1/feedbacks`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Api-Key': apiKey },
        body,
      });
      if (!res.ok) throw new Error(`ndovu.feedback: HTTP ${res.status}`);
      return res.json();
    },

    /**
     * Monta um botão flutuante + modal simples de feedback no DOM. Retorna
     * uma função para desmontar. Sem dependências — só um elemento
     * `<div id="ndovu-feedback">` com estilos inline.
     */
    mountFeedbackWidget(opts = {}) {
      if (typeof document === 'undefined') return () => {};
      return mountWidget(this, opts);
    },
  };

  if (captureGlobals) installGlobalHandlers(sdk);
  if (captureWebVitals) installWebVitals(sdk);

  return sdk;
}

// ---------------------------------------------------------------------------
// Widget de feedback — botão flutuante + modal, zero dependência
// ---------------------------------------------------------------------------

function mountWidget(sdk, {
  position = 'bottom-right', // bottom-right | bottom-left
  label = 'Feedback',
  title = 'Enviar feedback',
  placeholder = 'Descreva o que aconteceu…',
  successMessage = 'Recebido, obrigado!',
} = {}) {
  const rootId = 'ndovu-feedback-widget';
  if (document.getElementById(rootId)) return () => {};

  const root = document.createElement('div');
  root.id = rootId;
  const posStyle = position === 'bottom-left' ? 'left:20px' : 'right:20px';
  root.innerHTML = `
    <button type="button" data-ndovu-open style="
      position:fixed;bottom:20px;${posStyle};z-index:2147483000;
      background:#111827;color:#fff;border:0;border-radius:9999px;
      padding:10px 16px;font:500 13px system-ui,sans-serif;
      box-shadow:0 4px 12px rgba(0,0,0,.2);cursor:pointer;">
      ${escapeHtml(label)}
    </button>
    <div data-ndovu-modal style="
      display:none;position:fixed;inset:0;z-index:2147483001;
      background:rgba(0,0,0,.5);align-items:center;justify-content:center;">
      <form data-ndovu-form style="
        background:#fff;border-radius:12px;padding:20px;width:min(420px,92vw);
        font:14px system-ui,sans-serif;color:#111;box-shadow:0 12px 40px rgba(0,0,0,.3);">
        <h3 style="margin:0 0 12px;font-size:16px;font-weight:600;">${escapeHtml(title)}</h3>
        <label style="display:block;margin-bottom:8px;font-size:12px;color:#555;">Tipo
          <select data-ndovu-type style="display:block;width:100%;margin-top:4px;padding:6px;
            border:1px solid #d1d5db;border-radius:6px;font:inherit;">
            <option value="bug">Bug</option>
            <option value="suggestion">Sugestão</option>
            <option value="praise">Elogio</option>
            <option value="other">Outro</option>
          </select>
        </label>
        <label style="display:block;margin-bottom:8px;font-size:12px;color:#555;">Email (opcional)
          <input data-ndovu-email type="email" style="display:block;width:100%;margin-top:4px;
            padding:6px;border:1px solid #d1d5db;border-radius:6px;font:inherit;box-sizing:border-box;" />
        </label>
        <label style="display:block;margin-bottom:12px;font-size:12px;color:#555;">Mensagem
          <textarea data-ndovu-message required rows="4"
            placeholder="${escapeHtml(placeholder)}"
            style="display:block;width:100%;margin-top:4px;padding:6px;
              border:1px solid #d1d5db;border-radius:6px;font:inherit;resize:vertical;box-sizing:border-box;"></textarea>
        </label>
        <div data-ndovu-status style="min-height:18px;font-size:12px;margin-bottom:8px;"></div>
        <div style="display:flex;gap:8px;justify-content:flex-end;">
          <button type="button" data-ndovu-cancel style="
            background:transparent;border:1px solid #d1d5db;padding:6px 12px;
            border-radius:6px;font:inherit;cursor:pointer;">Cancelar</button>
          <button type="submit" data-ndovu-submit style="
            background:#111827;color:#fff;border:0;padding:6px 14px;
            border-radius:6px;font:inherit;cursor:pointer;">Enviar</button>
        </div>
      </form>
    </div>`;
  document.body.appendChild(root);

  const modal = root.querySelector('[data-ndovu-modal]');
  const form = root.querySelector('[data-ndovu-form]');
  const status = root.querySelector('[data-ndovu-status]');
  const openBtn = root.querySelector('[data-ndovu-open]');
  const cancelBtn = root.querySelector('[data-ndovu-cancel]');
  const submitBtn = root.querySelector('[data-ndovu-submit]');
  const openModal = () => { modal.style.display = 'flex'; status.textContent = ''; };
  const closeModal = () => { modal.style.display = 'none'; };

  openBtn.addEventListener('click', openModal);
  cancelBtn.addEventListener('click', closeModal);
  modal.addEventListener('click', (e) => { if (e.target === modal) closeModal(); });

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const type = root.querySelector('[data-ndovu-type]').value;
    const email = root.querySelector('[data-ndovu-email]').value.trim() || undefined;
    const message = root.querySelector('[data-ndovu-message]').value.trim();
    if (!message) return;
    submitBtn.disabled = true;
    status.style.color = '#555';
    status.textContent = 'Enviando…';
    try {
      await sdk.feedback(message, { type, email });
      status.style.color = '#059669';
      status.textContent = successMessage;
      form.reset();
      setTimeout(closeModal, 1200);
    } catch (err) {
      status.style.color = '#b91c1c';
      status.textContent = 'Falha ao enviar. Tente novamente.';
    } finally {
      submitBtn.disabled = false;
    }
  });

  return () => root.remove();
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));
}

// ---------------------------------------------------------------------------
// Handlers globais — captura de erros JS sem código do app
// ---------------------------------------------------------------------------

function installGlobalHandlers(sdk) {
  if (typeof window === 'undefined') return;

  window.addEventListener('error', (ev) => {
    if (!ev) return;
    const err = ev.error;
    sdk.error(ev.message || 'window.onerror', {
      code: 'JS_ERROR',
      message: err?.message ?? ev.message,
      screen: location.pathname,
      body: {
        stack: err?.stack,
        source: ev.filename,
        line: ev.lineno,
        column: ev.colno,
      },
    });
  });

  window.addEventListener('unhandledrejection', (ev) => {
    const reason = ev?.reason;
    const message = reason?.message ?? String(reason);
    sdk.error('unhandled_rejection', {
      code: 'UNHANDLED_REJECTION',
      message,
      screen: location.pathname,
      body: { stack: reason?.stack, reason: reason ? String(reason) : undefined },
    });
  });

  // console.error/warn: só interceptamos console.error por padrão (warn é ruído).
  const originalError = console.error;
  console.error = (...args) => {
    try {
      const first = args[0];
      const message =
        first instanceof Error ? first.message : args.map(safeString).join(' ');
      sdk.error('console_error', {
        code: 'CONSOLE_ERROR',
        message: truncate(message, 500),
        screen: location.pathname,
        body: { args: args.slice(0, 5).map(safeString) },
      });
    } catch {
      // não pode quebrar o console do app
    }
    originalError.apply(console, args);
  };
}

function safeString(v) {
  if (v instanceof Error) return `${v.name}: ${v.message}`;
  if (typeof v === 'string') return v;
  try {
    return JSON.stringify(v);
  } catch {
    return String(v);
  }
}

function truncate(s, n) {
  return s && s.length > n ? s.slice(0, n) + '…' : s;
}

// ---------------------------------------------------------------------------
// Web Vitals — CLS, LCP, INP, FID, TTFB, FCP via PerformanceObserver
// ---------------------------------------------------------------------------

const VITAL_THRESHOLDS = {
  LCP: [2500, 4000],
  INP: [200, 500],
  FID: [100, 300],
  CLS: [0.1, 0.25],
  TTFB: [800, 1800],
  FCP: [1800, 3000],
};

function rating(name, value) {
  const t = VITAL_THRESHOLDS[name];
  if (!t) return 'unknown';
  if (value <= t[0]) return 'good';
  if (value <= t[1]) return 'needs-improvement';
  return 'poor';
}

function emitVital(sdk, name, value, extra = {}) {
  sdk.custom(`web_vital_${name.toLowerCase()}`, {
    vital: name,
    value: Math.round(value * 1000) / 1000,
    rating: rating(name, value),
    screen: location.pathname,
    ...extra,
  });
}

function installWebVitals(sdk) {
  if (typeof PerformanceObserver === 'undefined') return;

  // TTFB e FCP saem do Navigation/Paint timings (disponíveis logo após load)
  try {
    const nav = performance.getEntriesByType('navigation')[0];
    if (nav && nav.responseStart > 0) emitVital(sdk, 'TTFB', nav.responseStart);
    for (const p of performance.getEntriesByType('paint')) {
      if (p.name === 'first-contentful-paint') emitVital(sdk, 'FCP', p.startTime);
    }
  } catch {
    // ambientes sem Navigation Timing
  }

  observeVital('largest-contentful-paint', (entries) => {
    const last = entries[entries.length - 1];
    if (last) emitVital(sdk, 'LCP', last.startTime);
  });

  // CLS: soma sessões de layout shift ignorando shifts causados por input
  let clsValue = 0;
  observeVital('layout-shift', (entries) => {
    for (const e of entries) {
      if (!e.hadRecentInput) clsValue += e.value;
    }
    emitVital(sdk, 'CLS', clsValue);
  });

  // FID (primeiro delay de input)
  observeVital(
    'first-input',
    (entries) => {
      const first = entries[0];
      if (first) emitVital(sdk, 'FID', first.processingStart - first.startTime);
    },
    { once: true },
  );

  // INP (aproximação: pega o pior event timing > 40ms)
  let worstInp = 0;
  observeVital('event', (entries) => {
    for (const e of entries) {
      if (e.duration > worstInp && e.duration >= 40) {
        worstInp = e.duration;
        emitVital(sdk, 'INP', worstInp, { eventName: e.name });
      }
    }
  }, { durationThreshold: 40 });
}

function observeVital(type, cb, options = {}) {
  try {
    const observer = new PerformanceObserver((list) => cb(list.getEntries()));
    const init = { type, buffered: true, ...options };
    delete init.once;
    observer.observe(init);
    if (options.once) {
      // desliga após primeira entrega para não gerar spam
      setTimeout(() => observer.disconnect(), 60000);
    }
  } catch {
    // navegador sem suporte a esse tipo — silenciamos (best-effort)
  }
}

// ---------------------------------------------------------------------------
// Session replay MVP — snapshot HTML sanitizado no momento do error
// ---------------------------------------------------------------------------

/**
 * captureDOM devolve outerHTML do <html>, mas com:
 *   - value de <input type="password" | type="email" | ...> mascarado
 *   - conteúdo de elementos com atributo data-ndovu-mask substituído por ***
 *   - <script> removidos (evita exec no iframe do dashboard)
 *
 * Não redige texto genérico — o dev que marca com data-ndovu-mask onde
 * há PII fora de inputs (ex.: <span data-ndovu-mask>CPF</span>).
 */
function captureDOM() {
  if (typeof document === 'undefined' || !document.documentElement) return '';
  // Clona pra não mexer no DOM real.
  const clone = document.documentElement.cloneNode(true);
  // 1. Remove scripts inteiros (o replay é passivo, script não roda).
  clone.querySelectorAll('script, iframe').forEach((n) => n.remove());
  // 2. Mascara inputs sensíveis. `value` do input não vai pro outerHTML
  //    a menos que a gente escreva no atributo — cuidamos disso aqui.
  clone.querySelectorAll('input, textarea').forEach((el) => {
    const type = (el.getAttribute('type') || '').toLowerCase();
    const isSensitive = ['password', 'email', 'tel', 'cc-number', 'cc-csc'].includes(type)
      || el.hasAttribute('data-ndovu-mask');
    if (isSensitive) {
      el.setAttribute('value', '***');
    } else if (el.value != null) {
      // Preserva o valor visível ao snapshot (senão o dashboard mostra input vazio).
      el.setAttribute('value', String(el.value).slice(0, 200));
    }
  });
  // 3. Mascara texto de qualquer elemento marcado explicitamente.
  clone.querySelectorAll('[data-ndovu-mask]').forEach((el) => {
    el.textContent = '***';
  });
  return '<!doctype html>' + clone.outerHTML;
}

async function sendSnapshot(endpoint, apiKey, app, sessionId, eventId) {
  try {
    const html = captureDOM();
    if (!html) return;
    const body = JSON.stringify({
      eventId,
      sessionId,
      app,
      html,
      url: location.href,
      viewportW: window.innerWidth,
      viewportH: window.innerHeight,
      takenAt: new Date().toISOString(),
    });
    await fetch(`${endpoint.replace(/\/$/, '')}/v1/snapshots`, {
      method: 'POST',
      keepalive: true,
      headers: { 'Content-Type': 'application/json', 'X-Api-Key': apiKey },
      body,
    });
  } catch {
    // Snapshot é best-effort; erro aqui não deve afetar o app.
  }
}
