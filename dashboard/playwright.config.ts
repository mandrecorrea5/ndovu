import { defineConfig, devices } from '@playwright/test';

/**
 * Config Playwright para os E2E do ndovu.
 *
 * Pressuposto: a stack Docker Compose está rodando ANTES do teste
 * (docker compose up -d na raiz do projeto), com:
 *   - API em http://localhost:18081
 *   - Dashboard em http://localhost:13000
 *   - ClickHouse, Postgres, NATS, MinIO como dependências
 *
 * Não usamos `webServer` do Playwright porque a stack já é gerenciada
 * externamente — os testes assumem que os serviços estão up.
 */
export default defineConfig({
  testDir: './e2e/tests',
  timeout: 30_000,
  expect: { timeout: 5_000 },
  fullyParallel: false, // seed compartilhado no mesmo backend — sequential por segurança
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'playwright-report' }]],
  use: {
    baseURL: 'http://localhost:13000',
    trace: 'retain-on-failure',
    video: 'retain-on-failure',
    screenshot: 'only-on-failure',
    // Sem storageState default — cada teste faz seu próprio login via helper.
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
