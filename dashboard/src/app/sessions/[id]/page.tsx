'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useState } from 'react';
import { JsonView } from '@/components/JsonView';
import { ErrorPill, ErrorState, LoadingState, StatTile, StatusBadge, TYPE_META } from '@/components/ui';
import { api } from '@/lib/api';
import { fmtDateTime, fmtDuration, fmtTime } from '@/lib/time';
import type { TraceEvent } from '@/lib/types';

/**
 * O rastro da sessão: timeline cronológica de tudo que o usuário fez,
 * com payloads expandíveis. É a tela de troubleshooting.
 */
export default function SessionDetailPage() {
  const { id } = useParams<{ id: string }>();
  const sessionId = decodeURIComponent(id);

  const { data, isLoading, error } = useQuery({
    queryKey: ['session', sessionId],
    queryFn: () => api.session(sessionId),
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Link href="/sessions" className="text-sm text-muted hover:text-ink">
          ← sessões
        </Link>
        <h1 className="mono text-lg font-semibold">{sessionId}</h1>
      </div>

      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}

      {data ? (
        <>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <StatTile label="Usuário" value={data.session.userId ?? 'anônimo'} />
            <StatTile label="App" value={data.session.app} />
            <StatTile label="Início" value={fmtDateTime(data.session.startedAt)} />
            <StatTile label="Fim" value={fmtDateTime(data.session.lastEventAt)} />
            <StatTile
              label="Duração"
              value={fmtDuration(
                new Date(data.session.lastEventAt).getTime() -
                  new Date(data.session.startedAt).getTime(),
              )}
            />
            <StatTile label="Eventos" value={data.session.eventCount} />
            <StatTile
              label="Erros"
              value={data.session.errorCount}
              tone={data.session.errorCount > 0 ? 'danger' : undefined}
            />
          </div>

          {data.session.userAgent ? (
            <p className="mono truncate text-xs text-muted" title={data.session.userAgent}>
              {data.session.userAgent}
            </p>
          ) : null}

          {data.timeline.length === 0 ? (
            <div className="card p-4 text-sm text-ink-2">
              <p className="font-medium text-ink">Sessão sem eventos capturados.</p>
              <p className="mt-1 text-xs">
                Nenhum trace do SDK chegou para esta sessão. Pode ter sido criada apenas
                via widget de feedback ou snapshot, ou os eventos podem ter sido descartados
                pelo sampling adaptativo. O feedback/snapshot associado continua acessível
                nos respectivos menus.
              </p>
            </div>
          ) : (
            <ol className="relative ml-3 space-y-0">
              {data.timeline.map((event, i) => (
                <TimelineItem
                  key={event.eventId}
                  event={event}
                  isLast={i === data.timeline.length - 1}
                />
              ))}
            </ol>
          )}
        </>
      ) : null}
    </div>
  );
}

function TimelineItem({ event, isLast }: { event: TraceEvent; isLast: boolean }) {
  const [open, setOpen] = useState(false);
  const meta = TYPE_META[event.type] ?? { label: event.type, icon: '·' };
  const failed = Boolean(event.error);

  return (
    <li className="relative pb-1 pl-8">
      {/* trilho vertical */}
      {!isLast ? (
        <span
          aria-hidden
          className="absolute left-[11px] top-7 h-full w-px bg-grid"
        />
      ) : null}
      {/* nó */}
      <span
        aria-hidden
        className={`absolute left-0 top-1.5 flex h-6 w-6 items-center justify-center rounded-full border text-xs ${
          failed
            ? 'border-critical bg-critical/10 text-critical'
            : 'border-hairline bg-surface text-ink-2'
        }`}
      >
        {failed ? '✕' : meta.icon}
      </span>

      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="card mb-2 w-full px-4 py-2.5 text-left transition-colors hover:bg-plane"
      >
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span className="tabular text-xs text-muted">{fmtTime(event.timestamp)}</span>
          <span className="text-xs text-muted">{meta.label}</span>
          <span className="text-sm font-medium">{event.name}</span>
          {event.error ? <ErrorPill code={event.error.code ?? 'erro'} /> : null}
          <span className="ml-auto flex items-center gap-3">
            {event.http?.statusCode != null ? <StatusBadge status={event.http.statusCode} /> : null}
            {event.durationMs != null ? (
              <span className="tabular text-xs text-ink-2">{fmtDuration(event.durationMs)}</span>
            ) : null}
            <span className="text-xs text-muted">{open ? '▾' : '▸'}</span>
          </span>
        </div>
        <p className="mono mt-0.5 truncate text-xs text-ink-2">
          {event.http?.method ? `${event.http.method} ${event.http.url}` : event.screen ?? ''}
          {event.error?.message ? (
            <span className="text-critical"> — {event.error.message}</span>
          ) : null}
        </p>

        {open ? (
          <div className="mt-2 border-t border-hairline pt-2" onClick={(e) => e.stopPropagation()}>
            <div className="grid gap-x-8 gap-y-1 text-xs text-ink-2 sm:grid-cols-2">
              <p className="mono truncate">
                <span className="text-muted">eventId: </span>
                {event.eventId}
              </p>
              <p>
                <span className="text-muted">tela: </span>
                {event.screen ?? '—'}
              </p>
              {event.feature ? (
                <p>
                  <span className="text-muted">funcionalidade: </span>
                  {event.feature}
                </p>
              ) : null}
              {event.receivedAt ? (
                <p>
                  <span className="text-muted">recebido em: </span>
                  {fmtDateTime(event.receivedAt)}
                </p>
              ) : null}
            </div>
            <div className="grid gap-x-6 lg:grid-cols-2">
              <JsonView title="Request body" data={event.http?.requestBody} />
              <JsonView title="Response body" data={event.http?.responseBody} />
              <JsonView title="Error body" data={event.error?.body} />
              <JsonView title="Metadata" data={event.metadata} />
            </div>
          </div>
        ) : null}
      </button>
    </li>
  );
}
