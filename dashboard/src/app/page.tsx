'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useState } from 'react';
import { EventsChart } from '@/components/EventsChart';
import { Button, Select } from '@/components/form';
import { ErrorState, LoadingState, StatTile } from '@/components/ui';
import { api } from '@/lib/api';
import { fmtDuration, fmtNumber, RANGE_PRESETS, rangeToInterval } from '@/lib/time';

export default function OverviewPage() {
  const [range, setRange] = useState('24h');
  const [app, setApp] = useState('');
  const { data: options } = useQuery({ queryKey: ['filters'], queryFn: api.filterOptions });

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ['overview', range, app],
    queryFn: () => {
      const { from, to } = rangeToInterval(range);
      return api.overview({ from, to, app: app || undefined });
    },
    refetchInterval: 30_000,
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-xl font-semibold">Visão geral</h1>
        <div className="flex gap-2">
          <Button onClick={() => refetch()} loading={isFetching}>
            {isFetching ? 'atualizando…' : 'Atualizar'}
          </Button>
          <Select
            value={app}
            onChange={(e) => setApp(e.target.value)}
            aria-label="Filtrar por app"
            className="w-40"
          >
            <option value="">todos os apps</option>
            {options?.apps.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </Select>
          <Select
            value={range}
            onChange={(e) => setRange(e.target.value)}
            aria-label="Janela de tempo"
            className="w-44"
          >
            {RANGE_PRESETS.map((p) => (
              <option key={p.key} value={p.key}>
                {p.label}
              </option>
            ))}
          </Select>
        </div>
      </div>

      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}

      {data ? (
        <>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
            <StatTile label="Eventos" value={data.totalEvents} />
            <StatTile label="Sessões" value={data.totalSessions} />
            <StatTile label="Usuários" value={data.totalUsers} />
            <StatTile
              label="Erros"
              value={data.totalErrors}
              hint={`${(data.errorRate * 100).toFixed(1)}% dos eventos`}
              tone={data.totalErrors > 0 ? 'danger' : undefined}
            />
            <StatTile label="Duração média" value={fmtDuration(Math.round(data.avgDurationMs))} />
          </div>

          <section className="card px-4 py-3">
            <h2 className="mb-2 text-sm font-medium text-ink-2">Eventos ao longo do tempo</h2>
            {data.series.length > 0 ? (
              <EventsChart series={data.series} />
            ) : (
              <p className="py-10 text-center text-sm text-muted">Sem eventos na janela.</p>
            )}
          </section>

          <div className="grid gap-4 lg:grid-cols-2">
            <section className="card px-4 py-3">
              <h2 className="mb-2 text-sm font-medium text-ink-2">Top rotas</h2>
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-left text-xs uppercase tracking-wide text-muted">
                    <th className="py-1.5 font-medium">Rota</th>
                    <th className="py-1.5 text-right font-medium">Chamadas</th>
                    <th className="py-1.5 text-right font-medium">Erros</th>
                    <th className="py-1.5 text-right font-medium">média</th>
                    <th className="py-1.5 text-right font-medium">p95</th>
                  </tr>
                </thead>
                <tbody>
                  {data.topRoutes.map((r) => (
                    <tr key={r.url} className="border-t border-hairline">
                      <td className="max-w-52 py-1.5 pr-2">
                        <Link
                          href={`/traces?route=${encodeURIComponent(r.url)}`}
                          className="mono block truncate text-xs text-accent hover:underline"
                        >
                          {r.url}
                        </Link>
                      </td>
                      <td className="tabular py-1.5 text-right">{fmtNumber(r.count)}</td>
                      <td className={`tabular py-1.5 text-right ${r.errors > 0 ? 'text-critical' : ''}`}>
                        {fmtNumber(r.errors)}
                      </td>
                      <td className="tabular py-1.5 text-right text-ink-2">
                        {fmtDuration(Math.round(r.avgMs))}
                      </td>
                      <td className="tabular py-1.5 text-right text-ink-2">
                        {fmtDuration(Math.round(r.p95Ms))}
                      </td>
                    </tr>
                  ))}
                  {data.topRoutes.length === 0 ? (
                    <tr>
                      <td colSpan={5} className="py-6 text-center text-muted">
                        Sem chamadas HTTP na janela.
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </section>

            <section className="card px-4 py-3">
              <h2 className="mb-2 text-sm font-medium text-ink-2">Top erros</h2>
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-left text-xs uppercase tracking-wide text-muted">
                    <th className="py-1.5 font-medium">Código</th>
                    <th className="py-1.5 font-medium">Mensagem</th>
                    <th className="py-1.5 text-right font-medium">Ocorrências</th>
                  </tr>
                </thead>
                <tbody>
                  {data.topErrors.map((e) => (
                    <tr key={e.code} className="border-t border-hairline">
                      <td className="py-1.5 pr-2">
                        <Link
                          href={`/traces?onlyErrors=true&search=${encodeURIComponent(e.code)}`}
                          className="mono text-xs text-critical hover:underline"
                        >
                          {e.code}
                        </Link>
                      </td>
                      <td className="max-w-64 truncate py-1.5 pr-2 text-xs text-ink-2">
                        {e.message || '—'}
                      </td>
                      <td className="tabular py-1.5 text-right">{fmtNumber(e.count)}</td>
                    </tr>
                  ))}
                  {data.topErrors.length === 0 ? (
                    <tr>
                      <td colSpan={3} className="py-6 text-center text-muted">
                        Nenhum erro na janela. 🎉
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </section>
          </div>
        </>
      ) : null}
    </div>
  );
}
