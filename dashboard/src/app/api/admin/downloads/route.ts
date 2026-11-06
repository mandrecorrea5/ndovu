import { type NextRequest } from 'next/server';
import { upstream, requireSession } from '@/lib/bff';
import { errorResponse, proxyResponse, handleRoute } from '@/lib/proxy';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

async function POSTHandler(req: NextRequest) {
  const session = await requireSession();
  const body = await req.json().catch(() => ({}));
  try {
    const data = await upstream('POST', '/v1/admin/downloads', { token: session.token, body });
    return proxyResponse(data);
  } catch (err) {
    return errorResponse(err);
  }
}

export const POST = handleRoute(POSTHandler);
