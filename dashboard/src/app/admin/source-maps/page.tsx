'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Button, Field, Input, Select } from '@/components/form';
import { Modal, ModalActions } from '@/components/Modal';
import { ErrorState, LoadingState } from '@/components/ui';
import { api, ApiError, type SourceMap } from '@/lib/api';
import { fmtDateTime, fmtNumber } from '@/lib/time';

type FormState = {
  app: string;
  release: string;
  filename: string;
  content: string;
};
const EMPTY: FormState = { app: '', release: '', filename: '', content: '' };

export default function AdminSourceMapsPage() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ['admin-source-maps'],
    queryFn: () => api.listSourceMaps({}),
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

  const upload = useMutation({
    mutationFn: () => api.uploadSourceMap(form),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-source-maps'] });
      setOpen(false);
    },
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro'),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteSourceMap(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin-source-maps'] }),
    onError: (err) => setFeedback(err instanceof ApiError ? err.message : 'Erro ao remover'),
  });

  // Handler do input <file> — lê o .map como texto e coloca em form.content
  const onFile = async (file?: File | null) => {
    if (!file) return;
    const text = await file.text();
    setForm((f) => ({
      ...f,
      content: text,
      filename: f.filename || file.name.replace(/\.map$/, ''),
    }));
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Source maps</h1>
          <p className="text-sm text-ink-2">
            Suba os <code className="mono">.map</code> por release para desminificar stack traces
            na visualização de erros. Reenvio do mesmo arquivo sobrescreve.
          </p>
        </div>
        <Button variant="primary" onClick={openNew} icon="+">
          Novo upload
        </Button>
      </div>

      {feedback && !open ? <p className="text-sm text-critical">{feedback}</p> : null}
      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}

      {data ? (
        <div className="card overflow-x-auto">
          <table className="w-full min-w-[720px] text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wide text-muted">
                <th className="px-3 py-2 font-medium">App</th>
                <th className="px-3 py-2 font-medium">Release</th>
                <th className="px-3 py-2 font-medium">Arquivo</th>
                <th className="px-3 py-2 text-right font-medium">Tamanho</th>
                <th className="px-3 py-2 font-medium">Enviado em</th>
                <th className="px-3 py-2 font-medium">Ações</th>
              </tr>
            </thead>
            <tbody>
              {data.sourceMaps.map((sm: SourceMap) => (
                <tr key={sm.id} className="border-t border-hairline">
                  <td className="px-3 py-2 font-medium">{sm.app}</td>
                  <td className="mono px-3 py-2 text-xs text-accent">{sm.release}</td>
                  <td className="mono px-3 py-2 text-xs">{sm.filename}</td>
                  <td className="tabular px-3 py-2 text-right text-xs text-ink-2">
                    {fmtNumber(Math.round(sm.sizeBytes / 1024))} KB
                  </td>
                  <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                    {fmtDateTime(sm.uploadedAt)}
                  </td>
                  <td className="px-3 py-2">
                    <Button
                      size="sm"
                      variant="danger"
                      loading={remove.isPending}
                      onClick={() => {
                        if (
                          confirm(
                            `Remover source map ${sm.filename} (release ${sm.release})?`,
                          )
                        )
                          remove.mutate(sm.id);
                      }}
                    >
                      remover
                    </Button>
                  </td>
                </tr>
              ))}
              {data.sourceMaps.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-3 py-8 text-center text-muted">
                    Nenhum source map enviado ainda.
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
        title="Enviar source map"
        description="Escolha o app + release + arquivo. Reenvio do mesmo (app, release, filename) sobrescreve."
      >
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            upload.mutate();
          }}
        >
          <Field label="App" required htmlFor="sm-app">
            <Select
              id="sm-app"
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
          <Field label="Release" required htmlFor="sm-release">
            <Input
              id="sm-release"
              required
              placeholder="ex.: v1.2.3 ou hash do build"
              value={form.release}
              onChange={(e) => setForm({ ...form, release: e.target.value })}
            />
          </Field>
          <Field
            label="Filename"
            required
            htmlFor="sm-filename"
            hint="Nome do JS minificado (ex.: main.abc.js). Se você escolher um arquivo .map abaixo, preenche automaticamente."
          >
            <Input
              id="sm-filename"
              required
              placeholder="main.abc.js"
              value={form.filename}
              onChange={(e) => setForm({ ...form, filename: e.target.value })}
            />
          </Field>
          <Field
            label="Arquivo .map"
            hint="Ou cole o JSON direto abaixo se preferir automatizar via curl."
          >
            <Input type="file" accept=".map,application/json" onChange={(e) => onFile(e.target.files?.[0])} />
          </Field>
          <Field label="Conteúdo (JSON do source map)" required>
            <textarea
              className="block h-40 w-full rounded-md border border-hairline bg-surface px-3 py-2 font-mono text-xs text-ink focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30"
              required
              value={form.content}
              onChange={(e) => setForm({ ...form, content: e.target.value })}
              placeholder='{"version":3,"sources":[...],"names":[...],"mappings":"..."}'
            />
          </Field>
          {feedback ? <p className="text-sm text-critical">{feedback}</p> : null}
          <ModalActions>
            <Button onClick={() => setOpen(false)}>cancelar</Button>
            <Button variant="primary" type="submit" loading={upload.isPending}>
              enviar
            </Button>
          </ModalActions>
        </form>
      </Modal>
    </div>
  );
}
