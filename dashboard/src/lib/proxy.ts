import 'server-only';

import { NextResponse } from 'next/server';
import { UpstreamError } from './bff';

/**
 * serializa a resposta da API para o browser. O proxy repassa o payload
 * cru com o mesmo status: a UI depende dos códigos 401/403/404/409 para
 * decidir redirect e mensagens.
 */
export function proxyResponse(data: unknown): NextResponse {
  return NextResponse.json(data ?? null, {
    status: 200,
    headers: { 'Cache-Control': 'no-store' },
  });
}

/**
 * errorResponse transforma um UpstreamError em resposta com o status
 * original da API.
 */
export function errorResponse(err: unknown): NextResponse {
  if (err instanceof UpstreamError) {
    return NextResponse.json(
      { error: err.message, ...(err.details ? { details: err.details } : {}) },
      { status: err.status, headers: { 'Cache-Control': 'no-store' } },
    );
  }
  return NextResponse.json(
    { error: 'Erro interno no proxy' },
    { status: 500, headers: { 'Cache-Control': 'no-store' } },
  );
}

/**
 * handleRoute envolve um route handler do App Router e transforma qualquer
 * exceção não tratada em resposta JSON com o status correto.
 *
 * Motivo: requireSession() lança UpstreamError(401) quando a sessão está
 * ausente/expirada. Sem o wrapper, o Next trata a exceção como erro interno e
 * devolve 500 — o browser não consegue distinguir "faça login" de "quebrou".
 */
export function handleRoute<A extends unknown[]>(
  handler: (...args: A) => Response | Promise<Response>,
): (...args: A) => Promise<Response> {
  return async (...args: A) => {
    try {
      return await handler(...args);
    } catch (err) {
      return errorResponse(err);
    }
  };
}
