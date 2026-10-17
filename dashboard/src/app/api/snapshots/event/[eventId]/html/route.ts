import { NextResponse, type NextRequest } from 'next/server';
import { requireSession, upstreamRaw } from '@/lib/bff';
import { errorResponse } from '@/lib/proxy';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

/**
 * HTML do session snapshot — pass-through CRU.
 *
 * A API devolve isto como `text/html` (handlers.go PostSnapshot/GetSnapshot),
 * não como JSON. Por isso não usamos `upstream` + `proxyResponse` como nas
 * outras rotas: o corpo seria serializado como JSON e o iframe renderizaria o
 * HTML escapado. Repassamos status, Content-Type e corpo sem tocar neles — o
 * cliente (`api.snapshotHTML`) lê com `r.text()` e checa `res.ok` sozinho.
 */
export async function GET(
  req: NextRequest,
  ctx: { params: Promise<{ eventId: string }> },
) {
  let session;
  try {
    session = await requireSession();
  } catch (err) {
    return errorResponse(err);
  }

  const { eventId } = await ctx.params;

  let res: Response;
  try {
    res = await upstreamRaw(`/v1/snapshots/event/${encodeURIComponent(eventId)}/html`, {
      token: session.token,
      query: req.nextUrl.searchParams,
    });
  } catch {
    return NextResponse.json(
      { error: 'API indisponível — verifique se a Ndovu API está no ar.' },
      { status: 502, headers: { 'Cache-Control': 'no-store' } },
    );
  }

  const body = await res.text();
  if (!res.ok) {
    // Corpo de erro da API é JSON ({error, details}); preservamos status e
    // corpo para o dashboard diferenciar 404 (sem snapshot) de 503 (blob off).
    return new NextResponse(body || null, {
      status: res.status,
      headers: {
        'Content-Type': res.headers.get('content-type') ?? 'application/json',
        'Cache-Control': 'no-store',
      },
    });
  }

  return new NextResponse(body, {
    status: 200,
    headers: {
      'Content-Type': res.headers.get('content-type') ?? 'text/html; charset=utf-8',
      'Cache-Control': 'no-store',
      'X-Content-Type-Options': 'nosniff',
    },
  });
}
