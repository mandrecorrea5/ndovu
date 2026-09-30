/**
 * Sessão do browser — apenas um reflexo em memória da sessão verdadeira.
 *
 * A sessão real vive num cookie httpOnly + Secure + SameSite=Lax emitido pelo
 * BFF (`/api/auth/login`) e cifrado com AES-GCM no servidor. O token JWT
 * NUNCA é exposto ao JavaScript do browser: nenhuma rota client consome
 * credencial. `getSessionUser()` é cache para render; a autoridade é sempre o
 * BFF, que revalida com a API a cada request.
 *
 * Em caso de 401, chamamos `clearSession()` + redirect: o BFF já limpou o
 * cookie quando a API respondeu 401.
 */

export type Role = 'admin' | 'editor' | 'viewer';

export interface SessionUser {
  id: string;
  email: string;
  name: string;
  role: Role;
}

export function canAdmin(user: SessionUser | null): boolean {
  return user?.role === 'admin';
}

export function canEdit(user: SessionUser | null): boolean {
  return user?.role === 'admin' || user?.role === 'editor';
}

export function canShareViews(user: SessionUser | null): boolean {
  return canEdit(user);
}

const USER_KEY = 'ndovu.user';

// Cache em memória (aba). Não é fonte de verdade: /api/auth/me confirma.
let memoryUser: SessionUser | null | undefined;

export function getSessionUser(): SessionUser | null {
  if (memoryUser !== undefined) return memoryUser;
  try {
    const raw = sessionStorage.getItem(USER_KEY);
    memoryUser = raw ? (JSON.parse(raw) as SessionUser) : null;
  } catch {
    memoryUser = null;
  }
  return memoryUser;
}

export function setCachedUser(user: SessionUser | null): void {
  memoryUser = user;
  try {
    if (user) sessionStorage.setItem(USER_KEY, JSON.stringify(user));
    else sessionStorage.removeItem(USER_KEY);
  } catch {
    /* modo privado / storage cheio — sessão segue viva via cookie */
  }
}

export function clearSession(): void {
  setCachedUser(null);
}

export interface LoginResponseUser {
  user: SessionUser;
  expiresAt: string;
}

/** login(): chama o BFF, cacheia a identidade e devolve a sessão. */
export async function login(email: string, password: string): Promise<SessionUser> {
  const res = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
    credentials: 'same-origin',
  });
  const payload = (await res.json().catch(() => ({}))) as {
    error?: string;
    user?: SessionUser;
  };
  if (!res.ok) {
    throw new ApiErrorLite(payload.error ?? 'Falha no login', res.status);
  }
  const user = payload.user as SessionUser;
  setCachedUser(user);
  return user;
}

export class ApiErrorLite extends Error {
  constructor(
    message: string,
    readonly status?: number,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

/**
 * fetchSession confirma a sessão com o BFF (que revalida na API) e
 * sincroniza o cache de identidade.
 */
export async function fetchSession(): Promise<SessionUser | null> {
  const res = await fetch('/api/auth/me', {
    credentials: 'same-origin',
    cache: 'no-store',
  });
  if (res.status === 401) {
    clearSession();
    return null;
  }
  if (!res.ok) return getSessionUser();
  const payload = (await res.json().catch(() => ({}))) as { user?: SessionUser };
  if (!payload.user) {
    clearSession();
    return null;
  }
  setCachedUser(payload.user);
  return payload.user;
}

/**
 * logout(): pede ao BFF para limpar o cookie. Mesmo se a chamada falhar, o
 * cache é limpo — a sessão no servidor pode viver até o exp do JWT, mas
 * nada no client a usa mais.
 */
export async function logout(): Promise<void> {
  try {
    await fetch('/api/auth/logout', { method: 'POST', credentials: 'same-origin' });
  } finally {
    clearSession();
  }
}

export function redirectToLogin(): void {
  if (typeof window === 'undefined') return;
  const next = encodeURIComponent(window.location.pathname + window.location.search);
  window.location.assign(`/login?next=${next}`);
}
