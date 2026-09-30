import { clearSession, redirectToLogin, type SessionUser } from './auth';
import type {
  AlertRule,
  AnomalyDetection,
  AnomalyRule,
  App,
  AuditEntry,
  Company,
  CreatedApp,
  EventPage,
  EventQuery,
  FilterOptions,
  Funnel,
  FunnelResult,
  FunnelStep,
  Issue,
  IssueComment,
  Overview,
  Release,
  ReleaseComparison,
  RetentionResult,
  SavedView,
  SessionDetail,
  SessionPage,
  SourceMap,
  SamplingRule,
  SnapshotMeta,
  StackFrame,
  TraceEvent,
  UserAppPermission,
  UserFeedback,
  WebVitalStat,
} from './types';

export type {
  AlertRule,
  AnomalyDetection,
  AnomalyRule,
  App,
  AuditEntry,
  Company,
  CreatedApp,
  SamplingRule,
  SnapshotMeta,
  UserAppPermission,
  UserFeedback,
  Funnel,
  FunnelResult,
  FunnelStep,
  FunnelStepMatch,
  Issue,
  IssueComment,
  Release,
  ReleaseComparison,
  RetentionResult,
  SavedView,
  SourceMap,
  StackFrame,
  WebVitalStat,
} from './types';

// Mesma origem: o browser fala com o BFF do Next (/api/*) e o BFF fala com a
// API Go server-to-server. O token nunca passa pelo browser.
const BASE_URL = '';

/** Erro tipado da API para a UI diferenciar rede/contrato/servidor. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status?: number,
    readonly details?: string[],
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

async function request<T>(
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  opts: { params?: Record<string, string | undefined>; body?: unknown; auth?: boolean } = {},
): Promise<T> {
  const { params, body, auth = true } = opts;
  const url = new URL(bffPath(path), windowLocationOrigin());
  for (const [key, value] of Object.entries(params ?? {})) {
    if (value) url.searchParams.set(key, value);
  }

  const headers: Record<string, string> = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';

  let res: Response;
  try {
    res = await fetch(url.toString(), {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
      // BFF responde com cache-control: no-store; same-origin já manda cookie.
      credentials: 'same-origin',
    });
  } catch {
    throw new ApiError('API indisponível — verifique se a Ndovu API está no ar.');
  }

  if (res.status === 401 && auth) {
    clearSession();
    redirectToLogin();
    throw new ApiError('Sessão expirada — faça login novamente.', 401);
  }
  if (!res.ok) {
    const payload = await res.json().catch(() => ({}) as { error?: string; details?: string[] });
    throw new ApiError(payload.error ?? `HTTP ${res.status}`, res.status, payload.details);
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

/** Mapeia o path da API (legado) para a rota equivalente do BFF (/api/*). */
function bffPath(path: string): string {
  // /v1/auth/* já está montado como /api/auth/*
  const stripped = path.replace(/^\/v1/, '');
  const base = BASE_URL || (typeof window !== 'undefined' ? window.location.origin : 'http://localhost:3000');
  return new URL(`/api${stripped}`, base).toString();
}

function windowLocationOrigin(): string {
  if (typeof window !== 'undefined') return window.location.origin;
  return 'http://localhost:3000';
}

// ---------------------------------------------------------------------------
// Tipos do control plane
// ---------------------------------------------------------------------------

export interface LoginResult {
  token: string;
  expiresAt: string;
  user: SessionUser & { active: boolean; createdAt: string };
}

export interface AdminUser {
  id: string;
  email: string;
  name: string;
  role: 'admin' | 'editor' | 'viewer';
  active: boolean;
  companyId: string;
  company?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ApiKeyInfo {
  id: string;
  app: string;
  label?: string;
  prefix: string;
  active: boolean;
  createdAt: string;
  revokedAt?: string;
}

export interface CreatedApiKey extends ApiKeyInfo {
  key: string; // única vez em claro
}

// ---------------------------------------------------------------------------
// Cliente
// ---------------------------------------------------------------------------

export const api = {
  // consulta de traces (Bearer)
  events: (q: EventQuery) => request<EventPage>('GET', '/v1/events', { params: { ...q } }),
  event: (id: string) => request<TraceEvent>('GET', `/v1/events/${encodeURIComponent(id)}`),
  sessions: (q: Record<string, string | undefined>) =>
    request<SessionPage>('GET', '/v1/sessions', { params: q }),
  session: (id: string) => request<SessionDetail>('GET', `/v1/sessions/${encodeURIComponent(id)}`),
  overview: (q: Record<string, string | undefined>) =>
    request<Overview>('GET', '/v1/stats/overview', { params: q }),
  filterOptions: () => request<FilterOptions>('GET', '/v1/meta/filters'),

  // autenticação


  // administração (role admin)
  listUsers: () => request<{ users: AdminUser[] }>('GET', '/v1/admin/users'),
  createUser: (input: {
    email: string;
    name: string;
    password: string;
    role: string;
    companyId: string;
  }) => request<AdminUser>('POST', '/v1/admin/users', { body: input }),
  updateUser: (
    id: string,
    patch: {
      role?: string;
      active?: boolean;
      name?: string;
      password?: string;
      companyId?: string;
    },
  ) => request<AdminUser>('PATCH', `/v1/admin/users/${encodeURIComponent(id)}`, { body: patch }),

  // empresas (multi-tenant leve)
  listCompanies: () => request<{ companies: Company[] }>('GET', '/v1/admin/companies'),
  createCompany: (input: { name: string; document?: string; active: boolean }) =>
    request<Company>('POST', '/v1/admin/companies', { body: input }),
  updateCompany: (id: string, patch: { name?: string; document?: string; active?: boolean }) =>
    request<Company>('PATCH', `/v1/admin/companies/${encodeURIComponent(id)}`, { body: patch }),
  deleteCompany: (id: string) =>
    request<{ deleted: boolean }>('DELETE', `/v1/admin/companies/${encodeURIComponent(id)}`),
  listKeys: () => request<{ keys: ApiKeyInfo[] }>('GET', '/v1/admin/api-keys'),
  createKey: (input: { app: string; label?: string }) =>
    request<CreatedApiKey>('POST', '/v1/admin/api-keys', { body: input }),
  revokeKey: (id: string) =>
    request<{ revoked: boolean }>('DELETE', `/v1/admin/api-keys/${encodeURIComponent(id)}`),

  // issues (erros agrupados por fingerprint)
  issues: (q: {
    from?: string;
    to?: string;
    app?: string;
    release?: string;
    onlyOpen?: string;
    assignee?: string; // 'me' filtra pelas do usuário do token
    limit?: string;
  }) => request<{ issues: Issue[] }>('GET', '/v1/issues', { params: q }),
  patchIssue: (
    fingerprint: string,
    patch: {
      status: 'open' | 'investigating' | 'resolved' | 'ignored';
      assignee?: string;
      assigneeUserId?: string;
      app?: string;
    },
  ) =>
    request<{ fingerprint: string; status: string }>(
      'PATCH',
      `/v1/issues/${encodeURIComponent(fingerprint)}`,
      { body: patch },
    ),
  listIssueComments: (fingerprint: string) =>
    request<{ comments: IssueComment[] }>(
      'GET',
      `/v1/issues/${encodeURIComponent(fingerprint)}/comments`,
    ),
  postIssueComment: (fingerprint: string, body: string) =>
    request<IssueComment>('POST', `/v1/issues/${encodeURIComponent(fingerprint)}/comments`, {
      body: { body },
    }),
  deleteIssueComment: (id: string) =>
    request<{ deleted: boolean }>('DELETE', `/v1/issues/comments/${encodeURIComponent(id)}`),

  // web vitals (LCP, CLS, INP, FID, TTFB, FCP)
  vitals: (q: { from?: string; to?: string; app?: string; vital?: string }) =>
    request<{ vitals: WebVitalStat[] }>('GET', '/v1/stats/vitals', { params: q }),

  // releases (versões do app)
  releases: (q: { from?: string; to?: string; app?: string }) =>
    request<{ releases: Release[] }>('GET', '/v1/releases', { params: q }),
  compareReleases: (q: {
    from?: string;
    to?: string;
    app?: string;
    releaseA: string;
    releaseB: string;
  }) => request<ReleaseComparison>('GET', '/v1/stats/compare', { params: q }),

  // funis de conversão
  listFunnels: (app?: string) =>
    request<{ funnels: Funnel[] }>('GET', '/v1/funnels', {
      params: app ? { app } : undefined,
    }),
  createFunnel: (input: {
    app: string;
    name: string;
    windowSeconds: number;
    steps: FunnelStep[];
  }) => request<Funnel>('POST', '/v1/funnels', { body: input }),
  updateFunnel: (
    id: string,
    input: { name: string; windowSeconds: number; steps: FunnelStep[] },
  ) => request<Funnel>('PATCH', `/v1/funnels/${encodeURIComponent(id)}`, { body: input }),
  deleteFunnel: (id: string) =>
    request<{ deleted: boolean }>('DELETE', `/v1/funnels/${encodeURIComponent(id)}`),
  funnelResults: (id: string, q: { from?: string; to?: string }) =>
    request<FunnelResult>('GET', `/v1/funnels/${encodeURIComponent(id)}/results`, { params: q }),

  // retenção D1/D7/D14/D30 por cohort
  retention: (q: { app?: string; from?: string; to?: string; cohortBy?: string }) =>
    request<RetentionResult>('GET', '/v1/stats/retention', { params: q }),

  // Detecção de anomalia (Fase 4 sprint I)
  listAnomalyRules: () =>
    request<{ rules: AnomalyRule[] }>('GET', '/v1/admin/anomaly-rules'),
  createAnomalyRule: (input: Omit<AnomalyRule, 'id' | 'createdAt' | 'updatedAt'>) =>
    request<AnomalyRule>('POST', '/v1/admin/anomaly-rules', { body: input }),
  updateAnomalyRule: (id: string, input: Omit<AnomalyRule, 'id' | 'createdAt' | 'updatedAt'>) =>
    request<AnomalyRule>('PATCH', `/v1/admin/anomaly-rules/${encodeURIComponent(id)}`, { body: input }),
  deleteAnomalyRule: (id: string) =>
    request<{ deleted: boolean }>('DELETE', `/v1/admin/anomaly-rules/${encodeURIComponent(id)}`),
  listAnomalyDetections: (q: { limit?: string; offset?: string } = {}) =>
    request<{ detections: AnomalyDetection[]; total: number }>(
      'GET',
      '/v1/admin/anomaly-detections',
      { params: q },
    ),

  // User feedback (Fase 4 sprint J)
  listFeedbacks: (q: { app?: string; status?: string; limit?: string; offset?: string } = {}) =>
    request<{ feedbacks: UserFeedback[]; total: number }>(
      'GET',
      '/v1/admin/feedbacks',
      { params: q },
    ),
  setFeedbackStatus: (id: string, status: UserFeedback['status']) =>
    request<UserFeedback>('PATCH', `/v1/admin/feedbacks/${encodeURIComponent(id)}`, {
      body: { status },
    }),
  deleteFeedback: (id: string) =>
    request<{ deleted: boolean }>('DELETE', `/v1/admin/feedbacks/${encodeURIComponent(id)}`),

  // Session snapshots (session replay MVP, Sprint H)
  snapshotMeta: (eventId: string) =>
    request<SnapshotMeta>('GET', `/v1/snapshots/event/${encodeURIComponent(eventId)}`),
  // HTML via BFF: mesma origem → cookie httpOnly vai sozinho; sem token no JS.
  snapshotHTML: (eventId: string) => {
    const url = new URL(
      `/api/snapshots/event/${encodeURIComponent(eventId)}/html`,
      windowLocationOrigin(),
    );
    return fetch(url.toString(), { credentials: 'same-origin' }).then((r) => {
      if (!r.ok) throw new ApiError(`HTTP ${r.status}`, r.status);
      return r.text();
    });
  },

  // Sampling adaptativo (Sprint G)
  listSamplingRules: () =>
    request<{ rules: SamplingRule[] }>('GET', '/v1/admin/sampling-rules'),
  createSamplingRule: (input: Omit<SamplingRule, 'id' | 'createdAt' | 'updatedAt'>) =>
    request<SamplingRule>('POST', '/v1/admin/sampling-rules', { body: input }),
  updateSamplingRule: (id: string, input: Omit<SamplingRule, 'id' | 'createdAt' | 'updatedAt'>) =>
    request<SamplingRule>('PATCH', `/v1/admin/sampling-rules/${encodeURIComponent(id)}`, { body: input }),
  deleteSamplingRule: (id: string) =>
    request<{ deleted: boolean }>('DELETE', `/v1/admin/sampling-rules/${encodeURIComponent(id)}`),

  // RBAC granular por app (Sprint E.4)
  listUserPermissions: (userId: string) =>
    request<{ permissions: UserAppPermission[] }>(
      'GET',
      `/v1/admin/users/${encodeURIComponent(userId)}/permissions`,
    ),
  grantUserPermission: (userId: string, appId: string, role: string = 'viewer') =>
    request<UserAppPermission>(
      'PUT',
      `/v1/admin/users/${encodeURIComponent(userId)}/permissions/${encodeURIComponent(appId)}`,
      { body: { role } },
    ),
  revokeUserPermission: (userId: string, appId: string) =>
    request<{ revoked: boolean }>(
      'DELETE',
      `/v1/admin/users/${encodeURIComponent(userId)}/permissions/${encodeURIComponent(appId)}`,
    ),

  // LGPD/GDPR — portabilidade e direito ao esquecimento
  // gdprExportUrl retorna a URL pra fazer download (usa GET com Bearer que
  // o browser não consegue anexar — o handler que consome faz o fetch e cria blob).
  gdprExport: (userId: string) =>
    request<{ userId: string; eventCount: number; exportedAt: string; events: unknown[] }>(
      'GET',
      `/v1/admin/gdpr/user/${encodeURIComponent(userId)}/export`,
    ),
  gdprForget: (userId: string) =>
    request<{ userId: string; status: string; note: string }>(
      'DELETE',
      `/v1/admin/gdpr/user/${encodeURIComponent(userId)}`,
    ),

  // audit log (Fase 3 — governance)
  auditLog: (q: {
    actor?: string;
    action?: string;
    resourceType?: string;
    from?: string;
    to?: string;
    limit?: string;
    offset?: string;
  }) => request<{ entries: AuditEntry[]; total: number }>('GET', '/v1/admin/audit-log', { params: q }),

  // saved views (presets de filtro por tela)
  listSavedViews: (viewType?: string) =>
    request<{ views: SavedView[] }>('GET', '/v1/saved-views', {
      params: viewType ? { viewType } : undefined,
    }),
  createSavedView: (input: {
    name: string;
    viewType: string;
    filters: Record<string, unknown>;
    isShared: boolean;
  }) => request<SavedView>('POST', '/v1/saved-views', { body: input }),
  updateSavedView: (
    id: string,
    input: { name: string; filters: Record<string, unknown>; isShared: boolean; viewType?: string },
  ) => request<SavedView>('PATCH', `/v1/saved-views/${encodeURIComponent(id)}`, { body: input }),
  deleteSavedView: (id: string) =>
    request<{ deleted: boolean }>('DELETE', `/v1/saved-views/${encodeURIComponent(id)}`),

  // source maps (admin) + resolução de stack (viewer+)
  listSourceMaps: (q: { app?: string; release?: string }) =>
    request<{ sourceMaps: SourceMap[] }>('GET', '/v1/admin/source-maps', { params: q }),
  uploadSourceMap: (input: { app: string; release: string; filename: string; content: string }) =>
    request<SourceMap>('POST', '/v1/admin/source-maps', { body: input }),
  deleteSourceMap: (id: string) =>
    request<{ deleted: boolean }>('DELETE', `/v1/admin/source-maps/${encodeURIComponent(id)}`),
  resolvedStack: (eventId: string) =>
    request<{ frames: StackFrame[]; raw: string; release?: string }>(
      'GET',
      `/v1/events/${encodeURIComponent(eventId)}/resolved-stack`,
    ),

  // alertas (admin)
  listAlerts: () => request<{ alerts: AlertRule[] }>('GET', '/v1/admin/alerts'),
  createAlert: (input: Omit<AlertRule, 'id' | 'createdAt' | 'updatedAt'>) =>
    request<AlertRule>('POST', '/v1/admin/alerts', { body: input }),
  deleteAlert: (id: string) =>
    request<{ deleted: boolean }>('DELETE', `/v1/admin/alerts/${encodeURIComponent(id)}`),

  // apps emissores (CRUD + geração de chave no cadastro)
  listApps: () => request<{ apps: App[] }>('GET', '/v1/admin/apps'),
  createApp: (input: {
    name: string;
    technology?: string;
    company?: string;
    companyId?: string;
    responsible?: string;
  }) => request<CreatedApp>('POST', '/v1/admin/apps', { body: input }),
  updateApp: (
    id: string,
    patch: {
      name?: string;
      technology?: string;
      company?: string;
      companyId?: string;
      responsible?: string;
    },
  ) => request<App>('PATCH', `/v1/admin/apps/${encodeURIComponent(id)}`, { body: patch }),
  deleteApp: (id: string) =>
    request<{ deleted: boolean }>('DELETE', `/v1/admin/apps/${encodeURIComponent(id)}`),
};
