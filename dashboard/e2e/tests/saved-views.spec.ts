import { test, expect } from '../fixtures/test';
import { loginAs } from '../helpers/auth';
import { seedCompany, seedUser, unique } from '../helpers/seed';

// Saved views em /traces: editor salva uma view (com escopo compartilhado),
// aplica em nova sessão e vê que os filtros voltam via URL; viewer da
// mesma company enxerga a view compartilhada em "compartilhadas pelo time".

test.describe('saved views — /traces', () => {
  test('editor cria view compartilhada; aplica reinstala filtros; viewer da company vê', async ({
    page, api, cleanup,
  }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const editor = await seedUser(api, c.resource.id, 'editor');
    cleanup.push(editor.cleanup);
    const viewer = await seedUser(api, c.resource.id, 'viewer');
    cleanup.push(viewer.cleanup);

    // Editor abre /traces com "somente erros" já ligado pela URL — captura
    // esse filtro no snapshot do currentFilters.
    await loginAs(page, editor.resource.email, editor.resource.password);
    await page.goto('/traces?onlyErrors=true');

    // Consulta pra que setParam empurre onlyErrors=true (query string real).
    await page.getByRole('button', { name: /^consultar$/i }).click();
    await expect(page).toHaveURL(/onlyErrors=true/);

    // Salva a view compartilhada com nome único.
    const viewName = unique('view-erros');
    await page.getByRole('button', { name: /^views/i }).click();
    await page.getByRole('button', { name: /salvar filtros atuais como view/i }).click();
    await page.getByPlaceholder(/ex\.: Erros/i).fill(viewName);
    await page.getByLabel(/compartilhar com o time/i).check();
    await page.getByRole('button', { name: /^salvar$/i }).click();

    // Modal fecha, view aparece no menu — reabre e valida.
    await expect(page.getByPlaceholder(/ex\.: Erros/i)).toHaveCount(0);
    await page.getByRole('button', { name: /^views/i }).click();
    await expect(page.getByRole('button', { name: new RegExp(viewName) })).toBeVisible();

    // Reset da URL pra provar que aplicar a view REINSTALA filtro.
    await page.goto('/traces');
    await expect(page).not.toHaveURL(/onlyErrors=true/);
    await page.getByRole('button', { name: /^views/i }).click();
    await page.getByRole('button', { name: new RegExp(viewName) }).click();
    await expect(page).toHaveURL(/onlyErrors=true/);

    // Logout + viewer da mesma company vê a view em "compartilhadas".
    await page.context().clearCookies();
    await loginAs(page, viewer.resource.email, viewer.resource.password);
    await page.goto('/traces');
    await page.getByRole('button', { name: /^views/i }).click();
    await expect(
      page.getByText(/compartilhadas pelo time/i),
    ).toBeVisible();
    await expect(page.getByRole('button', { name: new RegExp(viewName) })).toBeVisible();
  });
});
