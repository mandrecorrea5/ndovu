import { test, expect } from '../fixtures/test';
import { loginAs, logout } from '../helpers/auth';
import { BOOTSTRAP_ADMIN } from '../helpers/api';
import { seedCompany, seedUser } from '../helpers/seed';

test.describe('reset de senha via admin', () => {
  test('admin usa atalho "senha" da tabela → gera → user faz login com nova senha', async ({
    page, api, cleanup,
  }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const target = await seedUser(api, c.resource.id, 'viewer', 'senha-original-123');
    cleanup.push(target.cleanup);

    // Admin abre a tela de users.
    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    await page.goto('/admin/users');

    // Localiza a linha do target e clica no botão "senha".
    const row = page.getByRole('row', { name: new RegExp(target.resource.email) });
    await row.getByRole('button', { name: /^senha$/i }).click();

    // Modal abre. Já vem com senha gerada (via generatePassword no setResetting).
    // Vou preencher senha específica pra saber o valor.
    const newPass = 'nova-senha-2026';
    const input = page.locator('input[autocomplete="new-password"]');
    await input.fill(newPass);

    await page.getByRole('button', { name: /resetar senha/i }).click();

    // Toast de sucesso.
    await expect(page.getByText(/atualizada com sucesso/i)).toBeVisible({ timeout: 5_000 });

    // Faz logout e testa nova senha.
    await logout(page);
    await loginAs(page, target.resource.email, newPass);
    await expect(page.getByRole('heading', { name: /vis[aã]o geral/i })).toBeVisible();

    // Senha antiga não deve funcionar mais.
    await logout(page);
    await page.goto('/login');
    await page.locator('#login-email').fill(target.resource.email);
    await page.locator('#login-pass').fill('senha-original-123');
    await page.getByRole('button', { name: /entrar/i }).click();
    await expect(page.getByRole('alert')).toBeVisible({ timeout: 5_000 });
  });

  test('botão "gerar" preenche uma senha aleatória de 12 chars', async ({
    page, api, cleanup,
  }) => {
    const c = await seedCompany(api);
    cleanup.push(c.cleanup);
    const target = await seedUser(api, c.resource.id, 'viewer');
    cleanup.push(target.cleanup);

    await loginAs(page, BOOTSTRAP_ADMIN.email, BOOTSTRAP_ADMIN.password);
    await page.goto('/admin/users');
    const row = page.getByRole('row', { name: new RegExp(target.resource.email) });
    await row.getByRole('button', { name: /^senha$/i }).click();

    // O modal já vem preenchido com senha gerada. Salvamos o valor.
    const input = page.locator('input[autocomplete="new-password"]');
    const initial = await input.inputValue();
    expect(initial.length).toBe(12);

    // Clicar em "gerar" deve gerar outra.
    await page.getByRole('button', { name: /^gerar$/i }).click();
    const second = await input.inputValue();
    expect(second.length).toBe(12);
    expect(second).not.toBe(initial);
  });
});
