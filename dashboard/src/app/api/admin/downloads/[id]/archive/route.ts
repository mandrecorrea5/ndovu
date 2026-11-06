import { type NextRequest } from 'next/server';
import { requireSession, upstreamRaw } from '@/lib/bff';
import { handleRoute } from '@/lib/proxy';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

/**
 * Repassa o zip do downloader sem passar por JSON: o corpo é binário e o
 * browser precisa do Content-Disposition para salvar com nome certo.
 */
async function GETHandler(
  req: NextRequest,
  ctx: { params: Promise<{ id: string }> },
) {
  const session = await requireSession();
  const id = (await ctx.params).id;

  let res: Response;
  try {
    res = await upstreamRaw(`/v1/admin/downloads/${encodeURIComponent(id)}/archive`, {
      token: session.token,
    });
  } catch {
    return Response.json(
      { error: 'API indisponível — verifique se a Ndovu API está no ar.' },
      { status: 502, headers: { 'Cache-Control': 'no-store' } },
    );
  }

  if (!res.ok) {
    const text = await res.text();
    let payload: unknown = { error: `HTTP ${res.status}` };
    try {
      payload = JSON.parse(text);
    } catch {
      // corpo não-JSON: mantém a mensagem genérica
    }
    return Response.json(payload, {
      status: res.status,
      headers: { 'Cache-Control': 'no-store' },
    });
  }

  return new Response(res.body, {
    status: 200,
    headers: {
      'Content-Type': res.headers.get('Content-Type') ?? 'application/zip',
      'Content-Disposition':
        res.headers.get('Content-Disposition') ?? `attachment; filename="ndovu-mp3-${id}.zip"`,
      'Cache-Control': 'no-store',
    },
  });
}

export const GET = handleRoute(GETHandler);
