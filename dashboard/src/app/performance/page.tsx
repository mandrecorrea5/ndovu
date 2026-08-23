'use client';

import { useQuery } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { Button, Select } from '@/components/form';
import { EmptyState, ErrorState, LoadingState } from '@/components/ui';
import { api, type WebVitalStat } from '@/lib/api';
import { fmtNumber, RANGE_PRESETS, rangeToInterval } from '@/lib/time';

// Thresholds de rating alinhados com o SDK (web_vital_* -> good/needs/poor).
const THRESHOLDS: Record<string, [number, number]> = {
  LCP: [2500, 4000],
  INP: [200, 500],
  FID: [100, 300],
  CLS: [0.1, 0.25],
  TTFB: [800, 1800],
  FCP: [1800, 3000],
};

const HELP: Record<string, string> = {
  LCP: 'Largest Contentful Paint — quanto tempo para o maior elemento aparecer.',
  INP: 'Interaction to Next Paint — latência percebida da pior interação.',
  FID: 'First Input Delay — atraso da primeira interação do usuário.',
  CLS: 'Cumulative Layout Shift — quanto a página "pula" durante o carregamento.',
  TTFB: 'Time to First Byte — quanto tempo o servidor levou para começar a responder.',
  FCP: 'First Contentful Paint — quando algo aparece na tela pela primeira vez.',
};

function fmtValue(vital: string, v: number): string {
  if (vital === 'CLS') return v.toFixed(3);
  return `${v.toFixed(0)} ms`;
}

function rating(vital: string, value: number): 'good' | 'needs-improvement' | 'poor' {
  const t = THRESHOLDS[vital];
  if (!t) return 'good';
  if (value <= t[0]) return 'good';
  if (value <= t[1]) return 'needs-improvement';
  return 'poor';
}

export default function PerformancePage() {
  const [range, setRange] = useState('24h');
  const [app, setApp] = useState('');

  const { data: options } = useQuery({ queryKey: ['filters'], queryFn: api.filterOptions });

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ['vitals', range, app],
    queryFn: () => {
      const { from, to } = rangeToInterval(range);
      return api.vitals({ from, to, app: app || undefined });
    },
    refetchInterval: 60_000,
  });

  const byVital = useMemo(() => {
    const grouped: Record<string, WebVitalStat[]> = {};
    for (const v of data?.vitals ?? []) {
      (grouped[v.vital] ??= []).push(v);
    }
    return grouped;
  }, [data]);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Performance (Web Vitals)</h1>
          <p className="text-sm text-ink-2">
            Métricas de experiência real de usuário — p75 e p95 por rota.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={app}
            onChange={(e) => setApp(e.target.value)}
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
            className="w-44"
          >
            {RANGE_PRESETS.map((p) => (
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
      {data && data.vitals.length === 0 ? (
        <EmptyState message="Sem Web Vitals coletados nesta janela. Confirme se o SDK está com captureWebVitals: true." />
      ) : null}

      {Object.entries(byVital).map(([vital, rows]) => (
        <section key={vital} className="card overflow-x-auto">
          <header className="border-b border-hairline px-4 py-2">
            <h2 className="text-sm font-semibold">{vital}</h2>
            <p className="text-xs text-muted">{HELP[vital] ?? ''}</p>
          </header>
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase tracking-wide text-muted">
              <tr>
                <th className="px-4 py-1.5 font-medium">Rota</th>
                <th className="px-4 py-1.5 text-right font-medium">amostras</th>
                <th className="px-4 py-1.5 text-right font-medium">p75</th>
                <th className="px-4 py-1.5 text-right font-medium">p95</th>
                <th className="px-4 py-1.5 text-right font-medium">good/poor</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => {
                const r75 = rating(vital, r.p75);
                return (
                  <tr key={vital + r.screen} className="border-t border-hairline">
                    <td className="mono max-w-96 truncate px-4 py-1.5 text-xs text-ink-2">
                      {r.screen || '—'}
                    </td>
                    <td className="tabular px-4 py-1.5 text-right">{fmtNumber(r.count)}</td>
                    <td
                      className={`tabular px-4 py-1.5 text-right font-medium ${
                        r75 === 'poor'
                          ? 'text-critical'
                          : r75 === 'needs-improvement'
                            ? 'text-warn'
                            : 'text-good'
                      }`}
                    >
                      {fmtValue(vital, r.p75)}
                    </td>
                    <td className="tabular px-4 py-1.5 text-right text-ink-2">
                      {fmtValue(vital, r.p95)}
                    </td>
                    <td className="tabular px-4 py-1.5 text-right text-xs text-ink-2">
                      <span className="text-good">{fmtNumber(r.good)}</span>
                      <span className="mx-1 text-muted">/</span>
                      <span className="text-critical">{fmtNumber(r.poor)}</span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </section>
      ))}
    </div>
  );
}
