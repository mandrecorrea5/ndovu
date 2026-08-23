'use client';

import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Button, Field, Input, Select } from '@/components/form';
import { EmptyState, ErrorState, LoadingState } from '@/components/ui';
import { api, type AuditEntry } from '@/lib/api';
import { fmtDateTime, fmtNumber } from '@/lib/time';

const PAGE_SIZE = 50;

// Ações conhecidas — usado no dropdown. Se novas ações forem introduzidas
// no backend, o filtro texto continua funcionando; esse é só o quick pick.
const KNOWN_ACTIONS = [
  'user.create', 'user.update',
  'app.create', 'app.update', 'app.delete',
  'company.create', 'company.update', 'company.delete',
  'apikey.create', 'apikey.revoke',
  'alert.create', 'alert.delete',
  'sourcemap.upload', 'sourcemap.delete',
  'issue.update',
  'digest.send_now',
];

export default function AuditLogPage() {
  const [action, setAction] = useState('');
  const [actor, setActor] = useState('');
  const [offset, setOffset] = useState(0);

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ['audit-log', action, actor, offset],
    queryFn: () =>
      api.auditLog({
        action: action || undefined,
        actor: actor || undefined,
        limit: String(PAGE_SIZE),
        offset: String(offset),
      }),
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Audit log</h1>
          <p className="text-sm text-ink-2">
            Registro imutável de ações administrativas — quem fez o quê, quando, com qual IP.
            Insert-only por design.
          </p>
        </div>
        <Button onClick={() => refetch()} loading={isFetching}>
          {isFetching ? 'atualizando…' : 'Atualizar'}
        </Button>
      </div>

      <div className="card flex flex-wrap items-end gap-3 px-4 py-3">
        <Field label="Ação" className="w-56">
          <Select value={action} onChange={(e) => { setAction(e.target.value); setOffset(0); }}>
            <option value="">todas</option>
            {KNOWN_ACTIONS.map((a) => (
              <option key={a} value={a}>{a}</option>
            ))}
          </Select>
        </Field>
        <Field label="Actor userId" className="w-64" hint="uuid do usuário que executou">
          <Input
            value={actor}
            onChange={(e) => setActor(e.target.value)}
            placeholder="feb1770a-6f1c-…"
          />
        </Field>
        <Button onClick={() => { setOffset(0); refetch(); }}>Consultar</Button>
      </div>

      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}
      {data && data.entries.length === 0 ? (
        <EmptyState message="Nenhuma entrada com esse filtro." />
      ) : null}

      {data && data.entries.length > 0 ? (
        <div className="card overflow-x-auto">
          <table className="w-full min-w-[900px] text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wide text-muted">
                <th className="px-3 py-2 font-medium">Quando</th>
                <th className="px-3 py-2 font-medium">Quem</th>
                <th className="px-3 py-2 font-medium">Ação</th>
                <th className="px-3 py-2 font-medium">Recurso</th>
                <th className="px-3 py-2 font-medium">Detalhes</th>
                <th className="px-3 py-2 font-medium">IP</th>
              </tr>
            </thead>
            <tbody>
              {data.entries.map((e: AuditEntry) => (
                <tr key={e.id} className="border-t border-hairline align-top">
                  <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                    {fmtDateTime(e.createdAt)}
                  </td>
                  <td className="px-3 py-2 text-xs">
                    <div className="font-medium text-ink">{e.actorEmail || '—'}</div>
                    {e.userAgent ? (
                      <div className="mono truncate text-[10px] text-muted" title={e.userAgent}>
                        {e.userAgent.slice(0, 30)}
                      </div>
                    ) : null}
                  </td>
                  <td className="px-3 py-2">
                    <span className="mono inline-flex items-center rounded-md border border-hairline px-1.5 py-0.5 text-xs text-accent">
                      {e.action}
                    </span>
                  </td>
                  <td className="px-3 py-2 text-xs text-ink-2">
                    {e.resourceType ? (
                      <>
                        <div>{e.resourceType}</div>
                        {e.resourceId ? (
                          <div className="mono truncate text-[10px] text-muted">{e.resourceId}</div>
                        ) : null}
                      </>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td className="px-3 py-2 text-xs">
                    {e.details && Object.keys(e.details).length > 0 ? (
                      <pre className="mono max-w-md overflow-hidden text-ellipsis whitespace-pre-wrap break-all rounded bg-plane p-1.5 text-[10px] text-ink-2">
                        {JSON.stringify(e.details, null, 0)}
                      </pre>
                    ) : (
                      <span className="text-muted">—</span>
                    )}
                  </td>
                  <td className="mono px-3 py-2 text-xs text-ink-2">{e.ip || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>

          <div className="flex items-center justify-between border-t border-hairline px-4 py-2 text-xs text-muted">
            <span className="tabular">
              {offset + 1}–{Math.min(offset + PAGE_SIZE, data.total)} de {fmtNumber(data.total)}
            </span>
            <div className="flex gap-2">
              <Button
                size="sm"
                disabled={offset === 0}
                onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
              >
                ← anterior
              </Button>
              <Button
                size="sm"
                disabled={offset + PAGE_SIZE >= data.total}
                onClick={() => setOffset(offset + PAGE_SIZE)}
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
