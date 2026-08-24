// Fixture central: estende `test` do Playwright com:
//   - `api`: APIClient logado como super-admin bootstrap
//   - `cleanup`: array de funções pra rodar no afterEach (LIFO)
//
// Uso:
//   test('meu teste', async ({ page, api, cleanup }) => {
//     const { resource, cleanup: cUndo } = await seedCompany(api);
//     cleanup.push(cUndo);
//     await page.goto('/');
//     ...
//   });

import { test as base, expect } from '@playwright/test';
import { APIClient, newAdminClient } from '../helpers/api';
import type { CleanupFn } from '../helpers/seed';

type Fixtures = {
  api: APIClient;
  cleanup: CleanupFn[];
};

export const test = base.extend<Fixtures>({
  api: async ({ request }, use) => {
    const client = await newAdminClient(request);
    await use(client);
  },

  cleanup: async ({}, use) => {
    const fns: CleanupFn[] = [];
    await use(fns);
    // LIFO: apaga user antes de app, app antes de company (ordem de FK).
    for (let i = fns.length - 1; i >= 0; i--) {
      try {
        await fns[i]();
      } catch (e) {
        // best effort — não deixa uma falha de cleanup mascarar erro real.
        console.warn(`cleanup ${i} falhou:`, e);
      }
    }
  },
});

export { expect };
