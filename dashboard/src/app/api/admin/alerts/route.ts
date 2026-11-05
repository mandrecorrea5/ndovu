import { type NextRequest } from 'next/server';
import { upstream, requireSession, UpstreamError } from '@/lib/bff';
import { errorResponse, proxyResponse, handleRoute } from '@/lib/proxy';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

async function GETHandler(
  req: NextRequest, ctx: { params: Promise<Record<string, never>> },
  
) {
  const session = await requireSession();
  
  
  return upstreamRequest(
    "GET",
    "/v1/admin/alerts",
    session.token,
    undefined,
    req.nextUrl.searchParams,
  );
}
async function POSTHandler(
  req: NextRequest, ctx: { params: Promise<Record<string, never>> },
  
) {
  const session = await requireSession();
  
  const body = await req.json();
  return upstreamRequest(
    "POST",
    "/v1/admin/alerts",
    session.token,
    body,
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

export const GET = handleRoute(GETHandler);
export const POST = handleRoute(POSTHandler);
