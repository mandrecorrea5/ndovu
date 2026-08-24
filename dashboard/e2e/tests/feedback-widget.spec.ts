import { test, expect } from '../fixtures/test';
import { loginAs } from '../helpers/auth';
import { BOOTSTRAP_ADMIN } from '../helpers/api';
import { postFeedback, seedApp, seedCompany, unique } from '../helpers/seed';

// Feedback do usuário: fluxo do widget do SDK → backoffice. Ingesta 1 bug
// via /v1/feedbacks com X-Api-Key, admin bootstrap abre /admin/feedbacks
// e faz a triagem: triagem → resolver, e por fim remove.

test.describe('feedback widget → backoffice', () => {
  test('feedback ingerido aparece no admin; triagem, resolver e remover funcionam', async ({
    page, api, cleanup, request,
  }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const app = await seedApp(api, c.resource.id);
    cleanup.push(app.cleanup);

    // Marker único no corpo pra localizar o card na lista sem esbarrar em
    // feedbacks residuais de outros runs.
    const marker = unique('boom');
    const message = `um bug — marker=${marker}`;
    const fb = await postFeedback(request, app.resource.name, app.resource.key, {
      type: 'bug',
      message,
      email: 'usuario@exemplo.com',
    });
    // Cleanup best-effort — se o teste já removeu, admin DELETE dá 404 (idempotente).
    cleanup.push(async () => api.delete(`/v1/admin/feedbacks/${fb.id}`));

    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    await page.goto('/admin/feedbacks');

    // Filtro default é `status=new` — o feedback recém-criado aparece.
    const card = page.locator('article', { hasText: marker });
    await expect(card).toBeVisible({ timeout: 10_000 });
    await expect(card.getByText(/● novo/)).toBeVisible();

    // Triagem → status vira "◐ triagem".
    await card.getByRole('button', { name: /^triagem$/i }).click();
    // Ao mudar de status, o card some do filtro "novos" (default). Amplia
    // filtro pra "todos" pra continuar interagindo.
    await page.getByRole('combobox').first().selectOption('');
    await expect(card.getByText(/◐ triagem/)).toBeVisible({ timeout: 10_000 });

    // Resolver → vira "✓ resolvido".
    await card.getByRole('button', { name: /^resolver$/i }).click();
    await expect(card.getByText(/✓ resolvido/)).toBeVisible({ timeout: 10_000 });

    // Remover com confirm nativo.
    page.once('dialog', (d) => d.accept());
    await card.getByRole('button', { name: /^remover$/i }).click();
    await expect(card).toHaveCount(0, { timeout: 10_000 });
  });
});
