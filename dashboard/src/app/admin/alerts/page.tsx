'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Button, Checkbox, Field, Input, Select } from '@/components/form';
import { Modal, ModalActions } from '@/components/Modal';
import { ErrorState, LoadingState } from '@/components/ui';
import { api, ApiError, type AlertRule } from '@/lib/api';
import { fmtDateTime } from '@/lib/time';

type FormState = {
  name: string;
  app: string;
  errorCode: string;
  threshold: number;
  windowSeconds: number;
  channel: 'slack' | 'webhook';
  targetUrl: string;
  silenceSeconds: number;
  active: boolean;
};

const EMPTY: FormState = {
  name: '',
  app: '',
  errorCode: '',
  threshold: 5,
  windowSeconds: 300,
  channel: 'slack',
  targetUrl: '',
  silenceSeconds: 900,
  active: true,
};

export default function AdminAlertsPage() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ['admin-alerts'],
    queryFn: api.listAlerts,
  });
  const { data: appsData } = useQuery({ queryKey: ['admin-apps'], queryFn: api.listApps });

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<FormState>(EMPTY);
  const [feedback, setFeedback] = useState('');

  const openNew = () => {
    setForm(EMPTY);
    setFeedback('');
    setOpen(true);
  };

  const create = useMutation({
    mutationFn: () => api.createAlert(form),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-alerts'] });
      setOpen(false);
    },
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro'),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteAlert(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin-alerts'] }),
    onError: (err) => setFeedback(err instanceof ApiError ? err.message : 'Erro'),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Alertas</h1>
          <p className="text-sm text-ink-2">
            Regras “se X erros em Y segundos, notifica Z”. O avaliador roda no writer a cada minuto
            (config <code className="mono">NDOVU_ALERTS_INTERVAL_SECONDS</code>) e respeita a janela
            de silêncio para não spamar.
          </p>
        </div>
        <Button variant="primary" onClick={openNew} icon="+">
          Nova regra
        </Button>
      </div>

      {feedback && !open ? <p className="text-sm text-ink-2">{feedback}</p> : null}
      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}

      {data ? (
        <div className="card overflow-x-auto">
          <table className="w-full min-w-[900px] text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wide text-muted">
                <th className="px-3 py-2 font-medium">Nome</th>
                <th className="px-3 py-2 font-medium">Escopo</th>
                <th className="px-3 py-2 font-medium">Threshold / janela</th>
                <th className="px-3 py-2 font-medium">Canal</th>
                <th className="px-3 py-2 font-medium">Criada</th>
                <th className="px-3 py-2 font-medium">Ações</th>
              </tr>
            </thead>
            <tbody>
              {data.alerts.map((r: AlertRule) => (
                <tr key={r.id} className="border-t border-hairline">
                  <td className="px-3 py-2 font-medium">{r.name}</td>
                  <td className="px-3 py-2 text-xs text-ink-2">
                    {r.app || 'todos'}
                    {r.errorCode ? (
                      <span className="mono ml-1 text-critical">· {r.errorCode}</span>
                    ) : null}
                  </td>
                  <td className="tabular px-3 py-2 text-xs text-ink-2">
                    ≥ {r.threshold} em {r.windowSeconds}s
                  </td>
                  <td className="px-3 py-2 text-xs">
                    <span className="rounded-md border border-hairline px-1.5 py-0.5">
                      {r.channel}
                    </span>
                  </td>
                  <td className="tabular px-3 py-2 text-xs text-ink-2">
                    {fmtDateTime(r.createdAt)}
                  </td>
                  <td className="px-3 py-2">
                    <Button
                      size="sm"
                      variant="danger"
                      loading={remove.isPending}
                      onClick={() => {
                        if (confirm(`Remover a regra "${r.name}"?`)) remove.mutate(r.id);
                      }}
                    >
                      remover
                    </Button>
                  </td>
                </tr>
              ))}
              {data.alerts.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-3 py-8 text-center text-muted">
                    Nenhuma regra ainda — comece por “erros 5xx em qualquer app”.
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
        title="Nova regra de alerta"
        description="Dispara quando o volume de erros na janela cruzar o threshold."
        size="lg"
      >
        <form
          className="grid grid-cols-2 gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <Field label="Nome" required htmlFor="alert-name">
            <Input
              id="alert-name"
              required
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="ex.: 5xx no portal"
            />
          </Field>
          <Field label="App (vazio = todos)" htmlFor="alert-app">
            <Select
              id="alert-app"
              value={form.app}
              onChange={(e) => setForm({ ...form, app: e.target.value })}
            >
              <option value="">todos</option>
              {appsData?.apps.map((a) => (
                <option key={a.id} value={a.name}>
                  {a.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Error code (opcional)" htmlFor="alert-code">
            <Input
              id="alert-code"
              value={form.errorCode}
              onChange={(e) => setForm({ ...form, errorCode: e.target.value })}
              placeholder="ex.: HTTP_500"
            />
          </Field>
          <Field label="Canal" htmlFor="alert-channel">
            <Select
              id="alert-channel"
              value={form.channel}
              onChange={(e) => setForm({ ...form, channel: e.target.value as FormState['channel'] })}
            >
              <option value="slack">Slack (webhook)</option>
              <option value="webhook">Webhook genérico</option>
            </Select>
          </Field>
          <Field label="Threshold (nº de erros)" required htmlFor="alert-th">
            <Input
              id="alert-th"
              type="number"
              min={1}
              required
              value={form.threshold}
              onChange={(e) => setForm({ ...form, threshold: Number(e.target.value) })}
            />
          </Field>
          <Field label="Janela (segundos)" required htmlFor="alert-win">
            <Input
              id="alert-win"
              type="number"
              min={30}
              required
              value={form.windowSeconds}
              onChange={(e) => setForm({ ...form, windowSeconds: Number(e.target.value) })}
            />
          </Field>
          <div className="col-span-2">
            <Field label="URL de destino" required htmlFor="alert-url">
              <Input
                id="alert-url"
                required
                value={form.targetUrl}
                onChange={(e) => setForm({ ...form, targetUrl: e.target.value })}
                placeholder="https://hooks.slack.com/services/…"
              />
            </Field>
          </div>
          <Field label="Silêncio (segundos)" htmlFor="alert-silence">
            <Input
              id="alert-silence"
              type="number"
              min={0}
              value={form.silenceSeconds}
              onChange={(e) => setForm({ ...form, silenceSeconds: Number(e.target.value) })}
            />
          </Field>
          <div className="flex items-end">
            <Checkbox
              checked={form.active}
              onChange={(e) => setForm({ ...form, active: e.target.checked })}
              label="ativa"
            />
          </div>
          {feedback ? (
            <p className="col-span-2 text-sm text-critical">{feedback}</p>
          ) : null}
          <div className="col-span-2">
            <ModalActions>
              <Button onClick={() => setOpen(false)}>cancelar</Button>
              <Button variant="primary" type="submit" loading={create.isPending}>
                criar regra
              </Button>
            </ModalActions>
          </div>
        </form>
      </Modal>
    </div>
  );
}
