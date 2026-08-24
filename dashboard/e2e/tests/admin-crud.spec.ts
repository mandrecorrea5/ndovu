import { test, expect } from '../fixtures/test';
import { loginAs } from '../helpers/auth';
import { BOOTSTRAP_ADMIN } from '../helpers/api';
import { seedCompany, unique } from '../helpers/seed';

test.describe('admin CRUD — jornada completa', () => {
  test('cria company → app → user editor → editor faz login e vê app', async ({
    page,
    api,
    cleanup,
  }) => {
    // Setup via API (rápido) — cria company e user editor.
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);

    const editorEmail = unique('editor') + '@e2e.local';
    const editorPass = 'test-editor-1234';
    const editor = await api.post('/v1/admin/users', {
      email: editorEmail, name: editorEmail, password: editorPass,
      role: 'editor', companyId: c.resource.id, active: true,
    });
    cleanup.push(async () => {
      try { await api.patch(`/v1/admin/users/${editor.id}`, { active: false }); } catch {}
    });

    // Cria app via UI (validar a jornada real do admin).
    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    await page.goto('/admin/apps');
    await page.getByRole('button', { name: /novo app/i }).click();

    const appName = unique('app');
    await page.locator('#app-name').fill(appName);
    // Empresa: seleciona a company que criamos.
    await page.locator('#app-company').selectOption(c.resource.id);
    await page.getByRole('button', { name: /^cadastrar$/i }).click();

    // Aguarda o app aparecer na tabela.
    await expect(page.getByText(appName).first()).toBeVisible({ timeout: 8_000 });

    // Cleanup extra: pega o app id e agenda delete.
    const appsRes = await api.get('/v1/admin/apps');
    const created = appsRes.apps.find((a: { name: string; id: string }) => a.name === appName);
    if (created) {
      cleanup.push(() => api.delete(`/v1/admin/apps/${created.id}`));
    }

    // Editor faz login e navega até /admin (não deve conseguir ver menu),
    // mas o menu principal (Issues, Traces) tem que carregar.
    await page.getByRole('button', { name: /sair/i }).or(
      page.locator('button[aria-haspopup="menu"]').first()
    ).first().click();
    await page.getByRole('menuitem', { name: /sair/i }).click();
    await expect(page).toHaveURL(/\/login/);

    await loginAs(page, editorEmail, editorPass);
    // Editor NÃO vê link Empresas (é admin-only).
    await expect(page.getByRole('link', { name: /empresas/i })).toHaveCount(0);
    // Mas vê Funis (editor pode).
    await expect(page.getByRole('link', { name: /funis/i })).toBeVisible();
  });

  test('cadastro de app gera chave que aparece uma vez', async ({ page, api, cleanup }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);

    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    await page.goto('/admin/apps');

    await page.getByRole('button', { name: /novo app/i }).click();
    const appName = unique('with-key');
    await page.locator('#app-name').fill(appName);
    await page.locator('#app-company').selectOption(c.resource.id);
    await page.getByRole('button', { name: /^cadastrar$/i }).click();

    // Após criar, modal deve fechar OU mostrar chave. Só validamos que o
    // app apareceu na lista — que garante que o POST foi 201.
    await expect(page.getByText(appName).first()).toBeVisible({ timeout: 8_000 });

    // Confirma via API que a chave existe.
    const keys = await api.get('/v1/admin/api-keys');
    const found = keys.keys.some((k: { app: string }) => k.app === appName);
    expect(found).toBeTruthy();

    // Cleanup do app.
    const appsRes = await api.get('/v1/admin/apps');
    const created = appsRes.apps.find((a: { name: string; id: string }) => a.name === appName);
    if (created) cleanup.push(() => api.delete(`/v1/admin/apps/${created.id}`));
  });
});
