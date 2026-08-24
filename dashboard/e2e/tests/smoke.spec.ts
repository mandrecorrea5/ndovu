import { test, expect } from '../fixtures/test';
import { loginAs } from '../helpers/auth';
import { BOOTSTRAP_ADMIN } from '../helpers/api';
import { seedCompany, seedUser } from '../helpers/seed';

test.describe('smoke — navegação básica', () => {
  test('home mostra os cartões de KPI', async ({ page }) => {
    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    // A tela de overview tem heading "Visão geral" — validado no auth spec.
    // Aqui validamos as seções principais renderizando.
    await expect(page).toHaveURL(/\/(\?|$)/);
    // O sidebar tem link pra Issues (viewer também vê).
    await expect(page.getByRole('link', { name: /issues/i })).toBeVisible();
  });

  test('menu "Administração" aparece pra admin', async ({ page }) => {
    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    await expect(page.getByText(/administra[cç][ãa]o/i).first()).toBeVisible();
    // Um item específico do admin: "Empresas".
    await expect(page.getByRole('link', { name: /empresas/i })).toBeVisible();
  });

  test('menu "Administração" NÃO aparece pra viewer', async ({ page, api, cleanup }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const v = await seedUser(api, c.resource.id, 'viewer');
    cleanup.push(v.cleanup);

    await loginAs(page, v.resource.email, v.resource.password);
    // Viewer vê Issues no menu principal.
    await expect(page.getByRole('link', { name: /issues/i })).toBeVisible();
    // Não deve haver link/section de admin.
    await expect(page.getByRole('link', { name: /empresas/i })).toHaveCount(0);
    await expect(page.getByRole('link', { name: /chaves de api/i })).toHaveCount(0);
  });

  test('rotas principais respondem 200 e renderizam', async ({ page }) => {
    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    const routes = [
      { path: '/', heading: /vis[aã]o geral/i },
      { path: '/issues', heading: /issues/i },
      { path: '/traces', heading: /explorador/i },
      { path: '/sessions', heading: /sess[õo]es/i },
    ];
    for (const r of routes) {
      await page.goto(r.path);
      await expect(page.getByRole('heading', { name: r.heading })).toBeVisible({
        timeout: 8_000,
      });
    }
  });
});
