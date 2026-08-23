/**
 * Sessão do usuário no browser.
 *
 * O token JWT fica em localStorage (app interno atrás de login). Em produção
 * exposta à internet, considere trocar por cookie httpOnly emitido por um BFF.
 */

export type Role = 'admin' | 'editor' | 'viewer';

export interface SessionUser {
  id: string;
  email: string;
  name: string;
  role: Role;
}

/**
 * Helpers de autorização por role. Mantidos como funções puras para uso
 * fora do React (utilitário) e dentro (via useSessionUser + memo).
 *
 * Regra:
 *   admin  → tudo
 *   editor → tudo do viewer + escrever funnels/issues + saved views compartilhadas
 *   viewer → só leitura + saved views próprias
 */
export function canAdmin(user: SessionUser | null): boolean {
  return user?.role === 'admin';
}

export function canEdit(user: SessionUser | null): boolean {
  return user?.role === 'admin' || user?.role === 'editor';
}

export function canShareViews(user: SessionUser | null): boolean {
  return canEdit(user);
}

const TOKEN_KEY = 'ndovu.token';
const USER_KEY = 'ndovu.user';

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function getSessionUser(): SessionUser | null {
  try {
    const raw = localStorage.getItem(USER_KEY);
    return raw ? (JSON.parse(raw) as SessionUser) : null;
  } catch {
    return null;
  }
}

export function saveSession(token: string, user: SessionUser): void {
  try {
    localStorage.setItem(TOKEN_KEY, token);
    localStorage.setItem(USER_KEY, JSON.stringify(user));
  } catch {
    // sem storage disponível: a sessão só dura a navegação atual
  }
}

export function clearSession(): void {
  try {
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(USER_KEY);
  } catch {
    // ignore
  }
}

/** Redireciona para o login preservando a rota de origem. */
export function redirectToLogin(): void {
  if (typeof window !== 'undefined' && !window.location.pathname.startsWith('/login')) {
    const next = encodeURIComponent(window.location.pathname + window.location.search);
    window.location.href = `/login?next=${next}`;
  }
}
