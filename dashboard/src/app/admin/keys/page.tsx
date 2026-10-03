'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Button, Field, Input, Select } from '@/components/form';
import { Modal, ModalActions } from '@/components/Modal';
import { ErrorState, LoadingState } from '@/components/ui';
import { api, ApiError, type CreatedApiKey } from '@/lib/api';
import { fmtDateTime } from '@/lib/time';

type FormState = { app: string; label: string };
const EMPTY: FormState = { app: '', label: '' };

export default function AdminKeysPage() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({ queryKey: ['admin-keys'], queryFn: api.listKeys });
  const { data: appsData } = useQuery({ queryKey: ['admin-apps'], queryFn: api.listApps });

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<FormState>(EMPTY);
  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  const [feedback, setFeedback] = useState('');
  const [revealed, setRevealed] = useState<Record<string, boolean>>({});

  const openNew = () => {
    setForm(EMPTY);
    setFeedback('');
    setOpen(true);
  };

  const create = useMutation({
    mutationFn: () => api.createKey(form),
    onSuccess: (key) => {
      setCreated(key);
      qc.invalidateQueries({ queryKey: ['admin-keys'] });
      setOpen(false);
    },
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro'),
  });

  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeKey(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin-keys'] }),
    onError: (err) => setFeedback(err instanceof ApiError ? err.message : 'Erro ao revogar'),
  });

  const rotate = (app: string, label?: string) => {
    setForm({ app, label: label ?? '' });
    setFeedback('');
    setOpen(true);
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Chaves de API</h1>
          <p className="text-sm text-ink-2">
            Cada frontend emissor usa a própria chave no header <code className="mono">X-Api-Key</code>.
            As chaves ficam cifradas e disponíveis para consulta administrativa. Gerar uma nova
            revoga a ativa anterior e preserva o histórico.
          </p>
        </div>
        <Button variant="primary" onClick={openNew} icon="+">
          Nova chave
        </Button>
      </div>

      {created ? (
        <div className="card border-good/40 px-4 py-3">
          <p className="text-sm font-medium">
            Chave criada para <span className="mono">{created.app}</span>. Ela também ficará
            disponível no histórico abaixo:
          </p>
          <div className="mt-2 flex items-center gap-2">
            <code className="mono flex-1 overflow-x-auto rounded-md bg-plane px-3 py-2 text-xs">
              {created.key}
            </code>
            <Button size="sm" onClick={() => navigator.clipboard?.writeText(created.key)}>
              copiar
            </Button>
            <Button size="sm" onClick={() => setCreated(null)}>
              fechar
            </Button>
          </div>
        </div>
      ) : null}

      {feedback && !open ? <p className="text-sm text-ink-2">{feedback}</p> : null}
      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}

      {data ? (
        <div className="card overflow-x-auto">
          <table className="w-full min-w-[980px] text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wide text-muted">
                <th className="px-3 py-2 font-medium">App</th>
                <th className="px-3 py-2 font-medium">Descrição</th>
                <th className="px-3 py-2 font-medium">Chave</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Criada em</th>
                <th className="px-3 py-2 font-medium">Revogada em</th>
                <th className="px-3 py-2 font-medium">Ações</th>
              </tr>
            </thead>
            <tbody>
              {data.keys.map((k) => (
                <tr key={k.id} className="border-t border-hairline">
                  <td className="px-3 py-2 font-medium">{k.app}</td>
                  <td className="max-w-56 truncate px-3 py-2 text-xs text-ink-2">
                    {k.label || '—'}
                  </td>
                  <td className="px-3 py-2">
                    {k.key ? (
                      <div className="flex items-center gap-1">
                        <code className="mono max-w-56 truncate text-xs">
                          {revealed[k.id] ? k.key : `${k.prefix}…`}
                        </code>
                        <Button
                          size="sm"
                          onClick={() =>
                            setRevealed((state) => ({ ...state, [k.id]: !state[k.id] }))
                          }
                        >
                          {revealed[k.id] ? 'ocultar' : 'ver'}
                        </Button>
                        <Button size="sm" onClick={() => navigator.clipboard?.writeText(k.key!)}>
                          copiar
                        </Button>
                      </div>
                    ) : (
                      <span className="text-xs text-muted">não recuperável (chave antiga)</span>
                    )}
                  </td>
                  <td className="px-3 py-2">
                    {k.active ? (
                      <span className="text-good">● ativa</span>
                    ) : (
                      <span className="text-muted" title={k.revokedAt ? fmtDateTime(k.revokedAt) : ''}>
                        ○ revogada
                      </span>
                    )}
                  </td>
                  <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                    {fmtDateTime(k.createdAt)}
                  </td>
                  <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                    {k.revokedAt ? fmtDateTime(k.revokedAt) : '—'}
                  </td>
                  <td className="px-3 py-2">
                    {k.active ? (
                      <div className="flex gap-1">
                        <Button
                          size="sm"
                          variant="danger"
                          loading={revoke.isPending}
                          onClick={() => revoke.mutate(k.id)}
                        >
                          revogar
                        </Button>
                        <Button size="sm" loading={create.isPending} onClick={() => rotate(k.app, k.label)}>
                          gerar nova
                        </Button>
                      </div>
                    ) : null}
                  </td>
                </tr>
              ))}
              {data.keys.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-3 py-8 text-center text-muted">
                    Nenhuma chave ainda — comece pelo botão “Nova chave”.
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      ) : null}

      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title="Nova chave de API"
        description="A chave será armazenada cifrada no histórico. A chave ativa anterior deste app será revogada."
        size="sm"
      >
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <Field label="App emissor" required htmlFor="key-app">
            <Select
              id="key-app"
              required
              value={form.app}
              onChange={(e) => setForm({ ...form, app: e.target.value })}
            >
              <option value="">— selecione —</option>
              {appsData?.apps.map((a) => (
                <option key={a.id} value={a.name}>
                  {a.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Descrição" htmlFor="key-label">
            <Input
              id="key-label"
              placeholder="ex.: produção web"
              value={form.label}
              onChange={(e) => setForm({ ...form, label: e.target.value })}
            />
          </Field>
          {feedback ? <p className="text-sm text-critical">{feedback}</p> : null}
          <ModalActions>
            <Button onClick={() => setOpen(false)}>cancelar</Button>
            <Button variant="primary" type="submit" loading={create.isPending}>
              gerar chave
            </Button>
          </ModalActions>
        </form>
      </Modal>
    </div>
  );
}
