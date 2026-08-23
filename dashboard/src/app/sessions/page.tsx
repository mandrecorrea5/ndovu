'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useState } from 'react';
import { Button, Checkbox, Field, Input, Select } from '@/components/form';
import { EmptyState, ErrorState, LoadingState } from '@/components/ui';
import { api } from '@/lib/api';
import { fmtDateTime, fmtDuration, fmtNumber, RANGE_PRESETS, rangeToInterval } from '@/lib/time';

export default function SessionsPage() {
  const [dRange, setDRange] = useState('24h');
  const [dApp, setDApp] = useState('');
  const [dUserId, setDUserId] = useState('');
  const [dOnlyErrors, setDOnlyErrors] = useState(false);
  const [dFrom, setDFrom] = useState('');
  const [dTo, setDTo] = useState('');
  const [app, setApp] = useState('');
  const [userId, setUserId] = useState('');
  const [onlyErrors, setOnlyErrors] = useState(false);
  const [from, setFrom] = useState<string | undefined>(() => rangeToInterval('24h').from);
  const [to, setTo] = useState<string | undefined>(() => rangeToInterval('24h').to);
  const [offset, setOffset] = useState(0);
  const limit = 25;

  const { data: options } = useQuery({ queryKey: ['filters'], queryFn: api.filterOptions });
  const { data, isLoading, error } = useQuery({
    queryKey: ['sessions', from, to, app, userId, onlyErrors, offset],
    queryFn: () =>
      api.sessions({
        from,
        to,
        app: app || undefined,
        userId: userId || undefined,
        onlyErrors: onlyErrors ? 'true' : undefined,
        limit: String(limit),
        offset: String(offset),
      }),
  });

  const applyFilters = () => {
    setApp(dApp);
    setUserId(dUserId);
    setOnlyErrors(dOnlyErrors);
    if (dFrom || dTo) {
      setFrom(dFrom ? new Date(dFrom).toISOString() : undefined);
      setTo(dTo ? new Date(dTo).toISOString() : undefined);
    } else {
      const { from: f, to: t } = rangeToInterval(dRange);
      setFrom(f);
      setTo(t);
    }
    setOffset(0);
  };

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Sessões</h1>

      <div className="card flex flex-wrap items-end gap-3 px-4 py-3">
        <Field label="Período" className="w-40">
          <Select value={dRange} onChange={(e) => setDRange(e.target.value)}>
            {RANGE_PRESETS.map((p) => (
              <option key={p.key} value={p.key}>{p.label}</option>
            ))}
          </Select>
        </Field>
        <Field label="App" className="w-40">
          <Select value={dApp} onChange={(e) => setDApp(e.target.value)}>
            <option value="">todos os apps</option>
            {options?.apps.map((a) => (
              <option key={a} value={a}>{a}</option>
            ))}
          </Select>
        </Field>
        <Field label="Usuário (userId)" className="w-44">
          <Input
            placeholder="ex.: user-42"
            value={dUserId}
            onChange={(e) => setDUserId(e.target.value)}
          />
        </Field>
        <Field label="De">
          <Input
            type="datetime-local"
            value={dFrom}
            onChange={(e) => setDFrom(e.target.value)}
          />
        </Field>
        <Field label="Até">
          <Input
            type="datetime-local"
            value={dTo}
            onChange={(e) => setDTo(e.target.value)}
          />
        </Field>
        <div className="mb-1.5">
          <Checkbox
            checked={dOnlyErrors}
            onChange={(e) => setDOnlyErrors(e.target.checked)}
            label="Com erros"
          />
        </div>
        <Button variant="primary" onClick={applyFilters}>
          Consultar
        </Button>
      </div>

      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}
      {data && data.sessions.length === 0 ? (
        <EmptyState message="Nenhuma sessão com esses filtros." />
      ) : null}

      {data && data.sessions.length > 0 ? (
        <div className="card overflow-x-auto">
          <table className="w-full min-w-[760px] text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wide text-muted">
                <th className="px-3 py-2 font-medium">Sessão</th>
                <th className="px-3 py-2 font-medium">Usuário</th>
                <th className="px-3 py-2 font-medium">App</th>
                <th className="px-3 py-2 font-medium">Início</th>
                <th className="px-3 py-2 font-medium">Fim</th>
                <th className="px-3 py-2 text-right font-medium">Duração</th>
                <th className="px-3 py-2 text-right font-medium">Eventos</th>
                <th className="px-3 py-2 text-right font-medium">Erros</th>
              </tr>
            </thead>
            <tbody>
              {data.sessions.map((s) => (
                <tr key={s.sessionId} className="border-t border-hairline hover:bg-plane">
                  <td className="px-3 py-2">
                    <Link
                      href={`/sessions/${encodeURIComponent(s.sessionId)}`}
                      className="mono block max-w-56 truncate text-xs text-accent hover:underline"
                    >
                      {s.sessionId}
                    </Link>
                  </td>
                  <td className="mono max-w-40 truncate px-3 py-2 text-xs">{s.userId ?? 'anônimo'}</td>
                  <td className="px-3 py-2 text-xs text-ink-2">{s.app}</td>
                  <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                    {fmtDateTime(s.startedAt)}
                  </td>
                  <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                    {fmtDateTime(s.lastEventAt)}
                  </td>
                  <td className="tabular whitespace-nowrap px-3 py-2 text-right text-xs text-ink-2">
                    {fmtDuration(
                      Math.max(
                        0,
                        new Date(s.lastEventAt).getTime() - new Date(s.startedAt).getTime(),
                      ),
                    )}
                  </td>
                  <td className="tabular px-3 py-2 text-right">{fmtNumber(s.eventCount)}</td>
                  <td className={`tabular px-3 py-2 text-right ${s.errorCount > 0 ? 'font-medium text-critical' : 'text-muted'}`}>
                    {fmtNumber(s.errorCount)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="flex items-center justify-between border-t border-hairline px-4 py-2 text-xs text-muted">
            <span className="tabular">
              {offset + 1}–{Math.min(offset + limit, data.total)} de {fmtNumber(data.total)}
            </span>
            <div className="flex gap-2">
              <Button
                size="sm"
                disabled={offset === 0}
                onClick={() => setOffset(Math.max(0, offset - limit))}
              >
                ← anterior
              </Button>
              <Button
                size="sm"
                disabled={offset + limit >= data.total}
                onClick={() => setOffset(offset + limit)}
              >
                próxima →
              </Button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
