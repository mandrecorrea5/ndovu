/** Tipos espelhando o contrato v1 da Ndovu API (docs/CONTRACT.md). */

export type EventType = 'page_view' | 'action' | 'http_request' | 'error' | 'custom';

export interface HTTPInfo {
  method?: string;
  url?: string;
  statusCode?: number;
  requestBody?: unknown;
  responseBody?: unknown;
}

export interface ErrorInfo {
  code?: string;
  message?: string;
  body?: unknown;
}

export interface TraceEvent {
  eventId: string;
  sessionId: string;
  userId?: string;
  app: string;
  type: EventType;
  name: string;
  feature?: string;
  screen?: string;
  http?: HTTPInfo;
  error?: ErrorInfo;
  durationMs?: number;
  metadata?: Record<string, unknown>;
  timestamp: string;
  receivedAt?: string;
}

export interface Session {
  sessionId: string;
  userId?: string;
  app: string;
  userAgent?: string;
  attributes?: Record<string, unknown>;
  startedAt: string;
  lastEventAt: string;
  eventCount: number;
  errorCount: number;
}

export interface EventPage {
  events: TraceEvent[];
  nextCursor?: string;
}

export interface SessionPage {
  sessions: Session[];
  total: number;
}

export interface SessionDetail {
  session: Session;
  timeline: TraceEvent[];
}

export interface TimeBucket {
  bucket: string;
  total: number;
  errors: number;
}

export interface RouteStat {
  url: string;
  count: number;
  errors: number;
  avgMs: number;
  p95Ms: number;
}

export interface ErrorStat {
  code: string;
  message: string;
  count: number;
}

export interface Overview {
  totalEvents: number;
  totalSessions: number;
  totalUsers: number;
  totalErrors: number;
  errorRate: number;
  avgDurationMs: number;
  series: TimeBucket[];
  topRoutes: RouteStat[];
  topErrors: ErrorStat[];
}

export interface FilterOptions {
  apps: string[];
  types: string[];
  features: string[];
  names: string[];
}

/** Release: uma versão do app com contadores agregados na janela. */
export interface Release {
  release: string;
  app: string;
  firstSeen: string;
  lastSeen: string;
  events: number;
  errors: number;
  sessions: number;
  users: number;
  errorRate: number;
  avgDurationMs: number;
}

/** Comparação entre duas releases (delta + issues novas em B). */
export interface ReleaseComparison {
  releaseA: Release;
  releaseB: Release;
  deltaErrorRate: number;
  deltaAvgMs: number;
  newIssues: Issue[];
}

/** Passo de um funil: matcher AND sobre trace_events. type é obrigatório. */
export interface FunnelStepMatch {
  type: 'page_view' | 'action' | 'http_request' | 'error' | 'custom';
  name?: string;
  screen?: string;
  httpUrl?: string;
  feature?: string;
}

export interface FunnelStep {
  name: string;
  match: FunnelStepMatch;
}

/** Definição persistida de funil (Postgres). */
export interface Funnel {
  id: string;
  app: string;
  name: string;
  windowSeconds: number;
  steps: FunnelStep[]; // vem como JSON string do backend; parse na página
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
}

export interface FunnelStepResult {
  name: string;
  sessions: number;
  overallRate: number;
  stepConversion: number;
  dropoffFromPrev: number;
}

export interface FunnelResult {
  totalSessions: number;
  steps: FunnelStepResult[];
}

/** Matriz cohort × Dn para retenção. */
export interface RetentionCohort {
  cohort: string;
  newUsers: number;
  retained: Record<string, number>;
  retainedPct: Record<string, number>;
}

export interface RetentionResult {
  cohortBy: string;
  offsetsDays: number[];
  cohorts: RetentionCohort[];
}

/** Regra de detecção de anomalia (Fase 4 sprint I). */
export interface AnomalyRule {
  id: string;
  name: string;
  app: string;
  metric: 'error_count' | 'event_count' | 'error_rate';
  windowMinutes: number;
  baselineWeeks: number;
  sensitivity: number;
  direction: 'above' | 'below' | 'both';
  silenceSeconds: number;
  channel: 'slack' | 'webhook';
  targetUrl: string;
  active: boolean;
  createdAt: string;
  updatedAt: string;
}

/** Registro de um disparo de anomalia. */
export interface AnomalyDetection {
  id: string;
  ruleId: string;
  ruleName?: string;
  detectedAt: string;
  currentValue: number;
  baselineAvg: number;
  baselineStddev: number;
  zScore: number;
  direction: 'above' | 'below';
  notifyOk: boolean;
  notifyDetail?: string;
}

/** Metadata de um session snapshot (session replay MVP, Sprint H). */
export interface SnapshotMeta {
  id: string;
  eventId: string;
  sessionId: string;
  app: string;
  objectKey: string;
  sizeBytes: number;
  viewportW?: number;
  viewportH?: number;
  url?: string;
  takenAt: string;
  receivedAt: string;
}

/** Regra de sampling adaptativo (Sprint G). */
export interface SamplingRule {
  id: string;
  app: string;
  eventType: string;
  sampleRate: number;
  keepErrors: boolean;
  active: boolean;
  note?: string;
  createdAt: string;
  updatedAt: string;
}

/** Permissão explícita de user → app (RBAC granular, Sprint E.4). */
export interface UserAppPermission {
  userId: string;
  appId: string;
  appName?: string;
  role: string; // "viewer" | "editor"
  grantedAt: string;
}

/** Entrada de audit log (imutável, insert-only). */
export interface AuditEntry {
  id: string;
  actorUserId?: string;
  actorEmail: string;
  action: string;
  resourceType?: string;
  resourceId?: string;
  details?: Record<string, unknown>;
  ip?: string;
  userAgent?: string;
  createdAt: string;
}

/** Saved view: preset de filtros nomeado que o usuário salva por tela. */
export interface SavedView {
  id: string;
  ownerUserId: string;
  ownerName?: string;
  viewType: string; // 'traces' | 'issues' | 'sessions' — string livre
  name: string;
  filters: Record<string, unknown>;
  isShared: boolean;
  createdAt: string;
  updatedAt: string;
}

/** Source map armazenado (metadata; content não vem no JSON). */
export interface SourceMap {
  id: string;
  app: string;
  release: string;
  filename: string;
  sizeBytes: number;
  uploadedBy?: string;
  uploadedAt: string;
}

/** Um frame do stack trace já resolvido via source map. */
export interface StackFrame {
  file: string;
  line: number;
  column: number;
  function?: string;
  source?: string; // caminho original quando o map resolveu
  original: boolean;
}

/** Empresa cliente da ferramenta (multi-tenant leve). */
export interface Company {
  id: string;
  name: string;
  document?: string;
  active: boolean;
  createdAt: string;
  updatedAt: string;
}

/** App emissor (frontend) cadastrado no control plane. */
export interface App {
  id: string;
  name: string;
  technology?: string;
  companyId?: string;
  company?: string;
  responsible?: string;
  createdAt: string;
  updatedAt: string;
}

/** Resultado do cadastro de um app: o app + a chave gerada (única exibição). */
export interface CreatedApp extends App {
  key: string;
}

/** Issue: erro agrupado por fingerprint (LibreOffice → Sentry-like). */
export interface Issue {
  fingerprint: string;
  app: string;
  type: string;
  code: string;
  message: string;
  name: string;
  sampleUrl?: string;
  count: number;
  affectedUsers: number;
  firstSeen: string;
  lastSeen: string;
  status?: 'open' | 'investigating' | 'resolved' | 'ignored';
  assignee?: string;
  assigneeUserId?: string;
  assigneeName?: string;
  assigneeEmail?: string;
  impactScore: number;
}

/** Comentário em uma issue. */
export interface IssueComment {
  id: string;
  fingerprint: string;
  authorId?: string;
  authorName?: string;
  authorEmail?: string;
  body: string;
  createdAt: string;
}

/** Métrica Web Vital agregada por rota. */
export interface WebVitalStat {
  vital: string;
  screen: string;
  count: number;
  p75: number;
  p95: number;
  good: number;
  poor: number;
}

/** Regra de alerta configurável (admin). */
export interface AlertRule {
  id: string;
  name: string;
  app: string;
  errorCode: string;
  threshold: number;
  windowSeconds: number;
  channel: 'slack' | 'webhook';
  targetUrl: string;
  silenceSeconds: number;
  active: boolean;
  createdAt: string;
  updatedAt: string;
}

/** Feedback enviado pelo usuário final via widget (Sprint J). */
export interface UserFeedback {
  id: string;
  app: string;
  sessionId: string;
  eventId?: string;
  userId?: string;
  email?: string;
  type: 'bug' | 'suggestion' | 'praise' | 'other';
  message: string;
  url?: string;
  viewportW?: number;
  viewportH?: number;
  status: 'new' | 'triaging' | 'resolved' | 'dismissed';
  createdAt: string;
  resolvedAt?: string;
}

/** Filtro do explorador — vive na querystring para telas compartilháveis. */
export interface EventQuery {
  from?: string;
  to?: string;
  app?: string;
  userId?: string;
  sessionId?: string;
  type?: string;
  feature?: string;
  name?: string;
  route?: string;
  statusMin?: string;
  statusMax?: string;
  onlyErrors?: string;
  search?: string;
  limit?: string;
  cursor?: string;
}
