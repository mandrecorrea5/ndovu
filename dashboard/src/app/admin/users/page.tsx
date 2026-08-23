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

// generatePassword devolve uma senha aleatória "curta e legível" — 12
// caracteres, sem ambiguidade visual (sem 0/O/1/l/I). Suficiente pra
// primeiro acesso; o usuário troca depois.
function generatePassword(): string {
  const alphabet = 'ABCDEFGHJKMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789';
  const bytes = new Uint32Array(12);
  crypto.getRandomValues(bytes);
  let out = '';
  for (const b of bytes) out += alphabet[b % alphabet.length];
  return out;
}

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
  // Quando o admin salva com senha preenchida, exibimos um toast leve de sucesso.
  const [passwordChangedFor, setPasswordChangedFor] = useState<string | null>(null);
  // Modal simples de "resetar senha" acionado direto na linha da tabela.
  const [resetting, setResetting] = useState<AdminUser | null>(null);
  const [resetPassword, setResetPassword] = useState('');

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
        return { user: await api.updateUser(editing.id, patch), passwordChanged: !!form.password };
      }
      const user = await api.createUser({
        email: form.email,
        name: form.name,
        password: form.password,
        role: form.role,
        companyId: form.companyId,
      });
      return { user, passwordChanged: false };
    },
    onSuccess: ({ user, passwordChanged }) => {
      invalidate();
      setOpen(false);
      if (passwordChanged) {
        setPasswordChangedFor(user.email);
        setTimeout(() => setPasswordChangedFor(null), 4000);
      }
    },
    onError: (err) =>
      setFeedback(err instanceof ApiError ? (err.details?.join('; ') ?? err.message) : 'Erro'),
  });

  const resetPasswordMutation = useMutation({
    mutationFn: () =>
      api.updateUser(resetting!.id, { password: resetPassword }),
    onSuccess: () => {
      const email = resetting?.email ?? '';
      setResetting(null);
      setResetPassword('');
      setPasswordChangedFor(email);
      setTimeout(() => setPasswordChangedFor(null), 4000);
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
      {passwordChangedFor ? (
        <div className="rounded-md border border-good/40 bg-good/10 px-3 py-2 text-sm text-good">
          Senha de <span className="mono">{passwordChangedFor}</span> atualizada com sucesso.
        </div>
      ) : null}
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
                    <div className="flex flex-wrap gap-1">
                      <Button size="sm" onClick={() => openEdit(u)}>
                        editar
                      </Button>
                      <Button
                        size="sm"
                        onClick={() => {
                          setResetting(u);
                          setResetPassword(generatePassword());
                        }}
                      >
                        senha
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
          <div
            className={
              editing
                ? 'rounded-md border border-hairline bg-plane/60 px-3 py-3 space-y-2'
                : 'space-y-2'
            }
          >
            <Field
              label={editing ? 'Alterar senha (opcional)' : 'Senha inicial'}
              required={!editing}
              htmlFor="user-pass"
              hint={
                editing
                  ? 'Preencha só se quiser trocar. Mínimo 8 caracteres.'
                  : 'Mínimo 8 caracteres. Use "Gerar" para uma senha aleatória.'
              }
            >
              <div className="flex gap-2">
                <Input
                  id="user-pass"
                  type="text"
                  minLength={8}
                  required={!editing}
                  value={form.password}
                  onChange={(e) => setForm({ ...form, password: e.target.value })}
                  placeholder={editing ? 'deixe em branco para não alterar' : ''}
                  autoComplete="new-password"
                  className="mono"
                />
                <Button
                  type="button"
                  onClick={() => setForm({ ...form, password: generatePassword() })}
                >
                  gerar
                </Button>
                {form.password ? (
                  <Button
                    type="button"
                    onClick={() => {
                      navigator.clipboard?.writeText(form.password);
                    }}
                  >
                    copiar
                  </Button>
                ) : null}
              </div>
            </Field>
          </div>
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

      <Modal
        open={!!resetting}
        onClose={() => {
          setResetting(null);
          setResetPassword('');
        }}
        title={resetting ? `Resetar senha: ${resetting.email}` : ''}
        description="Gera uma nova senha para o usuário. Copie e envie por canal seguro — o valor não fica salvo em claro depois de aplicado."
      >
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (resetPassword.length >= 8) resetPasswordMutation.mutate();
          }}
        >
          <Field label="Nova senha" required hint="Mínimo 8 caracteres.">
            <div className="flex gap-2">
              <Input
                type="text"
                minLength={8}
                required
                value={resetPassword}
                onChange={(e) => setResetPassword(e.target.value)}
                autoComplete="new-password"
                className="mono"
              />
              <Button type="button" onClick={() => setResetPassword(generatePassword())}>
                gerar
              </Button>
              {resetPassword ? (
                <Button
                  type="button"
                  onClick={() => navigator.clipboard?.writeText(resetPassword)}
                >
                  copiar
                </Button>
              ) : null}
            </div>
          </Field>
          <ModalActions>
            <Button
              onClick={() => {
                setResetting(null);
                setResetPassword('');
              }}
            >
              cancelar
            </Button>
            <Button
              variant="primary"
              type="submit"
              loading={resetPasswordMutation.isPending}
              disabled={resetPassword.length < 8}
            >
              resetar senha
            </Button>
          </ModalActions>
        </form>
      </Modal>
    </div>
  );
}
