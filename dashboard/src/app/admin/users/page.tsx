'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Button, Field, Input, Select } from '@/components/form';
import { Modal, ModalActions } from '@/components/Modal';
import { ErrorState, LoadingState } from '@/components/ui';
import { UserPermissionsPanel } from '@/components/UserPermissionsPanel';
import { api, ApiError, type AdminUser } from '@/lib/api';
import { getSessionUser } from '@/lib/auth';
import { fmtDateTime } from '@/lib/time';

type FormState = {
  email: string;
  name: string;
  password: string;
  role: string;
  companyId: string;
};
const EMPTY: FormState = { email: '', name: '', password: '', role: 'viewer', companyId: '' };

export default function AdminUsersPage() {
  const qc = useQueryClient();
  const me = getSessionUser();
  const { data, isLoading, error } = useQuery({ queryKey: ['admin-users'], queryFn: api.listUsers });
  const { data: companiesData } = useQuery({
    queryKey: ['admin-companies'],
    queryFn: api.listCompanies,
  });

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<AdminUser | null>(null);
  const [form, setForm] = useState<FormState>(EMPTY);
  const [feedback, setFeedback] = useState('');

  const invalidate = () => qc.invalidateQueries({ queryKey: ['admin-users'] });

  const openNew = () => {
    setEditing(null);
    setForm(EMPTY);
    setFeedback('');
    setOpen(true);
  };
  const openEdit = (u: AdminUser) => {
    setEditing(u);
    setForm({ email: u.email, name: u.name, password: '', role: u.role, companyId: u.companyId });
    setFeedback('');
    setOpen(true);
  };

  const save = useMutation({
    mutationFn: async () => {
      if (editing) {
        const patch: Parameters<typeof api.updateUser>[1] = {
          name: form.name,
          role: form.role,
          companyId: form.companyId,
        };
        if (form.password) patch.password = form.password;
        return api.updateUser(editing.id, patch);
      }
      return api.createUser({
        email: form.email,
        name: form.name,
        password: form.password,
        role: form.role,
        companyId: form.companyId,
      });
    },
    onSuccess: () => {
      invalidate();
      setOpen(false);
    },
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro'),
  });

  const update = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Parameters<typeof api.updateUser>[1] }) =>
      api.updateUser(id, patch),
    onSuccess: () => invalidate(),
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro'),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Usuários</h1>
          <p className="text-sm text-ink-2">
            Admins criam contas, alteram papéis e desativam acessos. Todo usuário pertence a uma
            empresa. Sempre deve existir ao menos um admin ativo.
          </p>
        </div>
        <Button variant="primary" onClick={openNew} icon="+">
          Novo usuário
        </Button>
      </div>

      {feedback && !open ? <p className="text-sm text-critical">{feedback}</p> : null}
      {error ? <ErrorState message={(error as Error).message} /> : null}
      {isLoading ? <LoadingState /> : null}

      {data ? (
        <div className="card overflow-x-auto">
          <table className="w-full min-w-[860px] text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wide text-muted">
                <th className="px-3 py-2 font-medium">Nome</th>
                <th className="px-3 py-2 font-medium">E-mail</th>
                <th className="px-3 py-2 font-medium">Empresa</th>
                <th className="px-3 py-2 font-medium">Papel</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Criado em</th>
                <th className="px-3 py-2 font-medium">Ações</th>
              </tr>
            </thead>
            <tbody>
              {data.users.map((u) => (
                <tr key={u.id} className="border-t border-hairline">
                  <td className="px-3 py-2">
                    {u.name}
                    {me?.id === u.id ? <span className="ml-1 text-xs text-muted">(você)</span> : null}
                  </td>
                  <td className="mono px-3 py-2 text-xs">{u.email}</td>
                  <td className="px-3 py-2 text-xs text-ink-2">{u.company ?? '—'}</td>
                  <td className="px-3 py-2 text-xs uppercase text-ink-2">{u.role}</td>
                  <td className="px-3 py-2">
                    <span className={u.active ? 'text-good' : 'text-muted'}>
                      {u.active ? '● ativo' : '○ inativo'}
                    </span>
                  </td>
                  <td className="tabular whitespace-nowrap px-3 py-2 text-xs text-ink-2">
                    {fmtDateTime(u.createdAt)}
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex gap-1">
                      <Button size="sm" onClick={() => openEdit(u)}>
                        editar
                      </Button>
                      <Button
                        size="sm"
                        onClick={() => update.mutate({ id: u.id, patch: { active: !u.active } })}
                      >
                        {u.active ? 'desativar' : 'reativar'}
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title={editing ? `Editar usuário: ${editing.email}` : 'Novo usuário'}
        description="Usuário do backoffice — precisa estar atrelado a uma empresa."
      >
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field
            label="E-mail"
            required
            htmlFor="user-email"
            hint={editing ? 'e-mail não pode ser alterado' : undefined}
          >
            <Input
              id="user-email"
              type="email"
              required
              disabled={!!editing}
              value={form.email}
              onChange={(e) => setForm({ ...form, email: e.target.value })}
            />
          </Field>
          <Field label="Nome" required htmlFor="user-name">
            <Input
              id="user-name"
              required
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </Field>
          <Field label="Empresa" required htmlFor="user-company">
            <Select
              id="user-company"
              required
              value={form.companyId}
              onChange={(e) => setForm({ ...form, companyId: e.target.value })}
            >
              <option value="">— selecione —</option>
              {companiesData?.companies
                .filter((c) => c.active)
                .map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
            </Select>
          </Field>
          <Field label="Papel" required htmlFor="user-role">
            <Select
              id="user-role"
              value={form.role}
              onChange={(e) => setForm({ ...form, role: e.target.value })}
            >
              <option value="viewer">viewer — só consulta</option>
              <option value="editor">editor — consulta + funnels + triagem de issues</option>
              <option value="admin">admin — gerencia</option>
            </Select>
          </Field>
          <Field
            label={editing ? 'Nova senha (opcional)' : 'Senha inicial'}
            required={!editing}
            htmlFor="user-pass"
          >
            <Input
              id="user-pass"
              type="password"
              minLength={8}
              required={!editing}
              value={form.password}
              onChange={(e) => setForm({ ...form, password: e.target.value })}
              placeholder={editing ? 'deixe em branco para não alterar' : ''}
            />
          </Field>
          {editing && form.companyId ? (
            <UserPermissionsPanel
              userId={editing.id}
              companyId={form.companyId}
              role={form.role}
            />
          ) : null}
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
