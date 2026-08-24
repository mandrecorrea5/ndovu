import { test, expect } from '../fixtures/test';
import { loginAs } from '../helpers/auth';
import { seedApp, seedCompany, seedUser } from '../helpers/seed';

// Segurança crítica: admin de company A NUNCA deve enxergar recursos
// (users, apps, chaves, empresas) da company B. Confirmação end-to-end
// do isolamento cross-tenant implementado nos sprints anteriores.

test.describe('cross-tenant — admin A não vê recursos de B', () => {
  test('admin A vê só sua company + seus recursos em /admin/apps', async ({
    page, api, cleanup,
  }) => {
    // Setup: 2 companies com apps distintos + admin em cada.
    const cA = await seedCompany(api, 'CompA-' + Date.now());
    cleanup.push(cA.cleanup);
    const cB = await seedCompany(api, 'CompB-' + Date.now());
    cleanup.push(cB.cleanup);

    const appA = await seedApp(api, cA.resource.id, 'app-of-A');
    cleanup.push(appA.cleanup);
    const appB = await seedApp(api, cB.resource.id, 'app-of-B');
    cleanup.push(appB.cleanup);

    const adminA = await seedUser(api, cA.resource.id, 'admin');
    cleanup.push(adminA.cleanup);

    await loginAs(page, adminA.resource.email, adminA.resource.password);
    await page.goto('/admin/apps');

    // Vê app-of-A, NÃO vê app-of-B.
    await expect(page.getByText(appA.resource.name)).toBeVisible({ timeout: 5_000 });
    await expect(page.getByText(appB.resource.name)).toHaveCount(0);
  });

  test('admin A vê só a própria company em /admin/companies', async ({
    page, api, cleanup,
  }) => {
    const cA = await seedCompany(api, 'AloneCompA-' + Date.now());
    cleanup.push(cA.cleanup);
    const cB = await seedCompany(api, 'HiddenCompB-' + Date.now());
    cleanup.push(cB.cleanup);

    const adminA = await seedUser(api, cA.resource.id, 'admin');
    cleanup.push(adminA.cleanup);

    await loginAs(page, adminA.resource.email, adminA.resource.password);
    await page.goto('/admin/companies');

    await expect(page.getByText(cA.resource.name)).toBeVisible();
    await expect(page.getByText(cB.resource.name)).toHaveCount(0);
    // "Padrão" tampouco (não é company do admin A).
    await expect(page.getByText(/^Padrão$/)).toHaveCount(0);
  });

  test('admin A vê só users da própria company em /admin/users', async ({
    page, api, cleanup,
  }) => {
    const cA = await seedCompany(api, 'CA-' + Date.now());
    cleanup.push(cA.cleanup);
    const cB = await seedCompany(api, 'CB-' + Date.now());
    cleanup.push(cB.cleanup);

    const adminA = await seedUser(api, cA.resource.id, 'admin');
    cleanup.push(adminA.cleanup);
    const userB = await seedUser(api, cB.resource.id, 'viewer');
    cleanup.push(userB.cleanup);

    await loginAs(page, adminA.resource.email, adminA.resource.password);
    await page.goto('/admin/users');

    // Escopo na tabela (evita match com o menu do usuário no sidebar).
    // Cada linha tem nome + email na mesma célula — usa first() pra ambos.
    const table = page.getByRole('table');
    await expect(table.getByText(adminA.resource.email).first()).toBeVisible();
    await expect(table.getByText(userB.resource.email)).toHaveCount(0);
    await expect(table.getByText('admin@ndovu.local')).toHaveCount(0);
  });

  test('admin A não vê chaves de company B em /admin/keys', async ({
    page, api, cleanup,
  }) => {
    const cA = await seedCompany(api, 'CA-' + Date.now());
    cleanup.push(cA.cleanup);
    const cB = await seedCompany(api, 'CB-' + Date.now());
    cleanup.push(cB.cleanup);

    const appA = await seedApp(api, cA.resource.id, 'appA-key-' + Date.now());
    cleanup.push(appA.cleanup);
    const appB = await seedApp(api, cB.resource.id, 'appB-key-' + Date.now());
    cleanup.push(appB.cleanup);

    const adminA = await seedUser(api, cA.resource.id, 'admin');
    cleanup.push(adminA.cleanup);

    await loginAs(page, adminA.resource.email, adminA.resource.password);
    await page.goto('/admin/keys');

    // Chave de app-A aparece (via nome do app).
    await expect(page.getByText(appA.resource.name)).toBeVisible({ timeout: 5_000 });
    // Chave de app-B NÃO aparece.
    await expect(page.getByText(appB.resource.name)).toHaveCount(0);
  });
});
