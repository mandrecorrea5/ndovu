'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useMemo, useState } from 'react';
import { JsonView } from '@/components/JsonView';
import { EmptyState, ErrorState, LoadingState, StatTile } from '@/components/ui';
import { api } from '@/lib/api';
import { fmtDateTime } from '@/lib/time';
import { buildWaterfall, type WaterfallRow } from '@/lib/waterfall';

/**
 * Waterfall do trace: o clique e o fetch do browser (SDK) e, por baixo, os
 * spans do backend recebidos por OTLP — a mesma árvore, ligada pelo
 * traceparent que o SDK propaga.
 */
export default function TracePage() {
  const { traceId: raw } = useParams<{ traceId: string }>();
  const traceId = decodeURIComponent(raw);
  const [selected, setSelected] = useState<string | null>(null);

  const { data, isLoading, error } = useQuery({
    queryKey: ['trace', traceId],
    queryFn: () => api.trace(traceId),
  });
  const wf = useMemo(() => (data ? buildWaterfall(data) : null), [data]);
  const sessionId = data?.events.find((e) => e.sessionId)?.sessionId;
  const total = wf ? Math.max(wf.end - wf.start, 1) : 1;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Link href="/traces" className="text-sm text-muted hover:text-ink">
          ← explorador
        </Link>
        <h1 className="mono text-lg font-semibold">{traceId}</h1>
        {sessionId ? (
          <Link
            href={`/sessions/${encodeURIComponent(sessionId)}`}
            className="ml-auto text-sm text-accent hover:underline"
          >
            ver sessão →
          </Link>
        ) : null}
      </div>

      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}

      {wf && wf.rows.length === 0 ? (
        <EmptyState message="Nada neste trace que você possa ver. Ele pode ter expirado, ter sido descartado pelo sampling ou pertencer a um app fora do seu acesso." />
      ) : null}

      {wf && wf.rows.length > 0 ? (
        <>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
            <StatTile label="Início" value={fmtDateTime(new Date(wf.start).toISOString())} />
            <StatTile label="Duração" value={fmtMs(wf.end - wf.start)} />
            <StatTile label="Eventos do SDK" value={data!.events.length} />
            <StatTile label="Spans do backend" value={data!.spans.length} />
            <StatTile label="Com erro" value={wf.errors} tone={wf.errors > 0 ? 'danger' : undefined} />
          </div>

          <p className="text-xs text-ink-2">
            Serviços: <span className="mono">{wf.services.join(' · ')}</span>
          </p>

          <div className="card overflow-x-auto">
            <div className="min-w-[720px]">
              <div className="flex border-b border-hairline px-3 py-1.5 text-xs text-muted">
                <span className="w-[42%] shrink-0">Operação</span>
                <span className="relative flex-1">
                  <span>0</span>
                  <span className="absolute left-1/2 -translate-x-1/2">{fmtMs(total / 2)}</span>
                  <span className="absolute right-0">{fmtMs(total)}</span>
                </span>
              </div>
              <ol>
                {wf.rows.map((row) => (
                  <WaterfallLine
                    key={row.key}
                    row={row}
                    offset={(row.start - wf.start) / total}
                    width={row.durationMs / total}
                    open={selected === row.key}
                    onToggle={() => setSelected((cur) => (cur === row.key ? null : row.key))}
                  />
                ))}
              </ol>
            </div>
          </div>

          {data!.spans.length === 0 ? (
            <p className="text-xs text-muted">
              Sem spans do backend neste trace. Para vê-los aqui, exporte OpenTelemetry do servidor para o
              receptor OTLP do Ndovu (app marcado como “aceita OTLP”).
            </p>
          ) : null}
        </>
      ) : null}
    </div>
  );
}

function WaterfallLine({
  row,
  offset,
  width,
  open,
  onToggle,
}: {
  row: WaterfallRow;
  offset: number;
  width: number;
  open: boolean;
  onToggle: () => void;
}) {
  const bar = row.error ? 'bg-critical' : row.source === 'otlp' ? 'bg-accent' : 'bg-ink-2';
  return (
    <li className="border-b border-hairline last:border-b-0">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="flex w-full items-center px-3 py-1.5 text-left text-sm hover:bg-plane"
      >
        <span className="flex w-[42%] shrink-0 items-center gap-2 overflow-hidden pr-3" style={{ paddingLeft: row.depth * 14 }}>
          <span
            className={`shrink-0 rounded px-1 text-[10px] font-medium uppercase ${
              row.source === 'otlp' ? 'bg-accent/10 text-accent' : 'bg-plane text-ink-2'
            }`}
            title={row.source === 'otlp' ? 'span OTLP do backend' : 'evento do SDK'}
          >
            {row.source === 'otlp' ? row.kind : 'sdk'}
          </span>
          {row.error ? (
            <span aria-label="erro" className="shrink-0 text-critical">
              ✕
            </span>
          ) : null}
          <span className="truncate" title={row.name}>
            {row.name}
          </span>
          <span className="mono ml-auto shrink-0 text-xs text-muted">{row.service}</span>
        </span>
        <span className="relative h-5 flex-1">
          <span
            className={`absolute top-1 h-3 rounded-sm ${bar}`}
            style={{ left: `${offset * 100}%`, width: `max(${width * 100}%, 2px)` }}
          />
          <span
            className="tabular absolute top-0 whitespace-nowrap text-xs text-ink-2"
            style={offset + width > 0.8 ? { right: `${(1 - offset) * 100 + 0.5}%` } : { left: `calc(${(offset + width) * 100}% + 6px)` }}
          >
            {fmtMs(row.durationMs)}
          </span>
        </span>
      </button>
      {open ? <RowDetail row={row} /> : null}
    </li>
  );
}

function RowDetail({ row }: { row: WaterfallRow }) {
  if (row.span) {
    const s = row.span;
    return (
      <div className="border-t border-hairline bg-plane/50 px-4 py-2 text-xs text-ink-2">
        <div className="grid gap-x-8 gap-y-1 sm:grid-cols-2">
          <Field label="spanId" value={s.spanId} mono />
          <Field label="pai" value={s.parentSpanId || '—'} mono />
          <Field label="serviço" value={s.serviceName} />
          <Field label="app" value={s.app} />
          <Field label="status" value={s.statusMessage ? `${s.statusCode} — ${s.statusMessage}` : s.statusCode} />
          <Field label="instrumentação" value={s.scopeName || '—'} />
        </div>
        <div className="grid gap-x-6 lg:grid-cols-2">
          <JsonView title="Atributos" data={nonEmpty(s.attributes)} />
          <JsonView title="Resource" data={nonEmpty(s.resourceAttributes)} />
          <JsonView title="Eventos" data={s.events?.length ? s.events : undefined} />
          <JsonView title="Links" data={s.links?.length ? s.links : undefined} />
        </div>
      </div>
    );
  }
  const e = row.event!;
  return (
    <div className="border-t border-hairline bg-plane/50 px-4 py-2 text-xs text-ink-2">
      <div className="grid gap-x-8 gap-y-1 sm:grid-cols-2">
        <Field label="eventId" value={e.eventId} mono />
        <Field label="spanId" value={e.trace?.spanId || '—'} mono />
        <Field label="tela" value={e.screen || '—'} />
        <Field label="usuário" value={e.userId || 'anônimo'} />
        {e.error?.message ? <Field label="erro" value={`${e.error.code ?? ''} ${e.error.message}`} /> : null}
      </div>
      <div className="grid gap-x-6 lg:grid-cols-2">
        <JsonView title="Request body" data={e.http?.requestBody} />
        <JsonView title="Response body" data={e.http?.responseBody} />
        <JsonView title="Error body" data={e.error?.body} />
        <JsonView title="Metadata" data={e.metadata} />
      </div>
    </div>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <p className={`truncate ${mono ? 'mono' : ''}`}>
      <span className="text-muted">{label}: </span>
      {value}
    </p>
  );
}

function nonEmpty(m?: Record<string, string>) {
  return m && Object.keys(m).length > 0 ? m : undefined;
}

/** ms com precisão útil para spans curtos (SQL de 0,4 ms não vira "0 ms"). */
function fmtMs(ms: number): string {
  if (ms < 1) return `${ms.toFixed(2)} ms`;
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}
