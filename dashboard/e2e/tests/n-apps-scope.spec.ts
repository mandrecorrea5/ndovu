import { test, expect } from '../fixtures/test';
import { loginAs } from '../helpers/auth';
import { seedApp, seedCompany, seedUser, ingestEvent } from '../helpers/seed';

// Modelo N apps: editor/viewer com grant explícito vê só os apps
// concedidos; sem grant vê todos da company. Cobre user_app_permissions
// ponta-a-ponta (backend + UI).

test.describe('N apps — RBAC granular por app', () => {
  test('editor sem grant vê ambos os apps; com grant só o concedido; ao revogar volta', async ({
    page, api, cleanup, request,
  }) => {
    // Setup: 1 company + 2 apps + editor + 1 evento em cada app.
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const app1 = await seedApp(api, c.resource.id);
    cleanup.push(app1.cleanup);
    const app2 = await seedApp(api, c.resource.id);
    cleanup.push(app2.cleanup);
    const editor = await seedUser(api, c.resource.id, 'editor');
    cleanup.push(editor.cleanup);

    await ingestEvent(request, app1.resource.name, app1.resource.key, 'ev-app1');
    await ingestEvent(request, app2.resource.name, app2.resource.key, 'ev-app2');

    // Aguarda writer persistir os 2 eventos.
    await expect
      .poll(async () => {
        const res = await api.get(
          `/v1/events?app=${encodeURIComponent(app2.resource.name)}&limit=10`,
        );
        return (res.events ?? []).length > 0;
      }, { timeout: 15_000, intervals: [500, 1000, 2000] })
      .toBeTruthy();

    // Editor SEM grant faz login → vê os 2 apps.
    await loginAs(page, editor.resource.email, editor.resource.password);
    await page.goto('/traces');
    await expect(page.getByText('ev-app1').first()).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('ev-app2').first()).toBeVisible();

    // Admin concede grant só em app1.
    await api.put(
      `/v1/admin/users/${editor.resource.id}/permissions/${app1.resource.id}`,
      { role: 'editor' },
    );

    // Editor recarrega — cache miss no tenantScope (30s TTL da chave, mas
    // tenantScope resolve por request). Vê só app1.
    await page.reload();
    await expect(page.getByText('ev-app1').first()).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('ev-app2')).toHaveCount(0);

    // Admin revoga grant → editor volta a ver os 2.
    await api.delete(
      `/v1/admin/users/${editor.resource.id}/permissions/${app1.resource.id}`,
    );
    await page.reload();
    await expect(page.getByText('ev-app1').first()).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('ev-app2').first()).toBeVisible();
  });
});
