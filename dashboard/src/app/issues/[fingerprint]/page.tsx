'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useMemo, useState } from 'react';
import { EventRow, EventsTableHead } from '@/components/EventRow';
import { Button, Field, Select, Textarea } from '@/components/form';
import { EmptyState, ErrorState, LoadingState, StatTile } from '@/components/ui';
import { api, type Issue, type IssueComment } from '@/lib/api';
import { canEdit, getSessionUser } from '@/lib/auth';
import { fmtDateTime, fmtNumber, rangeToInterval } from '@/lib/time';
import { jiraUrl, linearUrl } from '@/lib/tracker';

const STATUS_LABEL: Record<NonNullable<Issue['status']>, string> = {
  open: 'aberta',
  investigating: 'investigando',
  resolved: 'resolvida',
  ignored: 'ignorada',
};

export default function IssueDrillPage() {
  const params = useParams<{ fingerprint: string }>();
  const fingerprint = params.fingerprint;
  const qc = useQueryClient();
  const me = getSessionUser();
  const editable = canEdit(me);

  // Precisamos achar o issue completo — reusa /v1/issues com janela ampla.
  // Simples: pega os últimos 30 dias e filtra pelo fingerprint na resposta.
  const range = useMemo(() => rangeToInterval('30d'), []);
  const {
    data: issueList,
    isLoading,
    error,
  } = useQuery({
    queryKey: ['issue', fingerprint],
    queryFn: () => api.issues({ from: range.from, to: range.to, limit: '500' }),
  });
  const issue = issueList?.issues.find((i) => i.fingerprint === fingerprint);

  const commentsQ = useQuery({
    queryKey: ['issue-comments', fingerprint],
    queryFn: () => api.listIssueComments(fingerprint),
  });

  // Timeline: últimos eventos com esse code/message.
  const eventsQ = useQuery({
    queryKey: ['issue-events', fingerprint, issue?.app, issue?.code],
    enabled: !!issue,
    queryFn: () =>
      api.events({
        from: range.from,
        to: range.to,
        app: issue?.app,
        onlyErrors: 'true',
        search: issue?.code || issue?.name,
        limit: '20',
      }),
  });

  const usersQ = useQuery({
    queryKey: ['admin-users'],
    queryFn: api.listUsers,
    enabled: me?.role === 'admin',
  });

  const patch = useMutation({
    mutationFn: (input: {
      status?: Issue['status'];
      assigneeUserId?: string;
    }) =>
      api.patchIssue(fingerprint, {
        status: input.status ?? issue?.status ?? 'open',
        assigneeUserId: input.assigneeUserId,
        app: issue?.app,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['issue', fingerprint] }),
  });

  const [comment, setComment] = useState('');
  const addComment = useMutation({
    mutationFn: () => api.postIssueComment(fingerprint, comment),
    onSuccess: () => {
      setComment('');
      qc.invalidateQueries({ queryKey: ['issue-comments', fingerprint] });
    },
  });
  const removeComment = useMutation({
    mutationFn: (id: string) => api.deleteIssueComment(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['issue-comments', fingerprint] }),
  });

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorState message={(error as Error).message} />;
  if (!issue) return <EmptyState message="Issue não encontrada na janela de 30 dias." />;

  const ndovuUrl = typeof window !== 'undefined' ? location.href : '';
  const jira = jiraUrl(issue, ndovuUrl);
  const linear = linearUrl(issue, ndovuUrl);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <Link href="/issues" className="text-xs text-accent hover:underline">
            ← todas as issues
          </Link>
          <h1 className="mt-1 text-xl font-semibold">
            <span className="mono text-critical">{issue.code || '—'}</span>{' '}
            <span className="text-ink">{issue.message || issue.name}</span>
          </h1>
          <p className="mt-0.5 text-xs text-muted">
            app <span className="text-ink-2">{issue.app}</span> ·
            fingerprint <span className="mono text-ink-2">{issue.fingerprint}</span>
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {jira ? (
            <Button
              onClick={() => window.open(jira, '_blank')}
              icon="↗"
            >
              criar no Jira
            </Button>
          ) : null}
          {linear ? (
            <Button
              onClick={() => window.open(linear, '_blank')}
              icon="↗"
            >
              criar no Linear
            </Button>
          ) : null}
          <Button
            onClick={() => navigator.clipboard?.writeText(ndovuUrl)}
            icon="⧉"
          >
            copiar link
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile label="Ocorrências" value={issue.count} />
        <StatTile
          label="Usuários afetados"
          value={issue.affectedUsers}
          tone={issue.affectedUsers > 10 ? 'danger' : undefined}
        />
        <StatTile
          label="Impacto"
          value={issue.impactScore.toFixed(1)}
          hint="log(count) × users × recência"
        />
        <StatTile
          label="Última vez"
          value={fmtDateTime(issue.lastSeen)}
          hint={`primeira: ${fmtDateTime(issue.firstSeen)}`}
        />
      </div>

      {/* --- Triagem: status + assignee --- */}
      <section className="card space-y-3 px-4 py-3">
        <h2 className="text-sm font-semibold">Triagem</h2>
        <div className="grid gap-3 md:grid-cols-2">
          <Field label="Status" hint={!editable ? 'só editor ou admin altera' : undefined}>
            <Select
              value={issue.status ?? 'open'}
              disabled={!editable}
              onChange={(e) => patch.mutate({ status: e.target.value as Issue['status'] })}
            >
              {(Object.keys(STATUS_LABEL) as Array<keyof typeof STATUS_LABEL>).map((s) => (
                <option key={s} value={s}>
                  {STATUS_LABEL[s]}
                </option>
              ))}
            </Select>
          </Field>
          <Field
            label="Responsável"
            hint={!editable ? 'só editor ou admin reatribui' : undefined}
          >
            <Select
              value={issue.assigneeUserId ?? ''}
              disabled={!editable || usersQ.isLoading}
              onChange={(e) => patch.mutate({ assigneeUserId: e.target.value })}
            >
              <option value="">— sem responsável —</option>
              {usersQ.data?.users
                .filter((u) => u.active)
                .map((u) => (
                  <option key={u.id} value={u.id}>
                    {u.name} · {u.email}
                  </option>
                ))}
            </Select>
          </Field>
        </div>
        {issue.assigneeName ? (
          <p className="text-xs text-ink-2">
            Atribuída a <span className="font-medium text-ink">{issue.assigneeName}</span>{' '}
            <span className="text-muted">({issue.assigneeEmail})</span>
          </p>
        ) : null}
      </section>

      {/* --- Comentários --- */}
      <section className="card space-y-3 px-4 py-3">
        <h2 className="text-sm font-semibold">Comentários</h2>
        {editable ? (
          <form
            className="space-y-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (comment.trim()) addComment.mutate();
            }}
          >
            <Textarea
              placeholder="Adicione um comentário — contexto, hipótese, link para PR…"
              value={comment}
              onChange={(e) => setComment(e.target.value)}
              maxLength={5000}
            />
            <div className="flex justify-end">
              <Button
                type="submit"
                variant="primary"
                loading={addComment.isPending}
                disabled={!comment.trim()}
              >
                comentar
              </Button>
            </div>
          </form>
        ) : (
          <p className="text-xs text-muted">
            Apenas editores e administradores podem comentar.
          </p>
        )}

        {commentsQ.data?.comments.length === 0 ? (
          <p className="text-xs text-muted">Sem comentários ainda.</p>
        ) : (
          <ul className="space-y-2">
            {commentsQ.data?.comments.map((c: IssueComment) => (
              <li key={c.id} className="rounded-md border border-hairline bg-plane/60 px-3 py-2">
                <header className="flex items-center justify-between text-xs">
                  <div>
                    <span className="font-medium text-ink">{c.authorName || 'anônimo'}</span>{' '}
                    <span className="text-muted">· {fmtDateTime(c.createdAt)}</span>
                  </div>
                  {me?.id === c.authorId ? (
                    <button
                      type="button"
                      onClick={() => {
                        if (confirm('Remover este comentário?')) removeComment.mutate(c.id);
                      }}
                      className="text-xs text-muted hover:text-critical"
                    >
                      remover
                    </button>
                  ) : null}
                </header>
                <p className="mt-1 whitespace-pre-wrap text-sm text-ink">{c.body}</p>
              </li>
            ))}
          </ul>
        )}
      </section>

      {/* --- Timeline dos últimos eventos --- */}
      <section className="card overflow-x-auto">
        <header className="border-b border-hairline px-4 py-2">
          <h2 className="text-sm font-semibold">
            Últimas ocorrências
            <span className="ml-2 text-xs font-normal text-muted">
              {eventsQ.data ? `${fmtNumber(eventsQ.data.events.length)} eventos` : ''}
            </span>
          </h2>
        </header>
        {eventsQ.isLoading ? <LoadingState /> : null}
        {eventsQ.data && eventsQ.data.events.length > 0 ? (
          <table className="w-full text-sm">
            <EventsTableHead />
            <tbody>
              {eventsQ.data.events.map((event) => (
                <EventRow key={event.eventId} event={event} />
              ))}
            </tbody>
          </table>
        ) : null}
      </section>
    </div>
  );
}
