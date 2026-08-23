'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Button, Select } from '@/components/form';
import { api, ApiError, type App, type UserAppPermission } from '@/lib/api';

/**
 * UserPermissionsPanel — gerencia RBAC granular por app dentro do drill de
 * um usuário. Faz sentido só para viewers: admin de company vê tudo da
 * company automaticamente (o back devolve isso via /auth/me + tenantScope).
 *
 * Regras exibidas ao usuário do backoffice:
 *   - Só apps ATIVOS DA MESMA COMPANY podem ser concedidos (o back valida
 *     também; aqui filtramos client-side pra evitar tentativas visíveis).
 *   - Grant é idempotente; DELETE remove.
 */
export function UserPermissionsPanel({
  userId,
  companyId,
  role,
}: {
  userId: string;
  companyId: string;
  role: string;
}) {
  const qc = useQueryClient();
  const [selectedApp, setSelectedApp] = useState('');
  const [feedback, setFeedback] = useState('');

  // Só faz sentido carregar para viewers — admins têm tudo por default.
  const enabled = role === 'viewer';

  const perms = useQuery({
    queryKey: ['user-permissions', userId],
    queryFn: () => api.listUserPermissions(userId),
    enabled,
  });

  const appsQ = useQuery({
    queryKey: ['admin-apps'],
    queryFn: api.listApps,
    enabled,
  });

  const invalidate = () => qc.invalidateQueries({ queryKey: ['user-permissions', userId] });

  const grant = useMutation({
    mutationFn: (appId: string) => api.grantUserPermission(userId, appId, 'viewer'),
    onSuccess: () => {
      setSelectedApp('');
      setFeedback('');
      invalidate();
    },
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro'),
  });

  const revoke = useMutation({
    mutationFn: (appId: string) => api.revokeUserPermission(userId, appId),
    onSuccess: () => invalidate(),
    onError: (err) => setFeedback(err instanceof ApiError ? err.message : 'Erro'),
  });

  if (!enabled) {
    return (
      <div className="rounded-md border border-hairline bg-plane/40 px-3 py-2 text-xs text-muted">
        Admin da empresa vê todos os apps automaticamente — permissões
        granulares não se aplicam.
      </div>
    );
  }

  const granted = perms.data?.permissions ?? [];
  const grantedIds = new Set(granted.map((p) => p.appId));
  const availableApps: App[] = (appsQ.data?.apps ?? []).filter(
    (a) => a.companyId === companyId && !grantedIds.has(a.id),
  );

  return (
    <div className="space-y-2 rounded-md border border-hairline bg-plane/40 px-3 py-2">
      <p className="text-xs font-semibold uppercase tracking-wide text-muted">
        Apps que este viewer pode consultar
      </p>

      {granted.length === 0 ? (
        <p className="text-xs text-muted">
          Nenhum app concedido — este viewer não consegue ver nada até você adicionar.
        </p>
      ) : (
        <ul className="space-y-1">
          {granted.map((p: UserAppPermission) => (
            <li
              key={p.appId}
              className="flex items-center justify-between rounded border border-hairline bg-surface px-2 py-1 text-xs"
            >
              <span className="font-medium text-ink">{p.appName}</span>
              <Button
                size="sm"
                variant="danger"
                loading={revoke.isPending}
                onClick={() => revoke.mutate(p.appId)}
              >
                remover
              </Button>
            </li>
          ))}
        </ul>
      )}

      <div className="flex gap-2 pt-1">
        <Select
          value={selectedApp}
          onChange={(e) => setSelectedApp(e.target.value)}
          className="flex-1"
        >
          <option value="">— escolher app pra conceder —</option>
          {availableApps.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}
            </option>
          ))}
        </Select>
        <Button
          variant="primary"
          disabled={!selectedApp}
          loading={grant.isPending}
          onClick={() => grant.mutate(selectedApp)}
        >
          conceder
        </Button>
      </div>

      {availableApps.length === 0 && appsQ.data ? (
        <p className="text-xs text-muted">
          Todos os apps da empresa já foram concedidos (ou não há apps cadastrados nela).
        </p>
      ) : null}

      {feedback ? <p className="text-xs text-critical">{feedback}</p> : null}
    </div>
  );
}
