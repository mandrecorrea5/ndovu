import { test, expect } from '../fixtures/test';
import { loginAs } from '../helpers/auth';
import { BOOTSTRAP_ADMIN } from '../helpers/api';
import { seedCompany, seedApp, ingestEvent, unique } from '../helpers/seed';

test.describe('ingest → view (jornada happy path)', () => {
  test('evento ingerido via SDK aparece no explorador de traces', async ({
    page,
    api,
    cleanup,
    request,
  }) => {
    // Setup: company + app + chave via API.
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const a = await seedApp(api, c.resource.id);
    cleanup.push(a.cleanup);
    const app = a.resource;

    // Ingerir evento com nome único (pra encontrar depois na UI).
    const evName = unique('e2e-action');
    await ingestEvent(request, app.name, app.key, evName);

    // Login como super admin e navegar até /traces filtrando pelo app.
    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);

    // Espera o writer persistir no ClickHouse. Em vez de sleep mágico,
    // polla a API até ver o evento (rápido — writer costuma < 2s).
    await expect
      .poll(
        async () => {
          const res = await api.get(
            `/v1/events?app=${encodeURIComponent(app.name)}&limit=50`,
          );
          const events = res.events ?? [];
          return events.some((e: { name: string }) => e.name === evName);
        },
        {
          message: `evento ${evName} não apareceu na API depois de 15s`,
          timeout: 15_000,
          intervals: [500, 1000, 2000],
        },
      )
      .toBeTruthy();

    // Agora que a API já vê o evento, abre o explorador.
    await page.goto(`/traces?app=${encodeURIComponent(app.name)}`);
    // O evento aparece na tabela.
    await expect(page.getByText(evName).first()).toBeVisible({ timeout: 10_000 });
  });
});
