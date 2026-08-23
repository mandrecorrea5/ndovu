'use client';

import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { fmtDateTime } from '@/lib/time';

/**
 * SnapshotViewer — renderiza o HTML do snapshot dentro de um iframe sandbox.
 * Fetch é lazy (só quando montado no drill do evento).
 *
 * Segurança: iframe usa sandbox="" (deny-all) — bloqueia scripts, forms,
 * navegação e mesmo-origin. O CSP no back é uma segunda camada.
 *
 * Não usamos src={URL} porque o iframe não anexa nosso Bearer token; usamos
 * srcDoc com o HTML carregado via fetch autenticado.
 */
export function SnapshotViewer({ eventId }: { eventId: string }) {
  const meta = useQuery({
    queryKey: ['snapshot-meta', eventId],
    queryFn: () => api.snapshotMeta(eventId),
    retry: false, // 404 é caso comum (nem todo error tem snapshot)
  });

  const html = useQuery({
    queryKey: ['snapshot-html', eventId],
    queryFn: () => api.snapshotHTML(eventId),
    enabled: meta.data !== undefined,
  });

  const [expanded, setExpanded] = useState(false);

  // Não existe snapshot pra esse evento — não polui a UI.
  if (meta.isError || (!meta.isLoading && !meta.data)) return null;

  if (meta.isLoading) {
    return (
      <p className="mt-2 text-xs text-muted">Verificando snapshot de tela…</p>
    );
  }

  const m = meta.data!;
  return (
    <div className="mt-3 overflow-hidden rounded-md border border-hairline bg-surface">
      <header className="flex items-center justify-between border-b border-hairline px-3 py-1.5">
        <p className="text-xs font-medium text-ink-2">
          📸 Snapshot capturado{' '}
          {m.url ? (
            <span className="mono ml-1 text-muted">em {m.url}</span>
          ) : null}
          <span className="mx-2 text-muted">·</span>
          <span className="text-muted">
            {fmtDateTime(m.takenAt)} · {(m.sizeBytes / 1024).toFixed(1)} KB gzip
            {m.viewportW && m.viewportH ? ` · ${m.viewportW}×${m.viewportH}` : ''}
          </span>
        </p>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            setExpanded((v) => !v);
          }}
          className="rounded-md border border-hairline px-2 py-0.5 text-xs text-ink-2 hover:bg-plane"
        >
          {expanded ? 'recolher' : 'expandir'}
        </button>
      </header>
      {expanded ? (
        html.isLoading ? (
          <p className="p-3 text-xs text-muted">Carregando HTML…</p>
        ) : html.error ? (
          <p className="p-3 text-xs text-critical">
            Falha ao carregar: {(html.error as Error).message}
          </p>
        ) : (
          <SnapshotFrame html={html.data ?? ''} />
        )
      ) : (
        <p className="p-3 text-xs text-muted">
          Clique em "expandir" para renderizar. O HTML é aberto em iframe
          sandboxed (sem execução de script).
        </p>
      )}
    </div>
  );
}

/**
 * SnapshotFrame usa Blob URL para conteúdo grande — evita limite de srcDoc
 * (~2MB no Chrome). O sandbox vazio nega tudo (scripts, forms, submits).
 */
function SnapshotFrame({ html }: { html: string }) {
  const [src, setSrc] = useState('');
  useEffect(() => {
    const blob = new Blob([html], { type: 'text/html' });
    const url = URL.createObjectURL(blob);
    setSrc(url);
    return () => URL.revokeObjectURL(url);
  }, [html]);
  return (
    <iframe
      title="session snapshot"
      src={src}
      sandbox=""
      className="h-[500px] w-full border-0 bg-white"
    />
  );
}
