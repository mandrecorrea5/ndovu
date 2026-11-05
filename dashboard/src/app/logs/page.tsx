'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useState } from 'react';
import { EmptyState, ErrorState, LoadingState } from '@/components/ui';
import { api } from '@/lib/api';
import { fmtDateTime } from '@/lib/time';

function toLocalInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function iso(value: string): string {
  return value ? new Date(value).toISOString() : '';
}

const SEVERITIES = ['', 'trace', 'debug', 'info', 'warn', 'error', 'fatal'];

export default function LogsPage() {
  const [from, setFrom] = useState(() => toLocalInput(new Date(Date.now() - 60 * 60 * 1000)));
  const [to, setTo] = useState(() => toLocalInput(new Date()));
  const [app, setApp] = useState('');
  const [service, setService] = useState('');
  const [severity, setSeverity] = useState('');
  const [search, setSearch] = useState('');

  const { data: options } = useQuery({ queryKey: ['filters'], queryFn: api.filterOptions });
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['logs', from, to, app, service, severity, search],
    queryFn: () =>
      api.logs({
        from: iso(from),
        to: iso(to),
        app,
        service,
        severity,
        q: search,
        limit: '300',
      }),
  });

  const logs = data?.logs ?? [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <div>
          <h1 className="text-lg font-semibold">Logs OTLP</h1>
          <p className="text-sm text-muted">
            Logs enviados pelas APIs via <span className="mono">/v1/logs</span>, com salto para o trace.
          </p>
        </div>
        <button
          type="button"
          onClick={() => refetch()}
          className="ml-auto rounded-md border border-line px-3 py-1.5 text-sm hover:bg-surface"
        >
          Atualizar
        </button>
      </div>

      <div className="grid gap-2 rounded-lg border border-line bg-surface p-3 md:grid-cols-6">
        <label className="text-xs text-muted">
          De
          <input
            type="datetime-local"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
            className="mt-1 w-full rounded border border-line bg-canvas px-2 py-1 text-sm text-ink"
          />
        </label>
        <label className="text-xs text-muted">
          Até
          <input
            type="datetime-local"
            value={to}
            onChange={(e) => setTo(e.target.value)}
            className="mt-1 w-full rounded border border-line bg-canvas px-2 py-1 text-sm text-ink"
          />
        </label>
        <label className="text-xs text-muted">
          App
          <select
            value={app}
            onChange={(e) => setApp(e.target.value)}
            className="mt-1 w-full rounded border border-line bg-canvas px-2 py-1 text-sm text-ink"
          >
            <option value="">todos</option>
            {(options?.apps ?? []).map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
        </label>
        <label className="text-xs text-muted">
          Serviço
          <input
            value={service}
            onChange={(e) => setService(e.target.value)}
            placeholder="elama-api"
            className="mt-1 w-full rounded border border-line bg-canvas px-2 py-1 text-sm text-ink"
          />
        </label>
        <label className="text-xs text-muted">
          Severidade
          <select
            value={severity}
            onChange={(e) => setSeverity(e.target.value)}
            className="mt-1 w-full rounded border border-line bg-canvas px-2 py-1 text-sm text-ink"
          >
            {SEVERITIES.map((s) => (
              <option key={s} value={s}>
                {s || 'todas'}
              </option>
            ))}
          </select>
        </label>
        <label className="text-xs text-muted">
          Busca
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="texto, trace_id…"
            className="mt-1 w-full rounded border border-line bg-canvas px-2 py-1 text-sm text-ink"
          />
        </label>
      </div>

      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}
      {!isLoading && logs.length === 0 ? (
        <EmptyState message="Nenhum log nessa janela. Confira se a API do app tem OTEL_EXPORTER_OTLP_ENDPOINT apontando para otlp.ndovu.solutions e uma chave com OTLP habilitado." />
      ) : null}

      {logs.length > 0 ? (
        <div className="overflow-x-auto rounded-lg border border-line">
          <table className="w-full text-left text-sm">
            <thead className="bg-surface text-xs uppercase text-muted">
              <tr>
                <th className="px-3 py-2">Hora</th>
                <th className="px-3 py-2">Sev.</th>
                <th className="px-3 py-2">Serviço</th>
                <th className="px-3 py-2">Mensagem</th>
                <th className="px-3 py-2">Trace</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((l, i) => (
                <tr key={`${l.time}-${i}`} className="border-t border-line align-top">
                  <td className="whitespace-nowrap px-3 py-2 text-xs text-muted">
                    {fmtDateTime(l.time)}
                  </td>
                  <td className="px-3 py-2">
                    <span
                      className={
                        l.severityText === 'error' || l.severityText === 'fatal'
                          ? 'rounded bg-red-500/15 px-1.5 py-0.5 text-xs font-medium text-red-400'
                          : 'rounded bg-surface px-1.5 py-0.5 text-xs text-muted'
                      }
                    >
                      {l.severityText || '—'}
                    </span>
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-xs">
                    {l.serviceName}
                    <div className="text-muted">{l.app}</div>
                  </td>
                  <td className="px-3 py-2">
                    <pre className="whitespace-pre-wrap break-words font-mono text-xs">{l.body}</pre>
                  </td>
                  <td className="px-3 py-2 text-xs">
                    {l.traceId ? (
                      <Link href={`/traces/${l.traceId}`} className="text-accent hover:underline">
                        abrir
                      </Link>
                    ) : (
                      '—'
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </div>
  );
}
