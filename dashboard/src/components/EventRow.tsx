'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useState } from 'react';
import { api } from '@/lib/api';
import type { TraceEvent } from '@/lib/types';
import { fmtDateTime, fmtDuration } from '@/lib/time';
import { JsonView } from './JsonView';
import { SnapshotViewer } from './SnapshotViewer';
import { ErrorPill, StatusBadge, TypeBadge } from './ui';

/**
 * Linha expandível da tabela de eventos: o clique abre os detalhes completos
 * (payloads de request/response/erro, metadata, ids) sem sair da lista.
 */
export function EventRow({ event, showSessionLink = true }: { event: TraceEvent; showSessionLink?: boolean }) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <tr
        className="cursor-pointer border-t border-hairline hover:bg-plane"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
          {fmtDateTime(event.timestamp)}
        </td>
        <td className="px-3 py-2">
          <TypeBadge type={event.type} />
        </td>
        <td className="px-3 py-2 text-sm font-medium">
          {event.name}
          {event.error ? <ErrorPill code={event.error.code ?? 'erro'} /> : null}
        </td>
        <td className="mono max-w-56 truncate px-3 py-2 text-xs text-ink-2">
          {event.http?.url ?? event.screen ?? '—'}
        </td>
        <td className="px-3 py-2">
          <StatusBadge status={event.http?.statusCode} />
        </td>
        <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
          {fmtDuration(event.durationMs)}
        </td>
        <td className="mono max-w-32 truncate px-3 py-2 text-xs text-ink-2">
          {event.userId ?? 'anônimo'}
        </td>
        <td className="px-3 py-2 text-xs text-muted">{open ? '▾' : '▸'}</td>
      </tr>
      {open ? (
        <tr className="border-t border-hairline bg-plane/60">
          <td colSpan={8} className="px-4 py-3">
            <div className="grid gap-x-8 gap-y-1 text-xs text-ink-2 sm:grid-cols-2 lg:grid-cols-4">
              <p>
                <span className="text-muted">app: </span>
                {event.app}
              </p>
              <p className="mono truncate">
                <span className="text-muted">eventId: </span>
                {event.eventId}
              </p>
              <p className="mono truncate">
                <span className="text-muted">sessão: </span>
                {showSessionLink ? (
                  <Link
                    href={`/sessions/${encodeURIComponent(event.sessionId)}`}
                    className="text-accent hover:underline"
                    onClick={(e) => e.stopPropagation()}
                  >
                    {event.sessionId}
                  </Link>
                ) : (
                  event.sessionId
                )}
              </p>
              <p>
                <span className="text-muted">tela: </span>
                {event.screen ?? '—'}
              </p>
              {event.http?.method ? (
                <p className="mono">
                  <span className="text-muted">req: </span>
                  {event.http.method} {event.http.url}
                </p>
              ) : null}
              {event.feature ? (
                <p>
                  <span className="text-muted">funcionalidade: </span>
                  {event.feature}
                </p>
              ) : null}
              {event.error?.message ? (
                <p className="text-critical sm:col-span-2">
                  <span className="text-muted">erro: </span>
                  {event.error.message}
                </p>
              ) : null}
            </div>
            {event.type === 'error' ? (
              <>
                <ResolvedStack event={event} />
                <SnapshotViewer eventId={event.eventId} />
              </>
            ) : null}
            <div className="grid gap-x-6 lg:grid-cols-2">
              <JsonView title="Request body" data={event.http?.requestBody} />
              <JsonView title="Response body" data={event.http?.responseBody} />
              <JsonView title="Error body" data={event.error?.body} />
              <JsonView title="Metadata" data={event.metadata} />
            </div>
          </td>
        </tr>
      ) : null}
    </>
  );
}

/**
 * ResolvedStack só é montado quando a linha do erro é expandida — evita
 * chamadas HTTP em todos os eventos da lista.
 */
function ResolvedStack({ event }: { event: TraceEvent }) {
  const { data, isLoading } = useQuery({
    queryKey: ['resolved-stack', event.eventId],
    queryFn: () => api.resolvedStack(event.eventId),
    staleTime: 5 * 60_000,
  });

  if (isLoading) {
    return (
      <p className="mt-2 text-xs text-muted">Resolvendo stack com os source maps…</p>
    );
  }
  if (!data || data.frames.length === 0) return null;

  const anyResolved = data.frames.some((f) => f.original);

  return (
    <div className="mt-3 overflow-x-auto rounded-md border border-hairline bg-surface">
      <header className="flex items-center justify-between border-b border-hairline px-3 py-1.5">
        <p className="text-xs font-medium text-ink-2">
          Stack {anyResolved ? 'resolvido via source map' : '(sem source map — cru)'}
          {data.release ? (
            <span className="mono ml-2 text-accent">release {data.release}</span>
          ) : null}
        </p>
        {!anyResolved ? (
          <Link
            href="/admin/source-maps"
            className="text-xs text-accent hover:underline"
            onClick={(e) => e.stopPropagation()}
          >
            enviar source map
          </Link>
        ) : null}
      </header>
      <ol className="divide-y divide-hairline">
        {data.frames.map((frame, i) => (
          <li key={i} className="mono px-3 py-1.5 text-xs">
            {frame.function ? (
              <span className="text-ink">{frame.function}</span>
            ) : (
              <span className="text-muted">(anônimo)</span>
            )}
            <span className="ml-2 text-muted">
              {frame.original && frame.source ? (
                <span className="text-good">{frame.source}</span>
              ) : (
                <span>
                  {frame.file}:{frame.line}:{frame.column}
                </span>
              )}
            </span>
          </li>
        ))}
      </ol>
    </div>
  );
}

export function EventsTableHead() {
  return (
    <thead>
      <tr className="text-left text-xs uppercase tracking-wide text-muted">
        <th className="px-3 py-2 font-medium">Quando</th>
        <th className="px-3 py-2 font-medium">Tipo</th>
        <th className="px-3 py-2 font-medium">Evento</th>
        <th className="px-3 py-2 font-medium">Rota / tela</th>
        <th className="px-3 py-2 font-medium">Status</th>
        <th className="px-3 py-2 font-medium">Duração</th>
        <th className="px-3 py-2 font-medium">Usuário</th>
        <th className="px-3 py-2" aria-label="expandir" />
      </tr>
    </thead>
  );
}
