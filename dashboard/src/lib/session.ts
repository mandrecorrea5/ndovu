import 'server-only';

import { createCipheriv, createDecipheriv, randomBytes } from 'node:crypto';

/**
 * Sessão do BFF (server-side).
 *
 * O JWT da API é guardado dentro de um cookie httpOnly + Secure + SameSite=Lax,
 * cifrado com AES-256-GCM. A chave de cifra vem de NDOVU_SESSION_ENC_KEY e só
 * existe no processo do servidor Next: um cookie roubado não decifra a sessão
 * sem ela, e o token nunca chega ao JavaScript do browser.
 *
 * Formato do cookie: base64url( iv(12) || ciphertext+tag )
 *
 * Além do token, a sessão guarda a identidade (id/email/name/role) para o
 * AppShell renderizar sem request extra. Cada request autenticada revalida
 * com a API via /v1/auth/me — revogação/ BAN do usuário tem efeito imediato
 * (não apenas no exp do JWT).
 */

export const SESSION_COOKIE = 'ndovu_session';
const SESSION_TTL_SECONDS = 8 * 3600;

export interface SessionData {
  token: string;
  expiresAt: string;
  user: {
    id: string;
    email: string;
    name: string;
    role: 'admin' | 'editor' | 'viewer';
  };
}

function encryptionKey(): Buffer {
  const raw = process.env.NDOVU_SESSION_ENC_KEY ?? process.env.NDOVU_AUTH_SECRET;
  const normalized = (raw ?? '').trim();
  // 32 bytes = 256 bits, o tamanho de chave exigido pelo AES-256-GCM.
  const key = Buffer.from(normalized);
  if (key.length < 32) {
    throw new Error(
      'NDOVU_SESSION_ENC_KEY ausente ou curto demais (mínimo 32 bytes). Gere com `openssl rand -hex 16`.',
    );
  }
  return key.subarray(0, 32);
}

function b64url(buf: Buffer): string {
  return buf.toString('base64url');
}

export function encryptSession(data: SessionData): string {
  const iv = randomBytes(12);
  const cipher = createCipheriv('aes-256-gcm', encryptionKey(), iv);
  const plaintext = Buffer.from(JSON.stringify(data), 'utf8');
  const ciphertext = Buffer.concat([cipher.update(plaintext), cipher.final()]);
  const tag = cipher.getAuthTag();
  return b64url(Buffer.concat([iv, tag, ciphertext]));
}

export function decryptSession(value: string): SessionData | null {
  let raw: Buffer;
  try {
    raw = Buffer.from(value, 'base64url');
  } catch {
    return null;
  }
  // iv(12) + tag(16) + ciphertext
  if (raw.length < 29) return null;
  try {
    const iv = raw.subarray(0, 12);
    const tag = raw.subarray(12, 28);
    const ciphertext = raw.subarray(28);
    const decipher = createDecipheriv('aes-256-gcm', encryptionKey(), iv);
    decipher.setAuthTag(tag);
    const plaintext = Buffer.concat([decipher.update(ciphertext), decipher.final()]);
    const parsed = JSON.parse(plaintext.toString('utf8')) as SessionData;
    if (!parsed?.token || !parsed?.user?.id) return null;
    return parsed;
  } catch {
    // AuthTag inválido = cookie adulterado ou chave trocada.
    return null;
  }
}

/** CookieOptions usados em produção (Secure + SameSite=Lax). */
export const sessionCookieOptions = {
  httpOnly: true,
  secure: true,
  sameSite: 'lax' as const,
  path: '/',
  maxAge: SESSION_TTL_SECONDS,
};

export const clearedCookieOptions = {
  httpOnly: true,
  secure: true,
  sameSite: 'lax' as const,
  path: '/',
  maxAge: 0,
};
