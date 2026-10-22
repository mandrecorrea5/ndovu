import type { Span, TraceEvent, TraceView } from './types';

/**
 * Uma linha do waterfall: um evento do SDK ou um span OTLP, já com início e
 * duração em ms desde a época e a profundidade na árvore.
 */
export interface WaterfallRow {
  key: string;
  /** span_id do lado que gerou a linha (evento sem trace usa o eventId). */
  id: string;
  parentId?: string;
  depth: number;
  name: string;
  /** Serviço OTel, ou o app do SDK. */
  service: string;
  source: 'sdk' | 'otlp';
  kind: string;
  start: number;
  durationMs: number;
  error: boolean;
  event?: TraceEvent;
  span?: Span;
}

export interface Waterfall {
  rows: WaterfallRow[];
  start: number;
  end: number;
  services: string[];
  errors: number;
}

function fromEvent(e: TraceEvent, i: number): Omit<WaterfallRow, 'depth'> {
  return {
    key: `e:${e.eventId}:${i}`,
    id: e.trace?.spanId || e.eventId,
    parentId: e.trace?.parentSpanId || undefined,
    name: e.http?.method && e.http.url ? `${e.http.method} ${e.http.url}` : e.name,
    service: e.app,
    source: 'sdk',
    kind: e.type,
    start: Date.parse(e.timestamp),
    durationMs: e.durationMs ?? 0,
    error: Boolean(e.error) || (e.http?.statusCode ?? 0) >= 500,
    event: e,
  };
}

function fromSpan(s: Span, i: number): Omit<WaterfallRow, 'depth'> {
  return {
    key: `s:${s.spanId}:${i}`,
    id: s.spanId,
    parentId: s.parentSpanId || undefined,
    name: s.name,
    service: s.serviceName,
    source: 'otlp',
    kind: s.kind,
    start: Date.parse(s.startTime),
    durationMs: s.durationNs / 1e6,
    error: s.statusCode === 'error' || (s.events ?? []).some((ev) => ev.name === 'exception'),
    span: s,
  };
}

/**
 * Monta a árvore pelo parent_span_id dos dois lados. O fetch do browser é
 * pai do span SERVER do backend porque o SDK propaga o traceparent; linhas
 * cujo pai não está no trace (outro app fora do scope, span descartado pelo
 * sampling) sobem para a raiz em vez de sumir. Irmãos ficam em ordem de início.
 */
export function buildWaterfall(view: TraceView): Waterfall {
  const flat = [...view.events.map(fromEvent), ...view.spans.map(fromSpan)].filter((r) =>
    Number.isFinite(r.start),
  );

  const byId = new Map<string, Omit<WaterfallRow, 'depth'>>();
  for (const r of flat) if (!byId.has(r.id)) byId.set(r.id, r);

  const children = new Map<string, Omit<WaterfallRow, 'depth'>[]>();
  const roots: Omit<WaterfallRow, 'depth'>[] = [];
  for (const r of flat) {
    // Pai = a si mesmo (id repetido) seria ciclo: trata como raiz.
    const parent = r.parentId && r.parentId !== r.id ? byId.get(r.parentId) : undefined;
    if (parent && parent !== r) {
      const list = children.get(parent.key) ?? [];
      list.push(r);
      children.set(parent.key, list);
    } else {
      roots.push(r);
    }
  }

  const byStart = (a: { start: number }, b: { start: number }) => a.start - b.start;
  const rows: WaterfallRow[] = [];
  const seen = new Set<string>();
  const walk = (r: Omit<WaterfallRow, 'depth'>, depth: number) => {
    if (seen.has(r.key)) return; // defesa contra ciclo em dado malformado
    seen.add(r.key);
    rows.push({ ...r, depth });
    for (const c of (children.get(r.key) ?? []).sort(byStart)) walk(c, depth + 1);
  };
  for (const r of roots.sort(byStart)) walk(r, 0);
  // Ciclo puro (a→b→a) não tem raiz: entra solto no fim, sem perder linha.
  for (const r of flat) if (!seen.has(r.key)) walk(r, 0);

  const start = rows.length ? Math.min(...rows.map((r) => r.start)) : 0;
  const end = rows.length ? Math.max(...rows.map((r) => r.start + r.durationMs)) : 0;
  return {
    rows,
    start,
    end,
    services: [...new Set(rows.map((r) => r.service))],
    errors: rows.filter((r) => r.error).length,
  };
}
