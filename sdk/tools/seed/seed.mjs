#!/usr/bin/env node
/**
 * Seed do Ndovu: simula sessões realistas de vários frontends enviando
 * traces pelo MESMO contrato v1 que qualquer app usaria.
 *
 * Uso:
 *   node tools/seed/seed.mjs [--endpoint http://localhost:8080] [--key dev-ingest-key] [--sessions 40]
 */

import { randomUUID } from 'node:crypto';

const args = Object.fromEntries(
  process.argv.slice(2).map((a, i, all) => (a.startsWith('--') ? [a.slice(2), all[i + 1]] : [])).filter((p) => p.length),
);
const ENDPOINT = args.endpoint ?? 'http://localhost:8080';
const API_KEY = args.key ?? 'dev-ingest-key';
const SESSIONS = Number(args.sessions ?? 40);

const APPS = ['portal-cliente', 'app-mobile', 'backoffice-web'];
const USERS = Array.from({ length: 15 }, (_, i) => `user-${100 + i}`);

const rnd = (arr) => arr[Math.floor(Math.random() * arr.length)];
const between = (min, max) => Math.floor(min + Math.random() * (max - min));

/** Jornadas de negócio simuladas (telas + chamadas + erros plausíveis). */
const JOURNEYS = [
  {
    feature: 'login',
    steps: (t) => [
      pageView(t(0), 'tela_login', '/login'),
      http(t(2), 'login', '/login', 'POST', '/api/auth/login',
        { email: 'cliente@email.com', password: '***' },
        Math.random() < 0.12
          ? { fail: { status: 401, code: 'AUTH_INVALID', message: 'Credenciais inválidas', body: { attempt: 1 } } }
          : { ok: { token: '***', nome: 'Cliente' } }),
    ],
  },
  {
    feature: 'faturas',
    steps: (t) => [
      pageView(t(0), 'tela_faturas', '/faturas'),
      http(t(1), 'listar_faturas', '/faturas', 'GET', '/api/v2/faturas', undefined,
        { ok: { faturas: [{ id: 'F-01', valor: 189.9 }, { id: 'F-02', valor: 189.9 }] } }),
      pageView(t(4), 'tela_segunda_via', '/faturas/segunda-via'),
      http(t(6), 'segunda_via', '/faturas/segunda-via', 'POST', '/api/v2/faturas/segunda-via',
        { contratoId: String(between(100, 999)), mes: '2026-08' },
        Math.random() < 0.2
          ? { fail: { status: 500, code: 'FATURA_TIMEOUT', message: 'Timeout ao gerar fatura', body: { upstream: 'billing-svc', timeoutMs: 5000 }, slow: true } }
          : { ok: { faturaId: `F-2026-08-${between(100, 999)}`, url: '/download/f.pdf' } }),
    ],
  },
  {
    feature: 'pagamentos',
    steps: (t) => [
      pageView(t(0), 'tela_pagamento', '/pagamentos'),
      action(t(3), 'selecionou_pix', '/pagamentos'),
      http(t(5), 'gerar_pix', '/pagamentos', 'POST', '/api/v1/pagamentos/pix',
        { faturaId: 'F-01', valor: 189.9 },
        Math.random() < 0.08
          ? { fail: { status: 422, code: 'PIX_INDISPONIVEL', message: 'PSP indisponível', body: { psp: 'psp-x' } } }
          : { ok: { qrcode: '000201...', expiraEm: 3600 } }),
    ],
  },
  {
    feature: 'cadastro',
    steps: (t) => [
      pageView(t(0), 'tela_perfil', '/perfil'),
      http(t(2), 'carregar_perfil', '/perfil', 'GET', '/api/v1/perfil', undefined,
        { ok: { nome: 'Cliente', email: 'c@email.com' } }),
      action(t(8), 'editou_telefone', '/perfil'),
      http(t(10), 'salvar_perfil', '/perfil', 'PUT', '/api/v1/perfil',
        { telefone: '+55 85 9xxxx-xxxx' },
        Math.random() < 0.05
          ? { fail: { status: 400, code: 'TELEFONE_INVALIDO', message: 'Telefone em formato inválido', body: { campo: 'telefone' } } }
          : { ok: { atualizado: true } }),
    ],
  },
];

function pageView(ts, name, screen) {
  return { eventId: randomUUID(), type: 'page_view', name, screen, timestamp: ts };
}

function action(ts, name, screen) {
  return { eventId: randomUUID(), type: 'action', name, screen, timestamp: ts, metadata: { origem: 'seed' } };
}

function http(ts, name, screen, method, url, requestBody, outcome) {
  const base = { eventId: randomUUID(), type: 'http_request', name, screen, timestamp: ts };
  if (outcome.ok) {
    return {
      ...base,
      durationMs: between(80, 900),
      http: { method, url, statusCode: 200, requestBody, responseBody: outcome.ok },
    };
  }
  const f = outcome.fail;
  return {
    ...base,
    durationMs: f.slow ? between(4500, 6000) : between(120, 1200),
    http: { method, url, statusCode: f.status, requestBody, responseBody: f.body },
    error: { code: f.code, message: f.message, body: f.body },
  };
}

async function sendSession() {
  const app = rnd(APPS);
  const userId = rnd(USERS);
  const sessionId = randomUUID();
  // Sessões espalhadas nas últimas 24h
  const start = Date.now() - between(0, 24 * 3600_000);
  const t = (offsetSec) => new Date(start + offsetSec * 1000).toISOString();

  const journeyCount = between(1, 4);
  const events = [];
  let clock = 0;
  for (let j = 0; j < journeyCount; j++) {
    const journey = rnd(JOURNEYS);
    const shifted = (sec) => t(clock + sec);
    events.push(...journey.steps(shifted));
    clock += between(30, 180);
  }

  const res = await fetch(`${ENDPOINT}/v1/events`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Api-Key': API_KEY },
    body: JSON.stringify({
      app,
      sdkVersion: 'seed/1.0.0',
      session: {
        sessionId,
        userId,
        userAgent: 'NdovuSeed/1.0 (simulado)',
        attributes: { canal: app === 'app-mobile' ? 'mobile' : 'web', origem: 'seed' },
      },
      events,
    }),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`HTTP ${res.status}: ${body}`);
  }
  return res.json();
}

let accepted = 0;
for (let i = 0; i < SESSIONS; i++) {
  const r = await sendSession();
  accepted += r.accepted;
}
console.log(`✔ ${SESSIONS} sessões simuladas, ${accepted} eventos aceitos em ${ENDPOINT}`);
