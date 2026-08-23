'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Button, Checkbox, Field, Input, Select } from '@/components/form';
import { Modal, ModalActions } from '@/components/Modal';
import { ErrorState, LoadingState } from '@/components/ui';
import { api, ApiError, type AnomalyDetection, type AnomalyRule } from '@/lib/api';
import { fmtDateTime } from '@/lib/time';

const METRICS = [
  { v: 'error_count', label: 'Contagem de erros' },
  { v: 'event_count', label: 'Contagem de eventos' },
  { v: 'error_rate', label: 'Taxa de erro (erros/eventos)' },
];
const DIRECTIONS = [
  { v: 'above', label: 'Spike (acima da média)' },
  { v: 'below', label: 'Silêncio (abaixo da média)' },
  { v: 'both', label: 'Ambos' },
];

type FormState = {
  name: string;
  app: string;
  metric: 'error_count' | 'event_count' | 'error_rate';
  windowMinutes: number;
  baselineWeeks: number;
  sensitivity: number;
  direction: 'above' | 'below' | 'both';
  silenceSeconds: number;
  channel: 'slack' | 'webhook';
  targetUrl: string;
  active: boolean;
};

const EMPTY: FormState = {
  name: '',
  app: '',
  metric: 'error_count',
  windowMinutes: 15,
  baselineWeeks: 4,
  sensitivity: 3,
  direction: 'above',
  silenceSeconds: 1800,
  channel: 'slack',
  targetUrl: '',
  active: true,
};

export default function AnomaliesPage() {
  const qc = useQueryClient();
  const rulesQ = useQuery({ queryKey: ['anomaly-rules'], queryFn: api.listAnomalyRules });
  const detectionsQ = useQuery({
    queryKey: ['anomaly-detections'],
    queryFn: () => api.listAnomalyDetections({ limit: '20' }),
    refetchInterval: 60_000,
  });
  const appsQ = useQuery({ queryKey: ['admin-apps'], queryFn: api.listApps });

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<AnomalyRule | null>(null);
  const [form, setForm] = useState<FormState>(EMPTY);
  const [feedback, setFeedback] = useState('');

  const openNew = () => {
    setEditing(null);
    setForm(EMPTY);
    setFeedback('');
    setOpen(true);
  };

  const openEdit = (r: AnomalyRule) => {
    setEditing(r);
    setForm({
      name: r.name,
      app: r.app,
      metric: r.metric,
      windowMinutes: r.windowMinutes,
      baselineWeeks: r.baselineWeeks,
      sensitivity: r.sensitivity,
      direction: r.direction,
      silenceSeconds: r.silenceSeconds,
      channel: r.channel,
      targetUrl: r.targetUrl,
      active: r.active,
    });
    setFeedback('');
    setOpen(true);
  };

  const save = useMutation({
    mutationFn: () =>
      editing ? api.updateAnomalyRule(editing.id, form) : api.createAnomalyRule(form),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['anomaly-rules'] });
      setOpen(false);
    },
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro'),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteAnomalyRule(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['anomaly-rules'] }),
  });

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Detecção de anomalia</h1>
          <p className="text-sm text-ink-2">
            Compara a janela atual com o comportamento histórico (mesma hora + dia da semana nas
            últimas N semanas). Se o desvio ultrapassa <code className="mono">sensitivity</code>{' '}
            (em stddevs), o Ndovu avisa por Slack ou webhook — sem precisar de threshold fixo.
          </p>
        </div>
        <Button variant="primary" onClick={openNew} icon="+">
          Nova regra
        </Button>
      </div>

      {/* --- Regras --- */}
      <section>
        <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted">
          Regras
        </h2>
        {rulesQ.error ? <ErrorState message={(rulesQ.error as Error).message} /> : null}
        {rulesQ.isLoading ? <LoadingState /> : null}
        {rulesQ.data ? (
          <div className="card overflow-x-auto">
            <table className="w-full min-w-[900px] text-sm">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wide text-muted">
                  <th className="px-3 py-2 font-medium">Nome</th>
                  <th className="px-3 py-2 font-medium">Escopo</th>
                  <th className="px-3 py-2 font-medium">Métrica</th>
                  <th className="px-3 py-2 text-right font-medium">Janela</th>
                  <th className="px-3 py-2 text-right font-medium">Baseline</th>
                  <th className="px-3 py-2 text-right font-medium">Sensitivity</th>
                  <th className="px-3 py-2 font-medium">Direção</th>
                  <th className="px-3 py-2 font-medium">Canal</th>
                  <th className="px-3 py-2 font-medium">Status</th>
                  <th className="px-3 py-2 font-medium">Ações</th>
                </tr>
              </thead>
              <tbody>
                {rulesQ.data.rules.map((r) => (
                  <tr key={r.id} className="border-t border-hairline">
                    <td className="px-3 py-2 font-medium">{r.name}</td>
                    <td className="mono px-3 py-2 text-xs text-ink-2">{r.app || '*'}</td>
                    <td className="px-3 py-2 text-xs">{r.metric}</td>
                    <td className="tabular px-3 py-2 text-right text-xs">{r.windowMinutes}min</td>
                    <td className="tabular px-3 py-2 text-right text-xs">{r.baselineWeeks}w</td>
                    <td className="tabular px-3 py-2 text-right font-medium">
                      {r.sensitivity.toFixed(1)}σ
                    </td>
                    <td className="px-3 py-2 text-xs">
                      {r.direction === 'above' && '↑ spike'}
                      {r.direction === 'below' && '↓ silêncio'}
                      {r.direction === 'both' && '↕ ambos'}
                    </td>
                    <td className="px-3 py-2 text-xs">
                      <span className="rounded-md border border-hairline px-1.5 py-0.5">
                        {r.channel}
                      </span>
                    </td>
                    <td className="px-3 py-2 text-xs">
                      <span className={r.active ? 'text-good' : 'text-muted'}>
                        {r.active ? '● ativa' : '○ inativa'}
                      </span>
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
                            if (confirm(`Remover regra "${r.name}"?`)) remove.mutate(r.id);
                          }}
                        >
                          remover
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
                {rulesQ.data.rules.length === 0 ? (
                  <tr>
                    <td colSpan={10} className="px-3 py-8 text-center text-muted">
                      Nenhuma regra ainda. Comece por &quot;erro spike no app X, 3σ&quot;.
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>

      {/* --- Detecções recentes --- */}
      <section>
        <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted">
          Detecções recentes
          {detectionsQ.data ? (
            <span className="ml-2 text-xs font-normal text-muted">
              ({detectionsQ.data.total} no total)
            </span>
          ) : null}
        </h2>
        {detectionsQ.data && detectionsQ.data.detections.length === 0 ? (
          <p className="text-sm text-muted">
            Nenhuma anomalia detectada ainda. O avaliador roda a cada 5 minutos no writer.
          </p>
        ) : null}
        {detectionsQ.data && detectionsQ.data.detections.length > 0 ? (
          <div className="card overflow-x-auto">
            <table className="w-full min-w-[900px] text-sm">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wide text-muted">
                  <th className="px-3 py-2 font-medium">Quando</th>
                  <th className="px-3 py-2 font-medium">Regra</th>
                  <th className="px-3 py-2 font-medium">Direção</th>
                  <th className="px-3 py-2 text-right font-medium">Atual</th>
                  <th className="px-3 py-2 text-right font-medium">Baseline</th>
                  <th className="px-3 py-2 text-right font-medium">Z-score</th>
                  <th className="px-3 py-2 font-medium">Notify</th>
                </tr>
              </thead>
              <tbody>
                {detectionsQ.data.detections.map((d: AnomalyDetection) => (
                  <tr key={d.id} className="border-t border-hairline">
                    <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                      {fmtDateTime(d.detectedAt)}
                    </td>
                    <td className="px-3 py-2 font-medium">{d.ruleName || d.ruleId}</td>
                    <td className="px-3 py-2 text-xs">
                      {d.direction === 'above' ? (
                        <span className="text-critical">↑ spike</span>
                      ) : (
                        <span className="text-warn">↓ silêncio</span>
                      )}
                    </td>
                    <td className="tabular px-3 py-2 text-right font-medium">
                      {d.currentValue.toFixed(2)}
                    </td>
                    <td className="tabular px-3 py-2 text-right text-ink-2">
                      {d.baselineAvg.toFixed(2)}
                      <span className="text-muted"> ± {d.baselineStddev.toFixed(2)}</span>
                    </td>
                    <td
                      className={`tabular px-3 py-2 text-right font-medium ${
                        Math.abs(d.zScore) > 5 ? 'text-critical' : 'text-warn'
                      }`}
                    >
                      {d.zScore >= 0 ? '+' : ''}
                      {d.zScore.toFixed(2)}σ
                    </td>
                    <td className="px-3 py-2 text-xs">
                      {d.notifyOk ? (
                        <span className="text-good">✓ entregue</span>
                      ) : (
                        <span className="text-critical" title={d.notifyDetail}>
                          ✕ falhou
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>

      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title={editing ? `Editar regra: ${editing.name}` : 'Nova regra de anomalia'}
        description="Compara janela atual com mesma hora+weekday nas semanas passadas. Dispara se |z-score| > sensitivity."
        size="lg"
      >
        <form
          className="grid grid-cols-2 gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field label="Nome" required>
            <Input
              required
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="ex.: spike-erro-portal"
            />
          </Field>
          <Field label="App (vazio = todos)">
            <Select value={form.app} onChange={(e) => setForm({ ...form, app: e.target.value })}>
              <option value="">— todos —</option>
              {appsQ.data?.apps.map((a) => (
                <option key={a.id} value={a.name}>
                  {a.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Métrica" required>
            <Select
              value={form.metric}
              onChange={(e) => setForm({ ...form, metric: e.target.value as FormState['metric'] })}
            >
              {METRICS.map((m) => (
                <option key={m.v} value={m.v}>
                  {m.label}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Direção" required>
            <Select
              value={form.direction}
              onChange={(e) =>
                setForm({ ...form, direction: e.target.value as FormState['direction'] })
              }
            >
              {DIRECTIONS.map((d) => (
                <option key={d.v} value={d.v}>
                  {d.label}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Janela atual (minutos)" required hint="1–240">
            <Input
              type="number"
              min={1}
              max={240}
              required
              value={form.windowMinutes}
              onChange={(e) => setForm({ ...form, windowMinutes: Number(e.target.value) })}
            />
          </Field>
          <Field label="Baseline (semanas)" required hint="1–12 semanas atrás para calcular média">
            <Input
              type="number"
              min={1}
              max={12}
              required
              value={form.baselineWeeks}
              onChange={(e) => setForm({ ...form, baselineWeeks: Number(e.target.value) })}
            />
          </Field>
          <Field
            label={`Sensitivity (${form.sensitivity.toFixed(1)}σ)`}
            hint="2σ = mais sensível (mais alertas); 4σ = mais conservador"
          >
            <Input
              type="range"
              min={1}
              max={6}
              step={0.5}
              value={form.sensitivity}
              onChange={(e) => setForm({ ...form, sensitivity: Number(e.target.value) })}
            />
          </Field>
          <Field label="Silêncio (segundos)" hint="Impede re-disparo dentro do intervalo">
            <Input
              type="number"
              min={0}
              value={form.silenceSeconds}
              onChange={(e) => setForm({ ...form, silenceSeconds: Number(e.target.value) })}
            />
          </Field>
          <Field label="Canal">
            <Select
              value={form.channel}
              onChange={(e) => setForm({ ...form, channel: e.target.value as FormState['channel'] })}
            >
              <option value="slack">Slack (webhook)</option>
              <option value="webhook">Webhook genérico</option>
            </Select>
          </Field>
          <div />
          <div className="col-span-2">
            <Field label="URL de destino" required>
              <Input
                required
                value={form.targetUrl}
                onChange={(e) => setForm({ ...form, targetUrl: e.target.value })}
                placeholder="https://hooks.slack.com/services/…"
              />
            </Field>
          </div>
          <div className="col-span-2">
            <Checkbox
              checked={form.active}
              onChange={(e) => setForm({ ...form, active: e.target.checked })}
              label="Ativa (avaliada a cada 5 minutos)"
            />
          </div>
          {feedback ? <p className="col-span-2 text-sm text-critical">{feedback}</p> : null}
          <div className="col-span-2">
            <ModalActions>
              <Button onClick={() => setOpen(false)}>cancelar</Button>
              <Button variant="primary" type="submit" loading={save.isPending}>
                {editing ? 'salvar' : 'criar'}
              </Button>
            </ModalActions>
          </div>
        </form>
      </Modal>
    </div>
  );
}
