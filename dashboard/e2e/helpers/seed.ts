// Helpers de seed: criam dados via API antes do teste, devolvem o
// suficiente pra o teste interagir + uma função `cleanup` pra reverter.
//
// Cada função devolve `{ resource, cleanup: () => Promise<void> }`.
// O teste faz `cleanupList.push(res.cleanup)` — a fixture roda tudo em
// LIFO no afterEach.

import { APIClient } from './api';

// Prefixo único por run pra evitar colisão de nomes entre execuções.
const RUN_ID = Date.now().toString(36) + Math.random().toString(36).slice(2, 6);

export function unique(prefix: string): string {
  return `${prefix}-${RUN_ID}-${Math.random().toString(36).slice(2, 8)}`;
}

export type CleanupFn = () => Promise<void>;
export type Seeded<T> = { resource: T; cleanup: CleanupFn };

export async function seedCompany(
  api: APIClient,
  name?: string,
): Promise<Seeded<{ id: string; name: string }>> {
  const n = name ?? unique('company');
  const c = await api.post('/v1/admin/companies', { name: n, active: true });
  return {
    resource: { id: c.id, name: c.name },
    cleanup: () => api.delete(`/v1/admin/companies/${c.id}`),
  };
}

export async function seedApp(
  api: APIClient,
  companyID: string,
  name?: string,
): Promise<Seeded<{ id: string; name: string; key: string }>> {
  const n = name ?? unique('app');
  const app = await api.post('/v1/admin/apps', {
    name: n, companyId: companyID, technology: 'test',
  });
  return {
    resource: { id: app.id, name: app.name, key: app.key },
    cleanup: () => api.delete(`/v1/admin/apps/${app.id}`),
  };
}

export async function seedUser(
  api: APIClient,
  companyID: string,
  role: 'admin' | 'editor' | 'viewer',
  password: string = 'test-pass-1234',
  email?: string,
): Promise<Seeded<{ id: string; email: string; password: string; role: string }>> {
  const em = email ?? `${role}-${unique('u')}@e2e.local`;
  const user = await api.post('/v1/admin/users', {
    email: em, name: em, password, role, companyId: companyID, active: true,
  });
  return {
    resource: { id: user.id, email: em, password, role: user.role },
    // Não existe DELETE user na API (só PATCH active=false). "Cleanup"
    // aqui = desativar; usuário fica no banco mas isolado do próximo run
    // (email único garante isso via UNIQUE em INSERT + `unique()` no email).
    cleanup: async () => {
      try {
        await api.patch(`/v1/admin/users/${user.id}`, { active: false });
      } catch {
        // best effort
      }
    },
  };
}

/**
 * Ingest 1 evento de teste via API (usa a chave do app). Não retorna
 * cleanup — eventos ficam no ClickHouse mas o TTL de 90d limpa depois.
 */
export async function ingestEvent(
  request: import('@playwright/test').APIRequestContext,
  appName: string,
  apiKey: string,
  name: string,
): Promise<void> {
  const resp = await request.post('http://localhost:18081/v1/events', {
    headers: { 'X-Api-Key': apiKey },
    data: {
      app: appName,
      session: { sessionId: crypto.randomUUID(), userId: 'u-e2e' },
      events: [{
        eventId: crypto.randomUUID(),
        type: 'action',
        name,
        timestamp: new Date().toISOString(),
      }],
    },
  });
  if (!resp.ok()) {
    throw new Error(`ingest event: ${resp.status()} ${await resp.text()}`);
  }
}
