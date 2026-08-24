import { test, expect } from '../fixtures/test';
import { loginAs, logout } from '../helpers/auth';
import { BOOTSTRAP_ADMIN } from '../helpers/api';
import { seedCompany, seedUser } from '../helpers/seed';

test.describe('autenticação', () => {
  test('login OK com bootstrap admin leva à Visão geral', async ({ page }) => {
    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    // Home renderiza título/heading conhecido.
    await expect(page.getByRole('heading', { name: /vis[aã]o geral/i })).toBeVisible();
  });

  test('senha errada mostra mensagem sem sair de /login', async ({ page }) => {
    await page.goto('/login');
    await page.locator('#login-email').fill(BOOTSTRAP_ADMIN.email);
    await page.locator('#login-pass').fill('senha-errada-1234');
    await page.getByRole('button', { name: /entrar/i }).click();
    // Mensagem de erro visível (role="alert").
    await expect(page.getByRole('alert')).toBeVisible({ timeout: 5_000 });
    // Não navegou para "/".
    await expect(page).toHaveURL(/\/login/);
  });

  test('sessão persiste após reload', async ({ page }) => {
    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    await page.reload();
    // Continua no dashboard, não voltou pro login.
    await expect(page).not.toHaveURL(/\/login/);
    await expect(page.getByRole('heading', { name: /vis[aã]o geral/i })).toBeVisible();
  });

  test('logout limpa sessão e volta pra /login', async ({ page }) => {
    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    await logout(page);
    // Depois de logout, tentar acessar rota protegida redireciona pra login.
    await page.goto('/');
    await expect(page).toHaveURL(/\/login/);
  });

  test('usuário criado via admin consegue fazer login', async ({ page, api, cleanup }) => {
    // Cria company + user novo, faz login como esse user, valida.
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const u = await seedUser(api, c.resource.id, 'viewer');
    cleanup.push(u.cleanup);

    await loginAs(page, u.resource.email, u.resource.password);
    await expect(page.getByRole('heading', { name: /vis[aã]o geral/i })).toBeVisible();
  });
});
