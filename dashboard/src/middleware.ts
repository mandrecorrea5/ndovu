import { NextResponse, type NextRequest } from 'next/server';

/**
 * Middleware de rota: sem a sessão (cookie httpOnly presente e não-vazia),
 * qualquer navegação do dashboard vai para /login?next=…
 *
 * O gate é superficial — a autoridade é o BFF: /api/auth/me revalida com a
 * API e 401 limpa o cookie + redireciona. O middleware existe para não
 * renderizar o shell com URLs privadas antes do primeiro fetch.
 *
 * Não validamos o conteúdo do cookie aqui (edge runtime não tem
 * node:crypto/AES-GCM). Apenas presença: um cookie adulterado é rejeitado
 * na primeira chamada autenticada, não aqui.
 */
export function middleware(req: NextRequest) {
  const session = req.cookies.get('ndovu_session')?.value;
  if (session) return NextResponse.next();

  const login = new URL('/login', req.url);
  login.searchParams.set('next', req.nextUrl.pathname + req.nextUrl.search);
  return NextResponse.redirect(login);
}

export const config = {
  matcher: [
    /*
     * Tudo exceto: login, assets, ícone, favicon e as rotas do próprio BFF
     * (que têm controle próprio de sessão). /api/auth/* precisa responder
     * 401 em JSON para a UI, não redirect de navegação.
     */
    '/((?!api/|login|icon.svg|favicon.ico|_next/static|_next/image|robots.txt).*)',
  ],
};
