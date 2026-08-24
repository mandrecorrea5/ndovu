import { test, expect } from '../fixtures/test';
import { loginAs } from '../helpers/auth';
import { BOOTSTRAP_ADMIN } from '../helpers/api';
import { seedApp, seedCompany } from '../helpers/seed';

// Rotação zero-downtime: admin cria segunda chave, valida que ambas
// ingerem em paralelo (janela de transição), revoga a antiga pela UI,
// confirma que antiga passa a rejeitar mas nova continua ok.
// RevokeKey no backend invalida o cache imediatamente — o teste não
// depende de esperar TTL expirar.

async function ingest(
  request: import('@playwright/test').APIRequestContext,
  appName: string,
  apiKey: string,
): Promise<number> {
  const resp = await request.post('http://localhost:18081/v1/events', {
    headers: { 'X-Api-Key': apiKey },
    data: {
      app: appName,
      session: { sessionId: crypto.randomUUID(), userId: 'u-rot' },
      events: [{
        eventId: crypto.randomUUID(),
        type: 'action',
        name: 'rot-probe',
        timestamp: new Date().toISOString(),
      }],
    },
  });
  return resp.status();
}

test.describe('rotação de API keys — zero downtime', () => {
  test('cria segunda chave via UI, ambas ingerem, revoga a antiga e ela para', async ({
    page, api, cleanup, request,
  }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const app = await seedApp(api, c.resource.id);
    cleanup.push(app.cleanup);
    const key1 = app.resource.key;

    // Baseline: key inicial funciona.
    expect(await ingest(request, app.resource.name, key1)).toBe(202);

    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    await page.goto('/admin/keys');

    // Cria segunda chave via modal.
    await page.getByRole('button', { name: /nova chave/i }).click();
    await page.locator('#key-app').selectOption(app.resource.name);
    await page.locator('#key-label').fill('rotation-e2e');
    await page.getByRole('button', { name: /gerar chave/i }).click();

    // Banner "chave em claro" — extrai o valor do <code>. É a única chance
    // de ler a chave (backend guarda só hash).
    const codeEl = page.locator('code.mono', { hasText: /^ndv_|^[A-Za-z0-9_-]{16,}/ }).first();
    await expect(codeEl).toBeVisible({ timeout: 10_000 });
    const key2 = (await codeEl.textContent())?.trim() ?? '';
    expect(key2.length).toBeGreaterThan(10);
    expect(key2).not.toBe(key1);

    // Ambas ativas: rotação em andamento.
    expect(await ingest(request, app.resource.name, key1)).toBe(202);
    expect(await ingest(request, app.resource.name, key2)).toBe(202);

    // Revoga a key1 clicando na linha correta (prefixo dela).
    // O botão "fechar" o banner libera o layout da tabela abaixo.
    await page.getByRole('button', { name: /^fechar$/i }).click();

    // Cada chave tem prefix visível de 8 chars (na coluna "Prefixo"). Uso o
    // início do plaintext pra achar a linha da key1 sem ambiguidade — o
    // backend expõe os primeiros N chars.
    const key1Prefix = key1.slice(0, 8);
    const row1 = page.getByRole('row').filter({ hasText: key1Prefix });
    await expect(row1).toBeVisible();
    await row1.getByRole('button', { name: /^revogar$/i }).click();

    // Aguarda a linha virar "revogada" (sem botão revogar mais).
    await expect(row1.getByText(/○ revogada/)).toBeVisible({ timeout: 10_000 });

    // key1 agora rejeita, key2 continua aceitando.
    expect(await ingest(request, app.resource.name, key1)).toBe(401);
    expect(await ingest(request, app.resource.name, key2)).toBe(202);
  });
});
