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

/**
 * APIClient autenticado via BFF (mesmo caminho do browser).
 *
 * O dashboard não aceita mais token no header: a sessão é o cookie httpOnly
 * emitido em /api/auth/login. `loginAsBFF` faz esse login uma vez e devolve o
 * contexto com o cookie guardado — todos os requests autenticados passam por
 * ele (com o cookie), e os dados são lidos pela API de consulta via BFF.
 */
export class APIClient {
  constructor(
    private request: APIRequestContext,
    private token: string,
  ) {}

  get authHeaders() {
    return { Authorization: `Bearer ${this.token}` };
  }

  /**
   * BFF path: /v1/... do contrato vira /api/... (mesmo mapeamento do client).
   * Ex.: /v1/admin/users → /api/admin/users.
   */
  private bff(path: string): string {
    const dashboardBase = process.env.NDOVU_DASHBOARD ?? 'http://localhost:13000';
    return `${dashboardBase}${path.replace(/^\/v1/, '/api')}`;
  }

  async post(path: string, data: unknown): Promise<any> {
    const resp = await this.request.post(this.bff(path), {
      headers: this.authHeaders,
      data,
    });
    if (!resp.ok()) {
      throw new Error(`POST ${path}: ${resp.status()} ${await resp.text()}`);
    }
    const text = await resp.text();
    return text ? JSON.parse(text) : undefined;
  }

  async get(path: string): Promise<any> {
    const resp = await this.request.get(this.bff(path), {
      headers: this.authHeaders,
    });
    if (!resp.ok()) {
      throw new Error(`GET ${path}: ${resp.status()} ${await resp.text()}`);
    }
    const text = await resp.text();
    return text ? JSON.parse(text) : undefined;
  }

  async patch(path: string, data: unknown): Promise<any> {
    const resp = await this.request.patch(this.bff(path), {
      headers: this.authHeaders,
      data,
    });
    if (!resp.ok()) {
      throw new Error(`PATCH ${path}: ${resp.status()} ${await resp.text()}`);
    }
    const text = await resp.text();
    return text ? JSON.parse(text) : undefined;
  }

  async put(path: string, data: unknown): Promise<any> {
    const resp = await this.request.put(this.bff(path), {
      headers: this.authHeaders,
      data,
    });
    if (!resp.ok()) {
      throw new Error(`PUT ${path}: ${resp.status()} ${await resp.text()}`);
    }
    const text = await resp.text();
    return text ? JSON.parse(text) : undefined;
  }

  async delete(path: string): Promise<void> {
    // Idempotente por definição: se falhar com 404, ignora (recurso já foi).
    const resp = await this.request.delete(this.bff(path), {
      headers: this.authHeaders,
    });
    if (!resp.ok() && resp.status() !== 404) {
      // Cleanup best-effort: log em stderr mas não falha o teste.
      console.warn(`cleanup DELETE ${path}: ${resp.status()}`);
    }
  }
}

/**
 * newAdminClient devolve um APIClient autenticado via BFF (cookie httpOnly).
 * Login via /api/auth/login no dashboard — usa a mesma sessão que a UI usa.
 */
export async function newAdminClient(
  request: APIRequestContext,
): Promise<APIClient> {
  return newBFFClient(request, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
}

export async function newBFFClient(
  request: APIRequestContext,
  email: string,
  password: string,
): Promise<APIClient> {
  const dashboardBase = process.env.NDOVU_DASHBOARD ?? 'http://localhost:13000';
  const resp = await request.post(`${dashboardBase}/api/auth/login`, {
    data: { email, password },
  });
  if (!resp.ok()) {
    throw new Error(`login BFF ${email}: ${resp.status()} ${await resp.text()}`);
  }
  // A sessão BFF é o cookie (storageState do request context). Guardamos um
  // "token" placeholder só para manter a API do helper igual; o cookie viaja
  // sozinho em toda request do mesmo request context.
  return new APIClient(request, 'bff-session');
}
