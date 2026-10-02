import { NextResponse, type NextRequest } from 'next/server';

/**
 * Gate de sessão + CSP com nonce por requisição.
 *
 * O CSP precisa ser montado aqui (não em header estático do Caddy) porque
 * o App Router do Next injeta scripts inline (payload de hidratação do RSC)
 * em toda página; sem nonce por request, 'script-src self' bloqueia esses
 * scripts e quebra a hidratação. Next lê o nonce do CSP já presente nos
 * headers da REQUEST (setado abaixo) e o aplica automaticamente aos scripts
 * que ele mesmo renderiza.
 *
 * Sessão: sem o cookie httpOnly presente e não-vazio, qualquer navegação do
 * dashboard vai para /login?next=…
 *
 * O gate é superficial — a autoridade é o BFF: /api/auth/me revalida com a
 * API e 401 limpa o cookie + redireciona. O middleware existe para não
 * renderizar o shell com URLs privadas antes do primeiro fetch.
 *
 * Não validamos o conteúdo do cookie aqui (edge runtime não tem
 * node:crypto/AES-GCM). Apenas presença: um cookie adulterado é rejeitado
 * na primeira chamada autenticada, não aqui.
 */
function isPublicPath(pathname: string): boolean {
  return (
    pathname === '/login' ||
    pathname.startsWith('/api/') ||
    pathname.startsWith('/_next/')
  );
}

function buildCsp(nonce: string): string {
  return `default-src 'self'; script-src 'self' 'nonce-${nonce}' 'strict-dynamic'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'`;
}

export function middleware(req: NextRequest) {
  const nonce = Buffer.from(crypto.randomUUID()).toString('base64');
  const csp = buildCsp(nonce);

  const requestHeaders = new Headers(req.headers);
  requestHeaders.set('x-nonce', nonce);
  requestHeaders.set('Content-Security-Policy', csp);

  if (!isPublicPath(req.nextUrl.pathname)) {
    const session = req.cookies.get('ndovu_session')?.value;
    if (!session) {
      const login = new URL('/login', req.url);
      login.searchParams.set('next', req.nextUrl.pathname + req.nextUrl.search);
      const redirect = NextResponse.redirect(login);
      redirect.headers.set('Content-Security-Policy', csp);
      return redirect;
    }
  }

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  response.headers.set('Content-Security-Policy', csp);
  return response;
}

export const config = {
  matcher: [
    /*
     * Tudo exceto assets verdadeiramente estáticos, que não precisam de
     * nonce nem passam pelo gate de sessão.
     */
    '/((?!_next/static|_next/image|favicon.ico|icon.svg|robots.txt).*)',
  ],
};
