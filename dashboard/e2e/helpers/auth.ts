// Helpers de UI login/logout — usados pelos testes que precisam navegar
// como um usuário específico. Sempre validam o resultado (URL final).

import { expect, type Page } from '@playwright/test';

/**
 * Faz login via UI (não via API). Preferir esta forma quando o teste
 * quer validar a jornada real (o usuário digita, submete, é redirecionado).
 * Pra setup rápido use loginAPI + storageState.
 */
export async function loginAs(page: Page, email: string, password: string): Promise<void> {
  await page.goto('/login');
  await page.locator('#login-email').fill(email);
  await page.locator('#login-pass').fill(password);
  await page.getByRole('button', { name: /entrar/i }).click();
  // Login redireciona para "/" (Home) ou path definido em `?next`.
  await expect(page).toHaveURL(/\/(\?|$)/, { timeout: 10_000 });
  // A sessão BFF é o cookie httpOnly — a UI só sabe que está logada quando o
  // /api/auth/me confirma. Sem essa espera, um teste pode navegar antes da
  // sessão estar de fato estabelecida.
  await page.waitForResponse(
    (res) => res.url().includes('/api/auth/me') && res.status() === 200,
    { timeout: 10_000 },
  );
}

/** Faz logout via UI e confirma redirect pra /login. */
export async function logout(page: Page): Promise<void> {
  // O menu do usuário abre ao clicar no botão com as iniciais
  // (aria-haspopup="menu"); o item "Sair" aparece dentro dele.
  await page.locator('button[aria-haspopup="menu"]').first().click();
  await page.getByRole('menuitem', { name: /sair/i }).click();
  await expect(page).toHaveURL(/\/login/, { timeout: 10_000 });
}
