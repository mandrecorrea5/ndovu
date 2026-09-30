import { type NextRequest } from 'next/server';
import { upstream, requireSession, UpstreamError } from '@/lib/bff';
import { errorResponse, proxyResponse } from '@/lib/proxy';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

export async function GET(
  req: NextRequest, ctx: { params: Promise<Record<string, never>> },
  
) {
  const session = await requireSession();
  
  
  return upstreamRequest(
    "GET",
    "/v1/saved-views",
    session.token,
    undefined,
    req.nextUrl.searchParams,
  );
}
export async function POST(
  req: NextRequest, ctx: { params: Promise<Record<string, never>> },
  
) {
  const session = await requireSession();
  
  const body = await req.json();
  return upstreamRequest(
    "POST",
    "/v1/saved-views",
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
