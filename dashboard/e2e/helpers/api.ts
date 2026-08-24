// Cliente API para testes E2E. Usa APIRequestContext do Playwright para
// falar direto com a API do ndovu (http://localhost:18081) — sem passar
// pelo browser. Muito mais rápido pra setup/cleanup do que interagir
// com a UI.

import type { APIRequestContext } from '@playwright/test';

export const API_BASE = process.env.NDOVU_API ?? 'http://localhost:18081';

export const BOOTSTRAP_ADMIN = {
  email: 'admin@ndovu.local',
  password: 'admin12345',
};

/** Faz login e devolve o token JWT. Falha o teste se der erro. */
export async function loginAPI(
  request: APIRequestContext,
  email: string,
  password: string,
): Promise<string> {
  const resp = await request.post(`${API_BASE}/v1/auth/login`, {
    data: { email, password },
  });
  if (!resp.ok()) {
    throw new Error(`login ${email}: ${resp.status()} ${await resp.text()}`);
  }
  const body = await resp.json();
  return body.token as string;
}

/** APIClient encapsula um Bearer token + helpers CRUD. */
export class APIClient {
  constructor(
    private request: APIRequestContext,
    private token: string,
  ) {}

  get authHeaders() {
    return { Authorization: `Bearer ${this.token}` };
  }

  async post(path: string, data: unknown): Promise<any> {
    const resp = await this.request.post(`${API_BASE}${path}`, {
      headers: this.authHeaders,
      data,
    });
    if (!resp.ok()) {
      throw new Error(`POST ${path}: ${resp.status()} ${await resp.text()}`);
    }
    return resp.json();
  }

  async get(path: string): Promise<any> {
    const resp = await this.request.get(`${API_BASE}${path}`, {
      headers: this.authHeaders,
    });
    if (!resp.ok()) {
      throw new Error(`GET ${path}: ${resp.status()} ${await resp.text()}`);
    }
    return resp.json();
  }

  async patch(path: string, data: unknown): Promise<any> {
    const resp = await this.request.patch(`${API_BASE}${path}`, {
      headers: this.authHeaders,
      data,
    });
    if (!resp.ok()) {
      throw new Error(`PATCH ${path}: ${resp.status()} ${await resp.text()}`);
    }
    return resp.json();
  }

  async delete(path: string): Promise<void> {
    // Idempotente por definição: se falhar com 404, ignora (recurso já foi).
    const resp = await this.request.delete(`${API_BASE}${path}`, {
      headers: this.authHeaders,
    });
    if (!resp.ok() && resp.status() !== 404) {
      // Cleanup best-effort: log em stderr mas não falha o teste.
      console.warn(`cleanup DELETE ${path}: ${resp.status()}`);
    }
  }
}

/**
 * newAdminClient devolve um APIClient logado como super-admin bootstrap.
 * Usado para setup (criar company, user, app) — nunca dentro da UI.
 */
export async function newAdminClient(
  request: APIRequestContext,
): Promise<APIClient> {
  const token = await loginAPI(request, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
  return new APIClient(request, token);
}
