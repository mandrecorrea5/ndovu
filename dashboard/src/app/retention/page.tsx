'use client';

import { useQuery } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { Button, Select } from '@/components/form';
import { EmptyState, ErrorState, LoadingState } from '@/components/ui';
import { api } from '@/lib/api';
import { fmtNumber, rangeToInterval } from '@/lib/time';

const RANGE_OPTIONS = [
  { key: '30d', label: 'Últimos 30 dias' },
  { key: '60d', label: 'Últimos 60 dias' },
  { key: '90d', label: 'Últimos 90 dias' },
];

// rangeToInterval só conhece até 30d — construímos manualmente para 60/90.
function windowFor(key: string): { from: string; to: string } {
  if (key === '60d' || key === '90d') {
    const days = key === '60d' ? 60 : 90;
    const to = new Date();
    const from = new Date(to.getTime() - days * 24 * 3600_000);
    return { from: from.toISOString(), to: to.toISOString() };
  }
  return rangeToInterval(key);
}

// heatColor devolve uma cor baseada na fração 0..1 — verde forte para retenção
// alta, fundo neutro para baixa. Usa tokens do theme (accent) para casar com
// o resto da UI. Fórmula: hue estável, alpha escala com a fração.
function heatStyle(pct: number): React.CSSProperties {
  if (pct <= 0) return { background: 'transparent' };
  return {
    background: `color-mix(in oklab, var(--series-1) ${Math.min(pct * 100, 95).toFixed(0)}%, transparent)`,
    color: pct > 0.5 ? 'var(--surface-1)' : 'var(--text-primary)',
  };
}

const PCT = (n: number) => `${(n * 100).toFixed(1)}%`;

export default function RetentionPage() {
  const [range, setRange] = useState('30d');
  const [app, setApp] = useState('');
  const [cohortBy, setCohortBy] = useState('week');

  const dateRange = useMemo(() => windowFor(range), [range]);

  const { data: options } = useQuery({ queryKey: ['filters'], queryFn: api.filterOptions });
  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ['retention', range, app, cohortBy],
    queryFn: () =>
      api.retention({
        from: dateRange.from,
        to: dateRange.to,
        app: app || undefined,
        cohortBy,
      }),
  });

  const cohortLabel = (iso: string) => {
    const d = new Date(iso);
    return d.toLocaleDateString('pt-BR', { day: '2-digit', month: '2-digit', year: '2-digit' });
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Retenção</h1>
          <p className="text-sm text-ink-2">
            Coortes de usuários por período de primeiro acesso × % que retornaram em D1/D7/D14/D30.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Select value={app} onChange={(e) => setApp(e.target.value)} className="w-40">
            <option value="">todos os apps</option>
            {options?.apps.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </Select>
          <Select value={cohortBy} onChange={(e) => setCohortBy(e.target.value)} className="w-32">
            <option value="week">semanal</option>
            <option value="day">diário</option>
            <option value="month">mensal</option>
          </Select>
          <Select value={range} onChange={(e) => setRange(e.target.value)} className="w-44">
            {RANGE_OPTIONS.map((p) => (
              <option key={p.key} value={p.key}>
                {p.label}
              </option>
            ))}
          </Select>
          <Button onClick={() => refetch()} loading={isFetching}>
            {isFetching ? 'atualizando…' : 'Atualizar'}
          </Button>
        </div>
      </div>

      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}
      {(() => {
        // Tolerante a backend antigo que devolve cohorts:null ou offsetsDays:null.
        const cohorts = data?.cohorts ?? [];
        const offsets = data?.offsetsDays ?? [];
        if (!data || isLoading) return null;
        if (cohorts.length === 0) {
          return (
            <EmptyState message="Sem coortes na janela. Aumente o período ou verifique se o SDK está enviando userId." />
          );
        }
        return (
          <div className="card overflow-x-auto">
            <table className="w-full min-w-[560px] text-sm">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wide text-muted">
                  <th className="px-3 py-2 font-medium">Coorte</th>
                  <th className="px-3 py-2 text-right font-medium">Novos users</th>
                  {offsets.map((d) => (
                    <th key={d} className="px-3 py-2 text-center font-medium">
                      D{d}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {cohorts.map((c) => (
                  <tr key={c.cohort} className="border-t border-hairline">
                    <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                      {cohortLabel(c.cohort)}
                    </td>
                    <td className="tabular px-3 py-2 text-right">{fmtNumber(c.newUsers)}</td>
                    {offsets.map((d) => {
                      const key = `D${d}`;
                      const pct = (c.retainedPct ?? {})[key] ?? 0;
                      const n = (c.retained ?? {})[key] ?? 0;
                      return (
                        <td
                          key={d}
                          className="tabular px-3 py-2 text-center text-xs"
                          style={heatStyle(pct)}
                          title={`${n} de ${c.newUsers} usuários voltaram`}
                        >
                          {c.newUsers > 0 ? PCT(pct) : '—'}
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        );
      })()}
    </div>
  );
}
