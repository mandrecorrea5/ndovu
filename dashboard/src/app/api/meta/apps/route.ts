import { type NextRequest } from 'next/server';
import { upstream, requireSession } from '@/lib/bff';
import { errorResponse, proxyResponse } from '@/lib/proxy';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

export async function GET(req: NextRequest) {
  const session = await requireSession();
  try {
    const data = await upstream('GET', '/v1/meta/apps', {
      token: session.token,
      query: req.nextUrl.searchParams,
    });
    return proxyResponse(data);
  } catch (err) {
    return errorResponse(err);
  }
}
