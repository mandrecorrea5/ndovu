import 'server-only';

import { NextResponse } from 'next/server';
import { UpstreamError } from './bff';

/**
 * serializa a resposta da API para o browser. O proxy repassa o payload
 * cru com o mesmo status: a UI depende dos códigos 401/403/404/409 para
 * decidir redirect e mensagens.
 */
export function proxyResponse(data: unknown): NextResponse {
  return NextResponse.json(data ?? null, { status: 200 });
}

/**
 * errorResponse transforma um UpstreamError em resposta com o status
 * original da API.
 */
export function errorResponse(err: unknown): NextResponse {
  if (err instanceof UpstreamError) {
    return NextResponse.json(
      { error: err.message, ...(err.details ? { details: err.details } : {}) },
      { status: err.status },
    );
  }
  return NextResponse.json({ error: 'Erro interno no proxy' }, { status: 500 });
}
