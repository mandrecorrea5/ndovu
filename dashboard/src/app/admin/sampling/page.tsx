'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Button, Checkbox, Field, Input, Select } from '@/components/form';
import { Modal, ModalActions } from '@/components/Modal';
import { ErrorState, LoadingState } from '@/components/ui';
import { api, ApiError, type SamplingRule } from '@/lib/api';
import { fmtDateTime } from '@/lib/time';

const EVENT_TYPES = ['', 'page_view', 'action', 'http_request', 'error', 'custom'];

type FormState = {
  app: string;
  eventType: string;
  sampleRate: number;
  keepErrors: boolean;
  active: boolean;
  note: string;
};
const EMPTY: FormState = {
  app: '',
  eventType: '',
  sampleRate: 1,
  keepErrors: true,
  active: true,
  note: '',
};

export default function SamplingPage() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ['sampling-rules'],
    queryFn: api.listSamplingRules,
  });
  const { data: appsData } = useQuery({ queryKey: ['admin-apps'], queryFn: api.listApps });

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<SamplingRule | null>(null);
  const [form, setForm] = useState<FormState>(EMPTY);
  const [feedback, setFeedback] = useState('');

  const openNew = () => {
    setEditing(null);
    setForm(EMPTY);
    setFeedback('');
    setOpen(true);
  };

  const openEdit = (r: SamplingRule) => {
    setEditing(r);
    setForm({
      app: r.app,
      eventType: r.eventType,
      sampleRate: r.sampleRate,
      keepErrors: r.keepErrors,
      active: r.active,
      note: r.note ?? '',
    });
    setFeedback('');
    setOpen(true);
  };

  const save = useMutation({
    mutationFn: () => {
      if (editing) return api.updateSamplingRule(editing.id, form);
      return api.createSamplingRule(form);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sampling-rules'] });
      setOpen(false);
    },
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro'),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteSamplingRule(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sampling-rules'] }),
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Sampling adaptativo</h1>
          <p className="text-sm text-ink-2">
            Descarta uma fração dos eventos antes de gravar no ClickHouse. Precedência: regra
            mais específica ganha. Erros passam por bypass (<code className="mono">keepErrors</code>)
            para não perder sinais críticos ao amortizar volume.
          </p>
        </div>
        <Button variant="primary" onClick={openNew} icon="+">
          Nova regra
        </Button>
      </div>

      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}

      {data ? (
        <div className="card overflow-x-auto">
          <table className="w-full min-w-[900px] text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wide text-muted">
                <th className="px-3 py-2 font-medium">Escopo</th>
                <th className="px-3 py-2 text-right font-medium">Taxa</th>
                <th className="px-3 py-2 font-medium">Bypass erros</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Nota</th>
                <th className="px-3 py-2 font-medium">Atualizada</th>
                <th className="px-3 py-2 font-medium">Ações</th>
              </tr>
            </thead>
            <tbody>
              {data.rules.map((r) => (
                <tr key={r.id} className="border-t border-hairline">
                  <td className="px-3 py-2 text-xs">
                    <span className="mono">
                      {r.app || <span className="text-muted">*</span>}
                    </span>
                    <span className="mx-1 text-muted">·</span>
                    <span className="mono">
                      {r.eventType || <span className="text-muted">*</span>}
                    </span>
                  </td>
                  <td
                    className={`tabular px-3 py-2 text-right font-medium ${
                      r.sampleRate < 0.5 ? 'text-warn' : ''
                    }`}
                  >
                    {(r.sampleRate * 100).toFixed(0)}%
                  </td>
                  <td className="px-3 py-2 text-xs">
                    {r.keepErrors ? (
                      <span className="text-good">● sim</span>
                    ) : (
                      <span className="text-critical">○ não</span>
                    )}
                  </td>
                  <td className="px-3 py-2 text-xs">
                    <span className={r.active ? 'text-good' : 'text-muted'}>
                      {r.active ? '● ativa' : '○ inativa'}
                    </span>
                  </td>
                  <td className="max-w-64 truncate px-3 py-2 text-xs text-ink-2">
                    {r.note || '—'}
                  </td>
                  <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                    {fmtDateTime(r.updatedAt)}
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex gap-1">
                      <Button size="sm" onClick={() => openEdit(r)}>
                        editar
                      </Button>
                      <Button
                        size="sm"
                        variant="danger"
                        loading={remove.isPending}
                        onClick={() => {
                          if (confirm(`Remover regra ${r.app || '*'} · ${r.eventType || '*'}?`))
                            remove.mutate(r.id);
                        }}
                      >
                        remover
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
              {data.rules.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-3 py-8 text-center text-muted">
                    Nenhuma regra. Sem sampling, tudo é gravado no ClickHouse.
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
        title={editing ? 'Editar regra' : 'Nova regra de sampling'}
        description="Deixe app ou tipo vazio para casar com todos (wildcard)."
      >
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <div className="grid grid-cols-2 gap-3">
            <Field label="App (vazio = todos)">
              <Select value={form.app} onChange={(e) => setForm({ ...form, app: e.target.value })}>
                <option value="">— todos —</option>
                {appsData?.apps.map((a) => (
                  <option key={a.id} value={a.name}>
                    {a.name}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Tipo (vazio = todos)">
              <Select
                value={form.eventType}
                onChange={(e) => setForm({ ...form, eventType: e.target.value })}
              >
                {EVENT_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {t || '— todos —'}
                  </option>
                ))}
              </Select>
            </Field>
          </div>

          <Field
            label={`Taxa de amostragem (${(form.sampleRate * 100).toFixed(0)}%)`}
            hint="0 = descarta tudo. 1 = mantém tudo. 0.2 = mantém 20%."
          >
            <Input
              type="range"
              min={0}
              max={1}
              step={0.05}
              value={form.sampleRate}
              onChange={(e) => setForm({ ...form, sampleRate: Number(e.target.value) })}
            />
          </Field>

          <Checkbox
            checked={form.keepErrors}
            onChange={(e) => setForm({ ...form, keepErrors: e.target.checked })}
            label="Manter sempre erros (HasError ou http_status ≥ 500) — bypass da taxa"
          />
          <Checkbox
            checked={form.active}
            onChange={(e) => setForm({ ...form, active: e.target.checked })}
            label="Ativa"
          />

          <Field label="Nota (opcional)">
            <Input
              placeholder="ex.: portal com pico em promoção; amortiza page_view"
              value={form.note}
              onChange={(e) => setForm({ ...form, note: e.target.value })}
            />
          </Field>

          {feedback ? <p className="text-sm text-critical">{feedback}</p> : null}
          <ModalActions>
            <Button onClick={() => setOpen(false)}>cancelar</Button>
            <Button variant="primary" type="submit" loading={save.isPending}>
              {editing ? 'salvar' : 'criar'}
            </Button>
          </ModalActions>
        </form>
      </Modal>
    </div>
  );
}
