import { type NextRequest } from 'next/server';
import { upstream, requireSession } from '@/lib/bff';
import { errorResponse, proxyResponse } from '@/lib/proxy';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

/** Trace unificado (eventos do SDK + spans OTLP), já restrito ao scope do usuário pela API. */
export async function GET(req: NextRequest, ctx: { params: Promise<{ traceId: string }> }) {
  const session = await requireSession();
  const { traceId } = await ctx.params;
  try {
    const data = await upstream('GET', `/v1/traces/${encodeURIComponent(traceId)}`, {
      token: session.token,
      query: req.nextUrl.searchParams,
    });
    return proxyResponse(data);
  } catch (err) {
    return errorResponse(err);
  }
}
