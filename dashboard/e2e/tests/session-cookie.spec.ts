// Contrato de sessão do BFF:
//  - o login emite um cookie httpOnly (inacessível a document.cookie);
//  - a sessão é o cookie — um request sem ele é 401;
//  - o logout limpa o cookie e a sessão morre (não sobrevive a reload).
//
// Este spec é a barreguarda P0: se o token voltar para localStorage ou
// a API voltar a aceitar sessão por outro canal, ele falha.

import { expect, test } from '@playwright/test';
import { BOOTSTRAP_ADMIN } from '../helpers/api';
import { loginAs, logout } from '../helpers/auth';

const SESSION_COOKIE = 'ndovu_session';

test.describe('sessão httpOnly (BFF)', () => {
  test('login seta cookie httpOnly e o token não é legível por JS', async ({ page }) => {
    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);

    const cookies = await page.context().cookies();
    const session = cookies.find((c) => c.name === SESSION_COOKIE);
    expect(session, 'cookie de sessão presente').toBeTruthy();
    expect(session!.httpOnly, 'cookie httpOnly').toBe(true);
    expect(session!.sameSite).toBe('Lax');

    // Nenhum vestígio de credencial no storage legível por script.
    const leak = await page.evaluate(() => {
      const dump = {
        local: Object.keys(localStorage),
        session: Object.keys(sessionStorage),
      };
      const all = [...dump.local, ...dump.session].join(' ');
      return {
        keys: all,
        hasToken: /(^|[^a-z])token([^a-z]|$)/i.test(all) || /bearer/i.test(document.cookie),
      };
    });
    expect(leak.hasToken, `storage com credencial: ${leak.keys}`).toBe(false);
    expect(await page.evaluate(() => document.cookie)).not.toContain(SESSION_COOKIE);
  });

  test('sem o cookie, a API de consulta responde 401', async ({ page, request }) => {
    const dashboard = 'http://localhost:13000';
    const resp = await request.get(`${dashboard}/api/stats/overview`);
    expect(resp.status()).toBe(401);
    // E sem credencial nenhuma, a página protegida redireciona ao login.
    await page.goto('/traces');
    await expect(page).toHaveURL(/\/login/);
  });

  test('logout derruba a sessão e um request autenticado passa a dar 401', async ({ page }) => {
    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    // sanity: sessão viva → 200
    const ok = await page.evaluate(() =>
      fetch('/api/auth/me', { credentials: 'same-origin' }).then((r) => r.status),
    );
    expect(ok).toBe(200);

    await logout(page);
    const after = await page.evaluate(() =>
      fetch('/api/auth/me', { credentials: 'same-origin' }).then((r) => r.status),
    );
    expect(after).toBe(401);
  });
});
