'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import Link from 'next/link';
import { useState } from 'react';
import { Button, Select } from '@/components/form';
import { ErrorState, LoadingState } from '@/components/ui';
import { api, type UserFeedback } from '@/lib/api';
import { fmtDateTime } from '@/lib/time';

const STATUS_LABEL: Record<UserFeedback['status'], string> = {
  new: '● novo',
  triaging: '◐ triagem',
  resolved: '✓ resolvido',
  dismissed: '✕ descartado',
};

const STATUS_TONE: Record<UserFeedback['status'], string> = {
  new: 'text-critical',
  triaging: 'text-warn',
  resolved: 'text-good',
  dismissed: 'text-muted',
};

const TYPE_ICON: Record<UserFeedback['type'], string> = {
  bug: '🐞',
  suggestion: '💡',
  praise: '❤',
  other: '•',
};

export default function FeedbacksPage() {
  const qc = useQueryClient();
  const [statusFilter, setStatusFilter] = useState<'' | UserFeedback['status']>('new');
  const [appFilter, setAppFilter] = useState<string>('');

  const appsQ = useQuery({ queryKey: ['admin-apps'], queryFn: api.listApps });
  const listQ = useQuery({
    queryKey: ['feedbacks', statusFilter, appFilter],
    queryFn: () =>
      api.listFeedbacks({
        limit: '100',
        status: statusFilter || undefined,
        app: appFilter || undefined,
      }),
    refetchInterval: 60_000,
  });

  const setStatus = useMutation({
    mutationFn: ({ id, status }: { id: string; status: UserFeedback['status'] }) =>
      api.setFeedbackStatus(id, status),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['feedbacks'] }),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteFeedback(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['feedbacks'] }),
  });

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">Feedback do usuário</h1>
        <p className="text-sm text-ink-2">
          Mensagens enviadas pelo widget do SDK (bug/sugestão/elogio). Cada feedback traz o
          sessionId e o último eventId — clique pra ir direto na sessão ou no replay do erro que
          motivou.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <label className="text-xs uppercase tracking-wide text-muted">Status</label>
        <Select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value as typeof statusFilter)}
        >
          <option value="">todos</option>
          <option value="new">novos</option>
          <option value="triaging">em triagem</option>
          <option value="resolved">resolvidos</option>
          <option value="dismissed">descartados</option>
        </Select>
        <label className="ml-4 text-xs uppercase tracking-wide text-muted">App</label>
        <Select value={appFilter} onChange={(e) => setAppFilter(e.target.value)}>
          <option value="">todos</option>
          {appsQ.data?.apps.map((a) => (
            <option key={a.id} value={a.name}>
              {a.name}
            </option>
          ))}
        </Select>
        {listQ.data ? (
          <span className="ml-auto text-xs text-muted">
            {listQ.data.total} feedback{listQ.data.total === 1 ? '' : 's'}
          </span>
        ) : null}
      </div>

      {listQ.error ? <ErrorState message={(listQ.error as Error).message} /> : null}
      {listQ.isLoading ? <LoadingState /> : null}

      {listQ.data && listQ.data.feedbacks.length === 0 ? (
        <p className="rounded-md border border-hairline p-6 text-center text-sm text-muted">
          Nenhum feedback com os filtros atuais. Monte o widget nas apps com{' '}
          <code className="mono">sdk.mountFeedbackWidget()</code>.
        </p>
      ) : null}

      {listQ.data && listQ.data.feedbacks.length > 0 ? (
        <div className="space-y-3">
          {listQ.data.feedbacks.map((f) => (
            <FeedbackCard
              key={f.id}
              feedback={f}
              onSetStatus={(s) => setStatus.mutate({ id: f.id, status: s })}
              onDelete={() => {
                if (confirm('Remover este feedback?')) remove.mutate(f.id);
              }}
              busy={setStatus.isPending || remove.isPending}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}

function FeedbackCard({
  feedback,
  onSetStatus,
  onDelete,
  busy,
}: {
  feedback: UserFeedback;
  onSetStatus: (status: UserFeedback['status']) => void;
  onDelete: () => void;
  busy: boolean;
}) {
  return (
    <article className="card space-y-3 p-4">
      <div className="flex flex-wrap items-start gap-3">
        <div className="text-lg leading-none">{TYPE_ICON[feedback.type]}</div>
        <div className="flex-1 min-w-0">
          <div className="flex flex-wrap items-baseline gap-2">
            <span className="mono text-xs text-ink-2">{feedback.app}</span>
            <span className="text-xs uppercase tracking-wide text-muted">{feedback.type}</span>
            <span className={`text-xs ${STATUS_TONE[feedback.status]}`}>
              {STATUS_LABEL[feedback.status]}
            </span>
            <span className="ml-auto tabular text-xs text-muted">
              {fmtDateTime(feedback.createdAt)}
            </span>
          </div>
          <p className="mt-1 whitespace-pre-wrap text-sm">{feedback.message}</p>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3 text-xs text-ink-2">
        {feedback.email ? (
          <a href={`mailto:${feedback.email}`} className="underline hover:text-ink-1">
            {feedback.email}
          </a>
        ) : null}
        {feedback.url ? (
          <a
            href={feedback.url}
            target="_blank"
            rel="noreferrer"
            className="mono truncate max-w-[400px] underline hover:text-ink-1"
            title={feedback.url}
          >
            {feedback.url}
          </a>
        ) : null}
        {feedback.viewportW && feedback.viewportH ? (
          <span className="tabular text-muted">
            {feedback.viewportW}×{feedback.viewportH}
          </span>
        ) : null}
      </div>

      <div className="flex flex-wrap items-center gap-2 border-t border-hairline pt-3">
        <Link
          href={`/sessions/${encodeURIComponent(feedback.sessionId)}`}
          className="text-xs underline hover:text-ink-1"
        >
          ver sessão + timeline
        </Link>
        {feedback.eventId ? (
          <span
            className="mono truncate max-w-[200px] text-xs text-muted"
            title={`Último eventId visto: ${feedback.eventId}`}
          >
            event {feedback.eventId.slice(0, 8)}…
          </span>
        ) : null}
        <div className="ml-auto flex gap-1">
          {feedback.status !== 'triaging' ? (
            <Button size="sm" disabled={busy} onClick={() => onSetStatus('triaging')}>
              triagem
            </Button>
          ) : null}
          {feedback.status !== 'resolved' ? (
            <Button
              size="sm"
              variant="primary"
              disabled={busy}
              onClick={() => onSetStatus('resolved')}
            >
              resolver
            </Button>
          ) : null}
          {feedback.status !== 'dismissed' ? (
            <Button size="sm" disabled={busy} onClick={() => onSetStatus('dismissed')}>
              descartar
            </Button>
          ) : null}
          <Button size="sm" variant="danger" disabled={busy} onClick={onDelete}>
            remover
          </Button>
        </div>
      </div>
    </article>
  );
}
