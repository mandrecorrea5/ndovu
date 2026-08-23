'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Button, Checkbox, Field, Input } from '@/components/form';
import { Modal, ModalActions } from '@/components/Modal';
import { ErrorState, LoadingState } from '@/components/ui';
import { api, ApiError, type Company } from '@/lib/api';
import { fmtDateTime } from '@/lib/time';

type FormState = { name: string; document: string; active: boolean };
const EMPTY: FormState = { name: '', document: '', active: true };

export default function AdminCompaniesPage() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ['admin-companies'],
    queryFn: api.listCompanies,
  });

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Company | null>(null);
  const [form, setForm] = useState<FormState>(EMPTY);
  const [feedback, setFeedback] = useState('');

  const openNew = () => {
    setEditing(null);
    setForm(EMPTY);
    setFeedback('');
    setOpen(true);
  };
  const openEdit = (c: Company) => {
    setEditing(c);
    setForm({ name: c.name, document: c.document ?? '', active: c.active });
    setFeedback('');
    setOpen(true);
  };

  const save = useMutation({
    mutationFn: () =>
      editing ? api.updateCompany(editing.id, form) : api.createCompany(form),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-companies'] });
      setOpen(false);
    },
    onError: (err) => setFeedback(err instanceof ApiError ? err.message : 'Erro ao salvar'),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteCompany(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin-companies'] }),
    onError: (err) => setFeedback(err instanceof ApiError ? err.message : 'Erro ao remover'),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Empresas</h1>
          <p className="text-sm text-ink-2">
            Empresas cadastradas — todo usuário pertence a uma; apps podem ser atrelados.
          </p>
        </div>
        <Button variant="primary" onClick={openNew} icon="+">
          Nova empresa
        </Button>
      </div>

      {feedback && !open ? <p className="text-sm text-critical">{feedback}</p> : null}
      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}

      {data ? (
        <div className="card overflow-x-auto">
          <table className="w-full min-w-[640px] text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wide text-muted">
                <th className="px-3 py-2 font-medium">Nome</th>
                <th className="px-3 py-2 font-medium">Documento</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Criada</th>
                <th className="px-3 py-2 font-medium">Ações</th>
              </tr>
            </thead>
            <tbody>
              {data.companies.map((c) => (
                <tr key={c.id} className="border-t border-hairline">
                  <td className="px-3 py-2 font-medium">{c.name}</td>
                  <td className="mono px-3 py-2 text-xs text-ink-2">{c.document || '—'}</td>
                  <td className="px-3 py-2">
                    {c.active ? (
                      <span className="text-good">● ativa</span>
                    ) : (
                      <span className="text-muted">○ inativa</span>
                    )}
                  </td>
                  <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                    {fmtDateTime(c.createdAt)}
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex gap-1">
                      <Button size="sm" onClick={() => openEdit(c)}>
                        editar
                      </Button>
                      <Button
                        size="sm"
                        variant="danger"
                        loading={remove.isPending}
                        onClick={() => {
                          if (confirm(`Remover empresa "${c.name}"?`)) remove.mutate(c.id);
                        }}
                      >
                        remover
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
              {data.companies.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-3 py-8 text-center text-muted">
                    Nenhuma empresa cadastrada ainda.
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
        title={editing ? `Editar empresa: ${editing.name}` : 'Nova empresa'}
        description="Empresa cliente. Usuários precisam estar vinculados a uma empresa."
      >
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field label="Nome" required htmlFor="company-name">
            <Input
              id="company-name"
              required
              placeholder="ex.: Acme S.A."
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </Field>
          <Field label="Documento (CNPJ, opcional)" htmlFor="company-doc">
            <Input
              id="company-doc"
              placeholder="00.000.000/0000-00"
              value={form.document}
              onChange={(e) => setForm({ ...form, document: e.target.value })}
            />
          </Field>
          <Checkbox
            checked={form.active}
            onChange={(e) => setForm({ ...form, active: e.target.checked })}
            label="ativa"
          />
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
