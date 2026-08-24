import { test, expect } from '../fixtures/test';
import { loginAs } from '../helpers/auth';
import { seedApp, seedCompany, seedUser, unique } from '../helpers/seed';

// Funis: teste de CRUD focado em UI (listagem + seleção + remoção). A criação
// vai via API pra tirar o modal de 15 campos do caminho crítico do teste —
// preservamos o valor de o editor VER o funil, selecioná-lo e apagá-lo.

test.describe('funnels — visualizar e remover pela UI', () => {
  test('editor lista funil criado via API, seleciona e remove com confirm', async ({
    page, api, cleanup,
  }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const app = await seedApp(api, c.resource.id);
    cleanup.push(app.cleanup);
    const editor = await seedUser(api, c.resource.id, 'editor');
    cleanup.push(editor.cleanup);

    const funnelName = unique('funnel-ui');
    const funnel = await api.post('/v1/funnels', {
      app: app.resource.name,
      name: funnelName,
      windowSeconds: 1800,
      steps: [
        { name: 'Passo 1', match: { type: 'page_view' } },
        { name: 'Passo 2', match: { type: 'action' } },
      ],
    });
    cleanup.push(async () => api.delete(`/v1/funnels/${funnel.id}`));

    await loginAs(page, editor.resource.email, editor.resource.password);
    await page.goto('/funnels');

    // Aparece na sidebar (busca pelo nome, único).
    const sidebarBtn = page.getByRole('button', { name: new RegExp(funnelName) });
    await expect(sidebarBtn).toBeVisible({ timeout: 10_000 });

    // Seleciona → header exibe o nome + botões editar/remover pra editor.
    await sidebarBtn.click();
    await expect(page.getByRole('heading', { name: funnelName })).toBeVisible();
    await expect(page.getByRole('button', { name: /^editar$/i })).toBeVisible();

    // Remove com confirm.
    page.once('dialog', (d) => d.accept());
    await page.getByRole('button', { name: /^remover$/i }).click();

    // Sidebar perde o funil (poll — invalidação assíncrona do react-query).
    await expect(sidebarBtn).toHaveCount(0, { timeout: 10_000 });
  });
});
