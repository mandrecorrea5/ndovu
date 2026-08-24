import { test, expect } from '../fixtures/test';
import { loginAs } from '../helpers/auth';
import { ingestError, seedApp, seedCompany, seedUser } from '../helpers/seed';

// Triagem end-to-end: um erro entra, agrupa por fingerprint, editor
// resolve → reabre → ignora, e o backend persiste em cada passo.

test.describe('triagem de issues (editor)', () => {
  test('editor resolve → reabre → ignora issue agrupada', async ({
    page, api, cleanup, request,
  }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const app = await seedApp(api, c.resource.id);
    cleanup.push(app.cleanup);
    const editor = await seedUser(api, c.resource.id, 'editor');
    cleanup.push(editor.cleanup);

    // Erro estável — code em CAPS entra literal no fingerprint (Fingerprint()
    // faz Upper+Trim). Nome único por run pra achar a linha sem colisão.
    const code = `E2E-TRIAGE-${Date.now().toString(36).toUpperCase()}`;
    const msg = 'triage boom';
    await ingestError(request, app.resource.name, app.resource.key, code, msg);

    // Aguarda writer persistir + issue aparecer no listing (5s bucket + cache).
    await expect
      .poll(
        async () => {
          const res = await api.get(
            `/v1/issues?app=${encodeURIComponent(app.resource.name)}&onlyOpen=true`,
          );
          return (res.issues ?? []).some((i: { code?: string }) => i.code === code);
        },
        { timeout: 20_000, intervals: [500, 1000, 2000] },
      )
      .toBeTruthy();

    await loginAs(page, editor.resource.email, editor.resource.password);
    await page.goto('/issues');

    // Filtro "só abertas" é default ligado — desligamos pra que a issue
    // continue visível após passar por resolvida/ignorada.
    await page.getByLabel(/só abertas/i).uncheck();

    // Localiza a linha pelo code (único). Escopa botões à linha pra não
    // colidir com issues de outros testes rodando na mesma company/host.
    const row = page.getByRole('row', { name: new RegExp(code) });
    await expect(row).toBeVisible({ timeout: 10_000 });
    await expect(row.getByText(/^aberta$/)).toBeVisible();

    // Resolver.
    await row.getByRole('button', { name: /^resolver$/i }).click();
    await expect(row.getByText(/^resolvida$/)).toBeVisible({ timeout: 10_000 });

    // Reabrir (o botão "resolver" some, aparece "reabrir").
    await row.getByRole('button', { name: /^reabrir$/i }).click();
    await expect(row.getByText(/^aberta$/)).toBeVisible({ timeout: 10_000 });

    // Ignorar.
    await row.getByRole('button', { name: /^ignorar$/i }).click();
    await expect(row.getByText(/^ignorada$/)).toBeVisible({ timeout: 10_000 });
  });
});
