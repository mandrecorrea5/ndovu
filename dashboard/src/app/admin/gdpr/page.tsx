'use client';

import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';
import { Button, Field, Input } from '@/components/form';
import { ErrorState } from '@/components/ui';
import { api, ApiError } from '@/lib/api';

/**
 * LGPD/GDPR — portabilidade (export) e direito ao esquecimento (forget) para
 * um `userId` do end-user (o mesmo que o SDK envia em `session.userId`).
 * Não confundir com user do backoffice. Todas as ações vão para o audit log.
 */
export default function GDPRPage() {
  const [userId, setUserId] = useState('');
  const [message, setMessage] = useState('');

  const doExport = useMutation({
    mutationFn: () => api.gdprExport(userId),
    onSuccess: (data) => {
      // Cria um blob JSON e força download no browser.
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `ndovu-export-${data.userId}.json`;
      a.click();
      URL.revokeObjectURL(url);
      setMessage(`Exportado — ${data.eventCount} evento(s) baixados.`);
    },
    onError: (err) => setMessage(err instanceof ApiError ? err.message : 'Erro'),
  });

  const doForget = useMutation({
    mutationFn: () => api.gdprForget(userId),
    onSuccess: (data) => {
      setMessage(
        `Solicitação registrada. ${data.note} ClickHouse aplica async — pode levar alguns minutos até desaparecer das consultas.`,
      );
      setUserId('');
    },
    onError: (err) => setMessage(err instanceof ApiError ? err.message : 'Erro'),
  });

  const confirmForget = () => {
    if (!userId) return;
    if (
      confirm(
        `Apagar TODOS os eventos do user "${userId}"? Ação irreversível — recomendado exportar antes.`,
      )
    ) {
      doForget.mutate();
    }
  };

  return (
    <div className="max-w-2xl space-y-4">
      <div>
        <h1 className="text-xl font-semibold">LGPD / GDPR</h1>
        <p className="mt-1 text-sm text-ink-2">
          Portabilidade e direito ao esquecimento para <strong>usuários finais</strong> do seu app
          (identificados pelo <code className="mono">userId</code> que o SDK envia).
          Todas as ações são registradas no{' '}
          <a href="/admin/audit-log" className="text-accent hover:underline">
            audit log
          </a>
          .
        </p>
      </div>

      <div className="card space-y-3 px-4 py-3">
        <Field label="userId (do end-user)" required htmlFor="gdpr-user">
          <Input
            id="gdpr-user"
            placeholder="ex.: user-42 ou uuid do seu sistema"
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
          />
        </Field>

        <div className="flex flex-wrap gap-2">
          <Button
            variant="primary"
            disabled={!userId}
            loading={doExport.isPending}
            onClick={() => doExport.mutate()}
          >
            Exportar dados (portabilidade)
          </Button>
          <Button
            variant="danger"
            disabled={!userId}
            loading={doForget.isPending}
            onClick={confirmForget}
          >
            Esquecer usuário (delete)
          </Button>
        </div>

        {message ? (
          <p className="text-sm text-ink-2">{message}</p>
        ) : null}
      </div>

      <div className="card space-y-2 px-4 py-3 text-xs text-ink-2">
        <h2 className="text-sm font-semibold text-ink">Sobre a implementação</h2>
        <ul className="list-inside list-disc space-y-1">
          <li>
            <strong>Export</strong>: gera um JSON com todos os eventos do usuário (limite 10k),
            baixado localmente no seu browser.
          </li>
          <li>
            <strong>Forget</strong>: dispara um{' '}
            <code className="mono">ALTER TABLE trace_events DELETE</code> no ClickHouse. É
            assíncrono — as merges de partição aplicam a exclusão em background.
          </li>
          <li>
            <strong>Audit</strong>: cada ação grava uma entrada em <code className="mono">audit_log</code>{' '}
            com actor, IP e user-agent — retenção legal do próprio evento de exclusão.
          </li>
        </ul>
      </div>

      {(doExport.error || doForget.error) && !message ? (
        <ErrorState message={((doExport.error ?? doForget.error) as Error).message} />
      ) : null}
    </div>
  );
}
