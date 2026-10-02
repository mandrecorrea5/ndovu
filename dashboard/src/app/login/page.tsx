'use client';

import { useSearchParams } from 'next/navigation';
import { Suspense, useState } from 'react';
import { Button, Field, Input } from '@/components/form';
import { ApiErrorLite, login as loginSession } from '@/lib/auth';

function LoginForm() {
  const params = useSearchParams();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      await loginSession(email, password);
      const next = params.get('next');
      // Navegação dura (não router.replace + router.refresh do App Router):
      // as duas chamadas competem entre si — refresh() pode re-renderizar a
      // rota ainda em transição e a navegação "perde", deixando a página
      // visualmente presa em /login mesmo com o cookie de sessão já setado.
      window.location.assign(next && next.startsWith('/') ? next : '/');
    } catch (err) {
      setError(err instanceof ApiErrorLite ? err.message : 'Falha no login');
      setLoading(false);
    }
  };

  return (
    <main className="flex min-h-screen items-center justify-center px-4">
      <form onSubmit={submit} className="card w-full max-w-sm space-y-4 px-6 py-8">
        <div className="text-center">
          <p className="text-3xl" aria-hidden>
            🐘
          </p>
          <h1 className="mt-1 text-xl font-semibold">Ndovu</h1>
          <p className="text-sm text-ink-2">Frontend Journey Tracing</p>
        </div>

        <Field label="E-mail" required htmlFor="login-email">
          <Input
            id="login-email"
            type="email"
            autoComplete="username"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </Field>

        <Field label="Senha" required htmlFor="login-pass">
          <Input
            id="login-pass"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>

        {error ? (
          <p className="text-sm text-critical" role="alert">
            {error}
          </p>
        ) : null}

        <Button variant="primary" type="submit" loading={loading} className="w-full">
          {loading ? 'Entrando…' : 'Entrar'}
        </Button>
      </form>
    </main>
  );
}

export default function LoginPage() {
  return (
    <Suspense>
      <LoginForm />
    </Suspense>
  );
}
