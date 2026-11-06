import { type NextRequest } from 'next/server';
import { upstream, requireSession } from '@/lib/bff';
import { errorResponse, proxyResponse, handleRoute } from '@/lib/proxy';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

async function GETHandler(
  req: NextRequest,
  ctx: { params: Promise<{ id: string }> },
) {
  const session = await requireSession();
  const id = (await ctx.params).id;
  try {
    const data = await upstream('GET', `/v1/admin/downloads/${encodeURIComponent(id)}`, {
      token: session.token,
    });
    return proxyResponse(data);
  } catch (err) {
    return errorResponse(err);
  }
}

export const GET = handleRoute(GETHandler);
