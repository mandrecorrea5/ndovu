'use client';

import type { EventType } from '@/lib/types';
import { fmtNumber } from '@/lib/time';

/** Rótulos e ícones por tipo de evento — identidade nunca é só cor. */
export const TYPE_META: Record<EventType, { label: string; icon: string }> = {
  page_view: { label: 'Tela', icon: '👁' },
  action: { label: 'Ação', icon: '⚡' },
  http_request: { label: 'HTTP', icon: '⇄' },
  error: { label: 'Erro', icon: '✕' },
  custom: { label: 'Custom', icon: '◆' },
};

export function TypeBadge({ type }: { type: EventType }) {
  const meta = TYPE_META[type] ?? { label: type, icon: '·' };
  return (
    <span className="inline-flex items-center gap-1 rounded-md border border-hairline px-1.5 py-0.5 text-xs text-ink-2">
      <span aria-hidden>{meta.icon}</span>
      {meta.label}
    </span>
  );
}

export function StatusBadge({ status }: { status?: number }) {
  if (status == null) return <span className="text-muted">—</span>;
  const cls =
    status >= 500
      ? 'text-critical'
      : status >= 400
        ? 'text-warn'
        : 'text-good';
  const icon = status >= 400 ? '✕' : '✓';
  return (
    <span className={`tabular inline-flex items-center gap-1 text-sm font-medium ${cls}`}>
      <span aria-hidden>{icon}</span>
      {status}
    </span>
  );
}

export function ErrorPill({ code }: { code?: string }) {
  if (!code) return null;
  return (
    <span className="mono inline-flex items-center gap-1 rounded-md bg-critical/10 px-1.5 py-0.5 text-xs text-critical">
      <span aria-hidden>✕</span>
      {code}
    </span>
  );
}

export function StatTile({
  label,
  value,
  hint,
  tone,
}: {
  label: string;
  value: string | number;
  hint?: string;
  tone?: 'danger';
}) {
  return (
    <div className="card px-4 py-3">
      <p className="text-xs uppercase tracking-wide text-muted">{label}</p>
      <p className={`mt-1 text-2xl font-semibold ${tone === 'danger' ? 'text-critical' : ''}`}>
        {typeof value === 'number' ? fmtNumber(value) : value}
      </p>
      {hint ? <p className="mt-0.5 text-xs text-ink-2">{hint}</p> : null}
    </div>
  );
}

export function EmptyState({ message }: { message: string }) {
  return (
    <div className="card flex items-center justify-center px-6 py-12 text-sm text-ink-2">
      {message}
    </div>
  );
}

export function LoadingState() {
  return (
    <div className="card flex items-center justify-center px-6 py-12 text-sm text-muted">
      Carregando…
    </div>
  );
}

export function ErrorState({ message }: { message: string }) {
  return (
    <div className="card border-critical/40 px-6 py-4 text-sm text-critical" role="alert">
      {message}
    </div>
  );
}
