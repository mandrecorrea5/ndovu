import { NextResponse, type NextRequest } from 'next/server';
import { clearSessionCookie, login, UpstreamError } from '@/lib/bff';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

export async function POST(req: NextRequest) {
  const body = (await req.json().catch(() => null)) as {
    email?: string;
    password?: string;
  } | null;

  const email = body?.email?.trim();
  const password = body?.password;
  if (!email || !password) {
    return NextResponse.json({ error: 'email e password são obrigatórios' }, { status: 400 });
  }

  try {
    const session = await login(email, password);
    return NextResponse.json({ user: session.user, expiresAt: session.expiresAt });
  } catch (err) {
    await clearSessionCookie();
    if (err instanceof UpstreamError) {
      return NextResponse.json(
        { error: err.message, ...(err.details ? { details: err.details } : {}) },
        { status: err.status },
      );
    }
    return NextResponse.json({ error: 'Falha no login' }, { status: 500 });
  }
}
