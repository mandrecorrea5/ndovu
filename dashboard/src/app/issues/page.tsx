'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import Link from 'next/link';
import { useState } from 'react';
import { Button, Checkbox, Select } from '@/components/form';
import { EmptyState, ErrorState, LoadingState } from '@/components/ui';
import { api, type Issue } from '@/lib/api';
import { canEdit, getSessionUser } from '@/lib/auth';
import { fmtDateTime, fmtNumber, RANGE_PRESETS, rangeToInterval } from '@/lib/time';

const STATUS_LABEL: Record<NonNullable<Issue['status']>, string> = {
  open: 'aberta',
  investigating: 'investigando',
  resolved: 'resolvida',
  ignored: 'ignorada',
};

const STATUS_CLASS: Record<NonNullable<Issue['status']>, string> = {
  open: 'border-critical/40 text-critical',
  investigating: 'border-warn/40 text-warn',
  resolved: 'border-good/40 text-good',
  ignored: 'border-hairline text-muted',
};

export default function IssuesPage() {
  const [range, setRange] = useState('24h');
  const [app, setApp] = useState('');
  const [onlyOpen, setOnlyOpen] = useState(true);
  const [onlyMine, setOnlyMine] = useState(false);
  const qc = useQueryClient();

  const { data: options } = useQuery({ queryKey: ['filters'], queryFn: api.filterOptions });

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ['issues', range, app, onlyOpen, onlyMine],
    queryFn: () => {
      const { from, to } = rangeToInterval(range);
      return api.issues({
        from,
        to,
        app: app || undefined,
        onlyOpen: onlyOpen ? 'true' : undefined,
        assignee: onlyMine ? 'me' : undefined,
      });
    },
    refetchInterval: 30_000,
  });

  const patch = useMutation({
    mutationFn: (input: { fingerprint: string; status: Issue['status']; app: string }) =>
      api.patchIssue(input.fingerprint, { status: input.status!, app: input.app }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['issues'] }),
  });

  const editable = canEdit(getSessionUser());

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Issues</h1>
          <p className="text-sm text-ink-2">Erros agrupados por fingerprint — triage e resolva.</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Checkbox
            checked={onlyOpen}
            onChange={(e) => setOnlyOpen(e.target.checked)}
            label="só abertas"
          />
          <Checkbox
            checked={onlyMine}
            onChange={(e) => setOnlyMine(e.target.checked)}
            label="atribuídas a mim"
          />
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
      {data && data.issues.length === 0 ? (
        <EmptyState message="Nenhuma issue na janela — está tudo verde por aqui." />
      ) : null}

      {data && data.issues.length > 0 ? (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-plane text-left text-xs uppercase tracking-wide text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">Erro</th>
                <th className="px-3 py-2 font-medium">App</th>
                <th className="px-3 py-2 text-right font-medium">Ocorrências</th>
                <th className="px-3 py-2 text-right font-medium">Usuários</th>
                <th className="px-3 py-2 text-right font-medium">Impacto</th>
                <th className="px-3 py-2 font-medium">Última vez</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Responsável</th>
                <th className="px-3 py-2 font-medium">Ações</th>
              </tr>
            </thead>
            <tbody>
              {data.issues.map((issue) => (
                <tr key={issue.fingerprint} className="border-t border-hairline hover:bg-plane/50">
                  <td className="max-w-96 px-3 py-2">
                    <Link
                      href={`/issues/${encodeURIComponent(issue.fingerprint)}`}
                      className="block"
                    >
                      <div className="mono text-xs text-critical">{issue.code || '—'}</div>
                      <div className="truncate text-ink hover:underline">
                        {issue.message || issue.name}
                      </div>
                      {issue.sampleUrl ? (
                        <div className="mono truncate text-[11px] text-muted">
                          {issue.sampleUrl}
                        </div>
                      ) : null}
                    </Link>
                  </td>
                  <td className="px-3 py-2 text-ink-2">{issue.app}</td>
                  <td className="tabular px-3 py-2 text-right">{fmtNumber(issue.count)}</td>
                  <td className="tabular px-3 py-2 text-right">{fmtNumber(issue.affectedUsers)}</td>
                  <td className="tabular px-3 py-2 text-right text-ink-2">
                    {issue.impactScore.toFixed(1)}
                  </td>
                  <td className="px-3 py-2 text-xs text-ink-2">{fmtDateTime(issue.lastSeen)}</td>
                  <td className="px-3 py-2 text-xs">
                    <span
                      className={`rounded-md border px-1.5 py-0.5 ${STATUS_CLASS[issue.status ?? 'open']}`}
                    >
                      {STATUS_LABEL[issue.status ?? 'open']}
                    </span>
                  </td>
                  <td className="px-3 py-2 text-xs text-ink-2">
                    {issue.assigneeName ?? <span className="text-muted">—</span>}
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex flex-wrap gap-1">
                      <Link
                        href={`/traces?onlyErrors=true&app=${encodeURIComponent(issue.app)}&search=${encodeURIComponent(issue.code || issue.name)}`}
                        className="inline-flex items-center rounded-md border border-hairline px-2 py-1 text-xs text-ink-2 hover:bg-plane"
                      >
                        ver eventos
                      </Link>
                      {editable && issue.status !== 'resolved' ? (
                        <Button
                          size="sm"
                          className="border-good/40 text-good hover:bg-good/10"
                          onClick={() =>
                            patch.mutate({
                              fingerprint: issue.fingerprint,
                              status: 'resolved',
                              app: issue.app,
                            })
                          }
                        >
                          resolver
                        </Button>
                      ) : editable ? (
                        <Button
                          size="sm"
                          onClick={() =>
                            patch.mutate({
                              fingerprint: issue.fingerprint,
                              status: 'open',
                              app: issue.app,
                            })
                          }
                        >
                          reabrir
                        </Button>
                      ) : null}
                      {editable && issue.status !== 'ignored' ? (
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() =>
                            patch.mutate({
                              fingerprint: issue.fingerprint,
                              status: 'ignored',
                              app: issue.app,
                            })
                          }
                        >
                          ignorar
                        </Button>
                      ) : null}
                    </div>
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
