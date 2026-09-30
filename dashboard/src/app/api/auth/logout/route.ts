import { NextResponse } from 'next/server';
import { clearSessionCookie, readSession, UpstreamError, requireSession } from '@/lib/bff';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

export async function POST() {
  await clearSessionCookie();
  return NextResponse.json({ ok: true });
}

export async function GET() {
  try {
    const session = await requireSession();
    return NextResponse.json({
      user: session.user,
      expiresAt: session.expiresAt,
      authenticated: true,
    });
  } catch (err) {
    await clearSessionCookie();
    if (err instanceof UpstreamError) {
      return NextResponse.json({ error: err.message, authenticated: false }, { status: err.status });
    }
    return NextResponse.json(
      { error: 'API indisponível', authenticated: false },
      { status: 502 },
    );
  }
}
