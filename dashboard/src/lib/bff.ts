import 'server-only';

import { cookies } from 'next/headers';
import {
  SESSION_COOKIE,
  clearedCookieOptions,
  decryptSession,
  encryptSession,
  sessionCookieOptions,
  type SessionData,
} from './session';

// Server-to-server. Em produção (rede do compose) a URL é http://api:8080 —
// só a Caddy publica TLS. Em dev local fora do Docker, defina NDOVU_API.
const API_BASE =
  process.env.NDOVU_API_INTERNAL ??
  process.env.NDOVU_API ??
  (process.env.NODE_ENV === 'production' ? 'http://api:8080' : 'http://localhost:18081');

export class UpstreamError extends Error {
  constructor(
    readonly status: number,
    message: string,
    readonly details?: string[],
    readonly body?: unknown,
  ) {
    super(message);
    this.name = 'UpstreamError';
  }
}

async function upstream<T>(
  method: string,
  path: string,
  opts: { token?: string; body?: unknown; query?: URLSearchParams } = {},
): Promise<T> {
  const url = new URL(path, API_BASE);
  if (opts.query) {
    for (const [k, v] of opts.query.entries()) url.searchParams.set(k, v);
  }
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`;

  let res: Response;
  try {
    res = await fetch(url.toString(), {
      method,
      headers,
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      cache: 'no-store',
    });
  } catch {
    throw new UpstreamError(502, 'API indisponível — verifique se a Ndovu API está no ar.');
  }

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  const parsed = text ? safeJson(text) : undefined;

  if (!res.ok) {
    const payload = (parsed ?? {}) as { error?: string; details?: string[] };
    throw new UpstreamError(res.status, payload.error ?? `HTTP ${res.status}`, payload.details, parsed);
  }
  return parsed as T;
}

function safeJson(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

// ---------------------------------------------------------------------------
// Sessão (cookie httpOnly cifrado)
// ---------------------------------------------------------------------------

export async function readSession(): Promise<SessionData | null> {
  const store = await cookies();
  const raw = store.get(SESSION_COOKIE)?.value;
  if (!raw) return null;
  const session = decryptSession(raw);
  if (!session) return null;
  // JWT expirado localmente evita um round-trip inútil à API.
  const exp = Date.parse(session.expiresAt);
  if (!Number.isNaN(exp) && exp <= Date.now()) return null;
  return session;
}

export async function writeSession(session: SessionData): Promise<void> {
  const store = await cookies();
  store.set(SESSION_COOKIE, encryptSession(session), sessionCookieOptions);
}

export async function clearSessionCookie(): Promise<void> {
  const store = await cookies();
  store.set(SESSION_COOKIE, '', clearedCookieOptions);
}

/**
 * requireSession revalida a sessão na API a cada request. Custo: 1 chamada
 * interna por request do browser — em troca, desativação de usuário e
 * revogação têm efeito imediato e o 401 é detectado antes de vazar dados.
 */
export async function requireSession(): Promise<SessionData> {
  const session = await readSession();
  if (!session) throw new UpstreamError(401, 'Sessão expirada — faça login novamente.');
  try {
    await upstream('GET', '/v1/auth/me', { token: session.token });
  } catch (err) {
    if (err instanceof UpstreamError && (err.status === 401 || err.status === 403)) {
      await clearSessionCookie();
      throw new UpstreamError(401, 'Sessão expirada — faça login novamente.');
    }
    throw err;
  }
  return session;
}

// ---------------------------------------------------------------------------
// Endpoints
// ---------------------------------------------------------------------------

export interface LoginResult {
  token: string;
  expiresAt: string;
  user: SessionData['user'] & { active: boolean; createdAt: string };
}

export async function login(email: string, password: string): Promise<SessionData> {
  const result = await upstream<LoginResult>('POST', '/v1/auth/login', {
    body: { email, password },
  });
  if (!result?.token) throw new UpstreamError(502, 'API respondeu ao login sem token.');
  const session: SessionData = {
    token: result.token,
    expiresAt: result.expiresAt,
    user: {
      id: result.user.id,
      email: result.user.email,
      name: result.user.name,
      role: result.user.role,
    },
  };
  await writeSession(session);
  return session;
}

export { upstream };
