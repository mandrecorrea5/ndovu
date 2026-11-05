import { type NextRequest } from 'next/server';
import { upstream, requireSession, UpstreamError } from '@/lib/bff';
import { errorResponse, proxyResponse, handleRoute } from '@/lib/proxy';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

async function DELETEHandler(
  req: NextRequest, ctx: { params: Promise<{ userId: string }> },
  
) {
  const session = await requireSession();
  const userId = (await ctx.params).userId;
  
  return upstreamRequest(
    "DELETE",
    `/v1/admin/gdpr/user/${userId}`,
    session.token,
    undefined,
    req.nextUrl.searchParams,
  );
}

async function upstreamRequest(
  method: string,
  path: string,
  token: string,
  body: unknown,
  query: URLSearchParams,
) {
  try {
    const data = await upstream(method, path, { token, body: body ?? undefined, query });
    return proxyResponse(data);
  } catch (err) {
    return errorResponse(err);
  }
}

export const DELETE = handleRoute(DELETEHandler);
