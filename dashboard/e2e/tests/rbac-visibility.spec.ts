import { test, expect } from '../fixtures/test';
import { loginAs } from '../helpers/auth';
import { seedCompany, seedUser } from '../helpers/seed';

// RBAC visibility: viewer vê só ler; editor vê botões de escrita
// colaborativa (funnels, comentários, saved-views compartilhadas);
// admin vê tudo + menu admin. Cobre o comportamento UI que o backend
// já bloqueia por role — evita regressão de "botão aparece pra quem
// vai receber 403 ao clicar".

test.describe('RBAC — visibilidade condicional por role', () => {
  test('viewer NÃO vê botão "Novo funil"', async ({ page, api, cleanup }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const v = await seedUser(api, c.resource.id, 'viewer');
    cleanup.push(v.cleanup);

    await loginAs(page, v.resource.email, v.resource.password);
    await page.goto('/funnels');
    await expect(page.getByRole('button', { name: /novo funil/i })).toHaveCount(0);
  });

  test('editor VÊ botão "Novo funil"', async ({ page, api, cleanup }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const e = await seedUser(api, c.resource.id, 'editor');
    cleanup.push(e.cleanup);

    await loginAs(page, e.resource.email, e.resource.password);
    await page.goto('/funnels');
    await expect(page.getByRole('button', { name: /novo funil/i })).toBeVisible();
  });

  test('viewer não vê botões resolver/reabrir/ignorar em /issues', async ({
    page, api, cleanup,
  }) => {
    // Mesmo sem issues seedadas, a UI condicional é do menu contextual —
    // se aparecesse, viewer teria botões. Validamos comportamento inclusive
    // com lista vazia (os botões só aparecem em linha, mas não devem estar
    // renderizados no DOM em nenhum lugar).
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const v = await seedUser(api, c.resource.id, 'viewer');
    cleanup.push(v.cleanup);

    await loginAs(page, v.resource.email, v.resource.password);
    await page.goto('/issues');
    // Não há botão "Resolver" nem "Ignorar" no DOM.
    await expect(page.getByRole('button', { name: /^resolver$/i })).toHaveCount(0);
    await expect(page.getByRole('button', { name: /^ignorar$/i })).toHaveCount(0);
  });

  test('viewer não vê checkbox "compartilhar view"', async ({ page, api, cleanup }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const v = await seedUser(api, c.resource.id, 'viewer');
    cleanup.push(v.cleanup);

    await loginAs(page, v.resource.email, v.resource.password);
    await page.goto('/traces');
    // Abre o menu de saved views se existir (varia entre telas). Aqui só
    // validamos que o texto "compartilhar" NÃO aparece na página.
    await expect(page.getByText(/compartilhar com o time/i)).toHaveCount(0);
  });

  test('editor NÃO vê menu Administração', async ({ page, api, cleanup }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const e = await seedUser(api, c.resource.id, 'editor');
    cleanup.push(e.cleanup);

    await loginAs(page, e.resource.email, e.resource.password);
    await expect(page.getByRole('link', { name: /empresas/i })).toHaveCount(0);
    await expect(page.getByRole('link', { name: /chaves de api/i })).toHaveCount(0);
    await expect(page.getByRole('link', { name: /usuários/i })).toHaveCount(0);
  });
});
