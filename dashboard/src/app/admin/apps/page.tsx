'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import Link from 'next/link';
import { useState } from 'react';
import { Button, Field, Input, Select, Textarea } from '@/components/form';
import { Modal, ModalActions } from '@/components/Modal';
import { ErrorState, LoadingState } from '@/components/ui';
import { api, ApiError, type App, type CreatedApp } from '@/lib/api';
import { TECHNOLOGIES } from '@/lib/constants';
import { fmtDateTime } from '@/lib/time';

type FormState = {
  name: string;
  technology: string;
  allowedOrigins: string;
  allowOtlp: boolean;
  companyId: string;
  company: string;
  responsible: string;
};
const EMPTY: FormState = {
  name: '',
  technology: '',
  allowedOrigins: '',
  allowOtlp: false,
  companyId: '',
  company: '',
  responsible: '',
};

export default function AdminAppsPage() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({ queryKey: ['admin-apps'], queryFn: api.listApps });
  const { data: companiesData } = useQuery({
    queryKey: ['admin-companies'],
    queryFn: api.listCompanies,
  });

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<App | null>(null);
  const [form, setForm] = useState<FormState>(EMPTY);
  const [created, setCreated] = useState<CreatedApp | null>(null);
  const [feedback, setFeedback] = useState('');

  const invalidate = () => qc.invalidateQueries({ queryKey: ['admin-apps'] });

  const openNew = () => {
    setEditing(null);
    setForm(EMPTY);
    setFeedback('');
    setOpen(true);
  };
  const openEdit = (a: App) => {
    setEditing(a);
    setForm({
      name: a.name,
      technology: a.technology ?? '',
      allowedOrigins: (a.allowedOrigins ?? []).join('\n'),
      allowOtlp: a.allowOtlp ?? false,
      companyId: a.companyId ?? '',
      company: a.company ?? '',
      responsible: a.responsible ?? '',
    });
    setFeedback('');
    setOpen(true);
  };

  const save = useMutation({
    mutationFn: async () => {
      const payload = {
        name: form.name,
        technology: form.technology,
        allowedOrigins: form.allowedOrigins
          .split('\n')
          .map((origin) => origin.trim())
          .filter(Boolean),
        allowOtlp: form.allowOtlp,
        companyId: form.companyId || undefined,
        company: form.companyId ? undefined : form.company,
        responsible: form.responsible,
      };
      if (editing) return api.updateApp(editing.id, payload);
      return api.createApp(payload);
    },
    onSuccess: (result) => {
      if (!editing) setCreated(result as CreatedApp);
      invalidate();
      setOpen(false);
    },
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro'),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteApp(id),
    onSuccess: () => invalidate(),
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro'),
  });

  const confirmDelete = (a: App) => {
    if (confirm(`Excluir o app "${a.name}"? Todas as chaves dele serão revogadas.`)) {
      remove.mutate(a.id);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Apps</h1>
          <p className="text-sm text-ink-2">
            Cada frontend emissor. As chaves ficam disponíveis com histórico na administração de
            chaves de API.
          </p>
          <Link href="/admin/keys" className="text-sm text-accent hover:underline">
            Gerenciar chaves de API
          </Link>
        </div>
        <Button variant="primary" onClick={openNew} icon="+">
          Novo app
        </Button>
      </div>

      {created ? (
        <div className="card border-good/40 px-4 py-3">
          <p className="text-sm font-medium">
            App <span className="mono">{created.name}</span> cadastrado. A chave está cifrada no
            histórico e pode ser consultada em Chaves de API:
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
          <Link href="/admin/keys" className="mt-2 inline-block text-sm text-accent hover:underline">
            Abrir histórico de chaves
          </Link>
        </div>
      ) : null}

      {feedback && !open ? <p className="text-sm text-critical">{feedback}</p> : null}
      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}

      {data ? (
        <div className="card overflow-x-auto">
          <table className="w-full min-w-[900px] text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wide text-muted">
                <th className="px-3 py-2 font-medium">Nome</th>
                <th className="px-3 py-2 font-medium">Tecnologia</th>
                <th className="px-3 py-2 font-medium">Origens</th>
                <th className="px-3 py-2 font-medium">Empresa</th>
                <th className="px-3 py-2 font-medium">Responsável</th>
                <th className="px-3 py-2 font-medium">Criado em</th>
                <th className="px-3 py-2 font-medium">Ações</th>
              </tr>
            </thead>
            <tbody>
              {data.apps.map((a) => (
                <tr key={a.id} className="border-t border-hairline">
                  <td className="px-3 py-2 font-medium">{a.name}</td>
                  <td className="px-3 py-2 text-xs text-ink-2">{a.technology || '—'}</td>
                  <td className="max-w-56 px-3 py-2 text-xs text-ink-2">
                    {a.allowedOrigins?.length ? a.allowedOrigins.join(', ') : '—'}
                    {a.allowOtlp ? (
                      <span className="ml-1 rounded bg-accent/10 px-1 text-[10px] font-medium uppercase text-accent">
                        otlp
                      </span>
                    ) : null}
                  </td>
                  <td className="px-3 py-2 text-xs text-ink-2">{a.company || '—'}</td>
                  <td className="px-3 py-2 text-xs text-ink-2">{a.responsible || '—'}</td>
                  <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                    {fmtDateTime(a.createdAt)}
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex gap-1">
                      <Button size="sm" onClick={() => openEdit(a)}>
                        editar
                      </Button>
                      <Button
                        size="sm"
                        variant="danger"
                        loading={remove.isPending}
                        onClick={() => confirmDelete(a)}
                      >
                        excluir
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
              {data.apps.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-3 py-8 text-center text-muted">
                    Nenhum app cadastrado — comece pelo botão “Novo app”.
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
        title={editing ? `Editar app: ${editing.name}` : 'Novo app'}
        description="Cada app tem um nome único, tecnologia, empresa e as origens web autorizadas a enviar dados."
      >
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field label="Nome do app" required htmlFor="app-name">
            <Input
              id="app-name"
              required
              placeholder="ex.: portal-cliente"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </Field>
          <Field label="Tecnologia" htmlFor="app-tech">
            <Select
              id="app-tech"
              value={form.technology}
              onChange={(e) => setForm({ ...form, technology: e.target.value })}
            >
              <option value="">— selecione —</option>
              {TECHNOLOGIES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Origens permitidas do site" htmlFor="app-origins">
            <Textarea
              id="app-origins"
              placeholder={'https://www.exemplo.com\nhttps://exemplo.com'}
              value={form.allowedOrigins}
              onChange={(e) => setForm({ ...form, allowedOrigins: e.target.value })}
              rows={3}
            />
            <p className="mt-1 text-xs text-ink-2">
              Uma origem por linha, incluindo https:// e sem caminho. Ex.: https://www.exemplo.com.
              Deixe vazio para usar a lista global antiga do CORS.
            </p>
          </Field>
          <Field label="App de servidor (OpenTelemetry)" htmlFor="app-otlp">
            <label className="flex items-start gap-2 text-sm">
              <input
                id="app-otlp"
                type="checkbox"
                className="mt-1"
                checked={form.allowOtlp}
                onChange={(e) => setForm({ ...form, allowOtlp: e.target.checked })}
              />
              <span>
                A chave deste app aceita spans OTLP
                <span className="mt-1 block text-xs text-ink-2">
                  Ligue só em app de backend. A chave de um app de browser fica exposta no bundle do
                  site, e aceitar OTLP com ela deixaria qualquer um gravar spans em nome do app.
                </span>
              </span>
            </label>
          </Field>
          <Field label="Empresa" htmlFor="app-company">
            <Select
              id="app-company"
              value={form.companyId}
              onChange={(e) => setForm({ ...form, companyId: e.target.value })}
            >
              <option value="">— sem empresa cadastrada —</option>
              {companiesData?.companies
                .filter((c) => c.active)
                .map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
            </Select>
            {!form.companyId ? (
              <Input
                className="mt-2"
                placeholder="Ou digite o nome (fallback)"
                value={form.company}
                onChange={(e) => setForm({ ...form, company: e.target.value })}
              />
            ) : null}
          </Field>
          <Field label="Responsável" htmlFor="app-resp">
            <Input
              id="app-resp"
              placeholder="ex.: ana@acme.com"
              value={form.responsible}
              onChange={(e) => setForm({ ...form, responsible: e.target.value })}
            />
          </Field>
          {feedback ? <p className="text-sm text-critical">{feedback}</p> : null}
          <ModalActions>
            <Button onClick={() => setOpen(false)}>cancelar</Button>
            <Button variant="primary" type="submit" loading={save.isPending}>
              {editing ? 'salvar' : 'cadastrar'}
            </Button>
          </ModalActions>
        </form>
      </Modal>
    </div>
  );
}
