import { type NextRequest } from 'next/server';
import { upstream, requireSession, UpstreamError } from '@/lib/bff';
import { errorResponse, proxyResponse } from '@/lib/proxy';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

export async function PATCH(
  req: NextRequest, ctx: { params: Promise<{ id: string }> },
  
) {
  const session = await requireSession();
  const id = (await ctx.params).id;
  const body = await req.json();
  return upstreamRequest(
    "PATCH",
    `/v1/admin/apps/${id}`,
    session.token,
    body,
    req.nextUrl.searchParams,
  );
}
export async function DELETE(
  req: NextRequest, ctx: { params: Promise<{ id: string }> },
  
) {
  const session = await requireSession();
  const id = (await ctx.params).id;
  
  return upstreamRequest(
    "DELETE",
    `/v1/admin/apps/${id}`,
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
