'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useMemo, useState } from 'react';
import { Button, Field, Select } from '@/components/form';
import { EmptyState, ErrorState, LoadingState, StatTile } from '@/components/ui';
import { api, type Release } from '@/lib/api';
import { fmtDateTime, fmtDuration, fmtNumber, RANGE_PRESETS, rangeToInterval } from '@/lib/time';

const PCT = (n: number) => `${(n * 100).toFixed(2)}%`;
const SIGNED = (n: number, unit = '') =>
  `${n >= 0 ? '+' : ''}${n.toFixed(2)}${unit}`;

export default function ReleasesPage() {
  const [range, setRange] = useState('7d');
  const [app, setApp] = useState('');
  const [releaseA, setReleaseA] = useState('');
  const [releaseB, setReleaseB] = useState('');

  const { data: options } = useQuery({ queryKey: ['filters'], queryFn: api.filterOptions });

  const window = useMemo(() => rangeToInterval(range), [range]);

  const {
    data,
    isLoading,
    error,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ['releases', range, app],
    queryFn: () => api.releases({ from: window.from, to: window.to, app: app || undefined }),
  });

  const compare = useQuery({
    queryKey: ['releases-compare', range, app, releaseA, releaseB],
    enabled: !!(releaseA && releaseB),
    queryFn: () =>
      api.compareReleases({
        from: window.from,
        to: window.to,
        app: app || undefined,
        releaseA,
        releaseB,
      }),
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Releases</h1>
          <p className="text-sm text-ink-2">
            Versões do app na janela. Escolha 2 para comparar (delta de error rate, duração e
            issues novas — regressão introduzida no deploy).
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
          <Select value={range} onChange={(e) => setRange(e.target.value)} className="w-44">
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
      {data && data.releases.length === 0 ? (
        <EmptyState message="Nenhuma release detectada. Configure `release` no createNdovu do SDK ou envie via campo `release` do evento." />
      ) : null}

      {data && data.releases.length > 0 ? (
        <div className="card overflow-x-auto">
          <table className="w-full min-w-[900px] text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wide text-muted">
                <th className="px-3 py-2 font-medium">Release</th>
                <th className="px-3 py-2 font-medium">App</th>
                <th className="px-3 py-2 text-right font-medium">Eventos</th>
                <th className="px-3 py-2 text-right font-medium">Erros</th>
                <th className="px-3 py-2 text-right font-medium">Error rate</th>
                <th className="px-3 py-2 text-right font-medium">Sessões</th>
                <th className="px-3 py-2 text-right font-medium">Usuários</th>
                <th className="px-3 py-2 text-right font-medium">Duração média</th>
                <th className="px-3 py-2 font-medium">Última atividade</th>
                <th className="px-3 py-2 font-medium">Ações</th>
              </tr>
            </thead>
            <tbody>
              {data.releases.map((rel: Release) => {
                const selectedA = releaseA === rel.release;
                const selectedB = releaseB === rel.release;
                return (
                  <tr key={rel.release + rel.app} className="border-t border-hairline">
                    <td className="mono px-3 py-2 font-medium">{rel.release}</td>
                    <td className="px-3 py-2 text-xs text-ink-2">{rel.app}</td>
                    <td className="tabular px-3 py-2 text-right">{fmtNumber(rel.events)}</td>
                    <td
                      className={`tabular px-3 py-2 text-right ${
                        rel.errors > 0 ? 'text-critical' : 'text-muted'
                      }`}
                    >
                      {fmtNumber(rel.errors)}
                    </td>
                    <td
                      className={`tabular px-3 py-2 text-right ${
                        rel.errorRate > 0.05 ? 'text-critical' : ''
                      }`}
                    >
                      {PCT(rel.errorRate)}
                    </td>
                    <td className="tabular px-3 py-2 text-right">{fmtNumber(rel.sessions)}</td>
                    <td className="tabular px-3 py-2 text-right">{fmtNumber(rel.users)}</td>
                    <td className="tabular px-3 py-2 text-right text-ink-2">
                      {fmtDuration(Math.round(rel.avgDurationMs))}
                    </td>
                    <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                      {fmtDateTime(rel.lastSeen)}
                    </td>
                    <td className="px-3 py-2">
                      <div className="flex flex-wrap gap-1">
                        <Button
                          size="sm"
                          variant={selectedA ? 'primary' : 'secondary'}
                          onClick={() => setReleaseA(selectedA ? '' : rel.release)}
                        >
                          A
                        </Button>
                        <Button
                          size="sm"
                          variant={selectedB ? 'primary' : 'secondary'}
                          onClick={() => setReleaseB(selectedB ? '' : rel.release)}
                        >
                          B
                        </Button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : null}

      {compare.data ? (
        <section className="card space-y-3 px-4 py-3">
          <header className="flex items-center justify-between">
            <h2 className="text-sm font-semibold">
              Comparação:{' '}
              <span className="mono text-accent">{compare.data.releaseA.release}</span>{' '}
              →{' '}
              <span className="mono text-accent">{compare.data.releaseB.release}</span>
            </h2>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setReleaseA('');
                setReleaseB('');
              }}
            >
              limpar
            </Button>
          </header>

          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <StatTile
              label="Δ Error rate"
              value={SIGNED(compare.data.deltaErrorRate * 100, '%')}
              tone={compare.data.deltaErrorRate > 0 ? 'danger' : undefined}
              hint={`A: ${PCT(compare.data.releaseA.errorRate)} → B: ${PCT(compare.data.releaseB.errorRate)}`}
            />
            <StatTile
              label="Δ Duração média"
              value={SIGNED(compare.data.deltaAvgMs, ' ms')}
              tone={compare.data.deltaAvgMs > 0 ? 'danger' : undefined}
              hint={`A: ${fmtDuration(Math.round(compare.data.releaseA.avgDurationMs))} → B: ${fmtDuration(Math.round(compare.data.releaseB.avgDurationMs))}`}
            />
            <StatTile
              label="Eventos B"
              value={compare.data.releaseB.events}
              hint={`vs ${fmtNumber(compare.data.releaseA.events)} em A`}
            />
            <StatTile
              label="Novas issues em B"
              value={compare.data.newIssues.length}
              tone={compare.data.newIssues.length > 0 ? 'danger' : undefined}
              hint="fingerprints que não existiam em A"
            />
          </div>

          {compare.data.newIssues.length > 0 ? (
            <div className="overflow-x-auto rounded-md border border-hairline">
              <table className="w-full text-sm">
                <thead className="bg-plane text-left text-xs uppercase tracking-wide text-muted">
                  <tr>
                    <th className="px-3 py-2 font-medium">Novo erro em B</th>
                    <th className="px-3 py-2 text-right font-medium">Ocorrências</th>
                    <th className="px-3 py-2 text-right font-medium">Usuários</th>
                    <th className="px-3 py-2 font-medium">Última vez</th>
                  </tr>
                </thead>
                <tbody>
                  {compare.data.newIssues.slice(0, 10).map((issue) => (
                    <tr key={issue.fingerprint} className="border-t border-hairline">
                      <td className="max-w-96 px-3 py-2">
                        <div className="mono text-xs text-critical">{issue.code || '—'}</div>
                        <div className="truncate text-ink">{issue.message || issue.name}</div>
                      </td>
                      <td className="tabular px-3 py-2 text-right">{fmtNumber(issue.count)}</td>
                      <td className="tabular px-3 py-2 text-right">
                        {fmtNumber(issue.affectedUsers)}
                      </td>
                      <td className="px-3 py-2 text-xs text-ink-2">
                        {fmtDateTime(issue.lastSeen)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {compare.data.newIssues.length > 10 ? (
                <p className="border-t border-hairline px-3 py-2 text-xs text-muted">
                  Mostrando 10 de {compare.data.newIssues.length} novas issues.{' '}
                  <Link
                    className="text-accent hover:underline"
                    href={`/issues?app=${encodeURIComponent(compare.data.releaseB.app)}&release=${encodeURIComponent(compare.data.releaseB.release)}`}
                  >
                    ver todas
                  </Link>
                </p>
              ) : null}
            </div>
          ) : (
            <p className="text-sm text-good">
              Nenhuma issue nova em B — release limpa em relação a A.
            </p>
          )}
        </section>
      ) : null}

      {compare.error ? (
        <ErrorState message={(compare.error as Error).message} />
      ) : null}
    </div>
  );
}
