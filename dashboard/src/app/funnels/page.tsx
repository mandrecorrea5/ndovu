'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { Button, Field, Input, Select } from '@/components/form';
import { Modal, ModalActions } from '@/components/Modal';
import { EmptyState, ErrorState, LoadingState, StatTile } from '@/components/ui';
import { api, ApiError, type Funnel, type FunnelStep } from '@/lib/api';
import { canEdit, getSessionUser } from '@/lib/auth';
import { fmtNumber, RANGE_PRESETS, rangeToInterval } from '@/lib/time';

const PCT = (n: number) => `${(n * 100).toFixed(1)}%`;

type EditorState = {
  id?: string;
  app: string;
  name: string;
  windowSeconds: number;
  steps: FunnelStep[];
};

const EMPTY_STEP: FunnelStep = { name: '', match: { type: 'page_view' } };
const EMPTY_EDITOR: EditorState = {
  app: '',
  name: '',
  windowSeconds: 1800,
  steps: [
    { name: 'Passo 1', match: { type: 'page_view' } },
    { name: 'Passo 2', match: { type: 'action' } },
  ],
};

export default function FunnelsPage() {
  const qc = useQueryClient();
  const [range, setRange] = useState('7d');
  const [selectedApp, setSelectedApp] = useState('');
  const [selectedId, setSelectedId] = useState<string | undefined>();
  const [editorOpen, setEditorOpen] = useState(false);
  const [editor, setEditor] = useState<EditorState>(EMPTY_EDITOR);
  const [feedback, setFeedback] = useState('');

  const dateRange = useMemo(() => rangeToInterval(range), [range]);
  const editable = canEdit(getSessionUser());

  const { data: options } = useQuery({ queryKey: ['filters'], queryFn: api.filterOptions });
  const { data, isLoading, error } = useQuery({
    queryKey: ['funnels', selectedApp],
    queryFn: () => api.listFunnels(selectedApp || undefined),
  });

  const selected: Funnel | undefined = data?.funnels.find((f) => f.id === selectedId);
  const results = useQuery({
    queryKey: ['funnel-results', selectedId, range],
    enabled: !!selectedId,
    queryFn: () => api.funnelResults(selectedId!, { from: dateRange.from, to: dateRange.to }),
  });

  const save = useMutation({
    mutationFn: () => {
      if (editor.id) {
        return api.updateFunnel(editor.id, {
          name: editor.name,
          windowSeconds: editor.windowSeconds,
          steps: editor.steps,
        });
      }
      return api.createFunnel({
        app: editor.app,
        name: editor.name,
        windowSeconds: editor.windowSeconds,
        steps: editor.steps,
      });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['funnels'] });
      setEditorOpen(false);
    },
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro'),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteFunnel(id),
    onSuccess: (_, id) => {
      qc.invalidateQueries({ queryKey: ['funnels'] });
      if (id === selectedId) setSelectedId(undefined);
    },
  });

  const openNew = () => {
    setEditor({ ...EMPTY_EDITOR, app: selectedApp });
    setFeedback('');
    setEditorOpen(true);
  };

  const openEdit = (f: Funnel) => {
    const parsedSteps: FunnelStep[] =
      typeof f.steps === 'string' ? JSON.parse(f.steps as unknown as string) : f.steps;
    setEditor({
      id: f.id,
      app: f.app,
      name: f.name,
      windowSeconds: f.windowSeconds,
      steps: parsedSteps,
    });
    setFeedback('');
    setEditorOpen(true);
  };

  const updateStep = (i: number, patch: Partial<FunnelStep>) => {
    setEditor((s) => {
      const steps = s.steps.slice();
      steps[i] = { ...steps[i], ...patch, match: { ...steps[i].match, ...(patch.match ?? {}) } };
      return { ...s, steps };
    });
  };

  const funnels = data?.funnels ?? [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">Funis de conversão</h1>
          <p className="text-sm text-ink-2">
            Sequência de passos com contagem por step. Drop-off mostra onde o usuário abandonou.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Select value={selectedApp} onChange={(e) => setSelectedApp(e.target.value)} className="w-40">
            <option value="">todos os apps</option>
            {options?.apps.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </Select>
          <Select value={range} onChange={(e) => setRange(e.target.value)} className="w-44">
            {RANGE_PRESETS.map((p) => (
              <option key={p.key} value={p.key}>
                {p.label}
              </option>
            ))}
          </Select>
          {editable ? (
            <Button variant="primary" icon="+" onClick={openNew}>
              Novo funil
            </Button>
          ) : null}
        </div>
      </div>

      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}
      {data && funnels.length === 0 ? (
        <EmptyState
          message={
            editable
              ? 'Nenhum funil ainda. Comece pelo botão “Novo funil”.'
              : 'Nenhum funil cadastrado. Peça a um editor ou admin para criar.'
          }
        />
      ) : null}

      <div className="grid gap-4 lg:grid-cols-[280px_1fr]">
        {funnels.length > 0 ? (
          <aside className="card divide-y divide-hairline">
            {funnels.map((f) => (
              <button
                key={f.id}
                type="button"
                onClick={() => setSelectedId(f.id)}
                className={`block w-full px-4 py-3 text-left text-sm hover:bg-plane ${
                  f.id === selectedId ? 'bg-accent/10 text-accent' : 'text-ink'
                }`}
              >
                <div className="font-medium">{f.name}</div>
                <div className="text-xs text-muted">
                  {f.app} · janela {f.windowSeconds}s
                </div>
              </button>
            ))}
          </aside>
        ) : null}

        {selected ? (
          <section className="space-y-3">
            <header className="card flex items-center justify-between px-4 py-3">
              <div>
                <h2 className="text-base font-semibold">{selected.name}</h2>
                <p className="text-xs text-muted">
                  {selected.app} · janela de {selected.windowSeconds}s
                </p>
              </div>
              {editable ? (
                <div className="flex gap-1">
                  <Button size="sm" onClick={() => openEdit(selected)}>
                    editar
                  </Button>
                  <Button
                    size="sm"
                    variant="danger"
                    loading={remove.isPending}
                    onClick={() => {
                      if (confirm(`Remover funil "${selected.name}"?`)) remove.mutate(selected.id);
                    }}
                  >
                    remover
                  </Button>
                </div>
              ) : null}
            </header>

            {results.isLoading ? <LoadingState /> : null}
            {results.error ? <ErrorState message={(results.error as Error).message} /> : null}

            {results.data ? (
              <>
                <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
                  <StatTile label="Sessões no passo 1" value={results.data.totalSessions} />
                  <StatTile
                    label="Chegaram no último"
                    value={results.data.steps.at(-1)?.sessions ?? 0}
                  />
                  <StatTile
                    label="Conversão total"
                    value={PCT(results.data.steps.at(-1)?.overallRate ?? 0)}
                  />
                </div>

                <div className="card space-y-2 px-4 py-3">
                  <h3 className="text-sm font-semibold">Drop-off por passo</h3>
                  {results.data.steps.map((step, i) => (
                    <div key={i} className="space-y-1">
                      <div className="flex items-center justify-between text-xs">
                        <span className="font-medium text-ink">
                          {i + 1}. {step.name}
                        </span>
                        <span className="tabular text-ink-2">
                          {fmtNumber(step.sessions)} sessões · {PCT(step.overallRate)}
                          {i > 0 ? (
                            <span
                              className={`ml-2 ${
                                step.dropoffFromPrev > 0 ? 'text-critical' : 'text-good'
                              }`}
                            >
                              {step.dropoffFromPrev > 0
                                ? `↓ perdeu ${fmtNumber(step.dropoffFromPrev)}`
                                : '(sem drop)'}
                            </span>
                          ) : null}
                        </span>
                      </div>
                      <div className="h-2 overflow-hidden rounded bg-plane">
                        <div
                          className="h-full bg-accent transition-all"
                          style={{ width: `${(step.overallRate * 100).toFixed(1)}%` }}
                        />
                      </div>
                    </div>
                  ))}
                </div>
              </>
            ) : null}
          </section>
        ) : funnels.length > 0 ? (
          <EmptyState message="Selecione um funil à esquerda para ver o drop-off." />
        ) : null}
      </div>

      <Modal
        open={editorOpen}
        onClose={() => setEditorOpen(false)}
        title={editor.id ? `Editar funil: ${editor.name}` : 'Novo funil'}
        description="Adicione 2 a 8 passos. Cada passo é um matcher AND sobre trace_events."
        size="lg"
      >
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <div className="grid grid-cols-3 gap-3">
            <Field label="App" required>
              <Select
                required
                disabled={!!editor.id}
                value={editor.app}
                onChange={(e) => setEditor({ ...editor, app: e.target.value })}
              >
                <option value="">— selecione —</option>
                {options?.apps.map((a) => (
                  <option key={a} value={a}>
                    {a}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Nome" required>
              <Input
                required
                placeholder="ex.: login-until-checkout"
                value={editor.name}
                onChange={(e) => setEditor({ ...editor, name: e.target.value })}
              />
            </Field>
            <Field label="Janela (segundos)" required hint="tempo entre passo 1 e o último">
              <Input
                type="number"
                min={60}
                required
                value={editor.windowSeconds}
                onChange={(e) => setEditor({ ...editor, windowSeconds: Number(e.target.value) })}
              />
            </Field>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <p className="text-xs font-semibold uppercase tracking-wide text-muted">Passos</p>
              <Button
                size="sm"
                onClick={() =>
                  setEditor((s) => ({
                    ...s,
                    steps: [...s.steps, { ...EMPTY_STEP, name: `Passo ${s.steps.length + 1}` }],
                  }))
                }
                disabled={editor.steps.length >= 8}
              >
                + passo
              </Button>
            </div>
            {editor.steps.map((step, i) => (
              <div key={i} className="rounded-md border border-hairline bg-plane/40 p-3">
                <div className="mb-2 flex items-center justify-between">
                  <span className="text-xs font-semibold text-ink-2">Passo {i + 1}</span>
                  {editor.steps.length > 2 ? (
                    <button
                      type="button"
                      onClick={() =>
                        setEditor((s) => ({
                          ...s,
                          steps: s.steps.filter((_, k) => k !== i),
                        }))
                      }
                      className="text-xs text-muted hover:text-critical"
                    >
                      remover
                    </button>
                  ) : null}
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <Field label="Rótulo" required>
                    <Input
                      required
                      value={step.name}
                      onChange={(e) => updateStep(i, { name: e.target.value })}
                    />
                  </Field>
                  <Field label="Tipo de evento" required>
                    <Select
                      value={step.match.type}
                      onChange={(e) =>
                        updateStep(i, {
                          match: { type: e.target.value as FunnelStep['match']['type'] },
                        })
                      }
                    >
                      <option value="page_view">page_view</option>
                      <option value="action">action</option>
                      <option value="http_request">http_request</option>
                      <option value="error">error</option>
                      <option value="custom">custom</option>
                    </Select>
                  </Field>
                  <Field label="name (opcional)">
                    <Input
                      placeholder="ex.: clicou_login"
                      value={step.match.name ?? ''}
                      onChange={(e) => updateStep(i, { match: { ...step.match, name: e.target.value } })}
                    />
                  </Field>
                  <Field label="screen (opcional)">
                    <Input
                      placeholder="ex.: /checkout"
                      value={step.match.screen ?? ''}
                      onChange={(e) =>
                        updateStep(i, { match: { ...step.match, screen: e.target.value } })
                      }
                    />
                  </Field>
                  <Field label="feature (opcional)">
                    <Input
                      value={step.match.feature ?? ''}
                      onChange={(e) =>
                        updateStep(i, { match: { ...step.match, feature: e.target.value } })
                      }
                    />
                  </Field>
                  <Field label="httpUrl contains (opcional)">
                    <Input
                      placeholder="/api/orders"
                      value={step.match.httpUrl ?? ''}
                      onChange={(e) =>
                        updateStep(i, { match: { ...step.match, httpUrl: e.target.value } })
                      }
                    />
                  </Field>
                </div>
              </div>
            ))}
          </div>

          {feedback ? <p className="text-sm text-critical">{feedback}</p> : null}
          <ModalActions>
            <Button onClick={() => setEditorOpen(false)}>cancelar</Button>
            <Button variant="primary" type="submit" loading={save.isPending}>
              {editor.id ? 'salvar' : 'criar'}
            </Button>
          </ModalActions>
        </form>
      </Modal>
    </div>
  );
}
