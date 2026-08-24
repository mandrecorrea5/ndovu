# Ndovu — Plano de Testes

> **Público-alvo:** engenharia (backend, frontend, SRE) e tech lead.
> **Objetivo:** documentar a estratégia de testes automatizados para
> backend Go e frontend Next.js, com escopo, ferramentas, sprints e
> critérios mensuráveis. **Este documento é um plano — não execução.**
> Cada sprint precisa de aprovação para começar.
>
> Data desta versão: agosto/2026.

---

## Sumário

1. [Objetivo e princípios](#1-objetivo-e-princípios)
2. [Estado atual](#2-estado-atual)
3. [Estratégia — pirâmide de testes](#3-estratégia--pirâmide-de-testes)
4. [Metas mensuráveis](#4-metas-mensuráveis)
5. [Ferramentas escolhidas](#5-ferramentas-escolhidas)
6. [Sprint 1 — Domain + services críticos](#6-sprint-1--domain--services-críticos)
7. [Sprint 2 — HTTP handlers com fakes](#7-sprint-2--http-handlers-com-fakes)
8. [Sprint 3 — Adapters com testcontainers](#8-sprint-3--adapters-com-testcontainers)
9. [Sprint 4 — Config, platform, cmd](#9-sprint-4--config-platform-cmd)
10. [Sprint 5 — Playwright setup + auth](#10-sprint-5--playwright-setup--auth)
11. [Sprint 6 — Fluxos de negócio E2E](#11-sprint-6--fluxos-de-negócio-e2e)
12. [Sprint 7 — Regressão + robustez](#12-sprint-7--regressão--robustez)
13. [CI/CD](#13-cicd)
14. [Convenções e padrões](#14-convenções-e-padrões)
15. [Cronograma total](#15-cronograma-total)
16. [Critérios de aceite por sprint](#16-critérios-de-aceite-por-sprint)
17. [Manutenção contínua](#17-manutenção-contínua)

---

## 1. Objetivo e princípios

### Objetivo

Elevar o nível de confiança na entrega de features e refactors do ndovu
com testes automatizados que:

- **Rodam rápido** o suficiente pra serem executados a cada commit local.
- **Cobrem regressões críticas** — segurança, isolamento cross-tenant,
  ingestão, admin.
- **São mantidos junto com o código** — teste que quebra a build tem
  que ser fácil de arrumar.
- **Documentam o comportamento esperado** — servem de spec vivo.

### Princípios

1. **Sem 100% de cobertura.** Isso é ilusão. Cobrir o que importa.
2. **Cada teste justifica sua manutenção.** Se um teste quebra a cada
   refactor sem apontar bug real, mata.
3. **Preferir teste comportamental a teste estrutural.** Não teste
   "método X chamou método Y". Teste "quando o admin cria um user, o
   próximo login com essas credenciais funciona".
4. **Fixture pequena, teste específico.** Cada teste seta só o mínimo
   necessário; ownership de setup fica no teste.
5. **Falha clara, mensagem útil.** Assertions com mensagem que aponta
   pra causa. Preferir `t.Fatalf("X: esperado A, veio %v", got)` a
   `assert.Equal`.
6. **Nenhum teste flaky.** Retry silencioso é dívida. Se flakey, corrige
   ou remove.

---

## 2. Estado atual

Levantamento em agosto/2026.

### Backend Go — ~28% cobertura ponderada

| Package | Cobertura | Notas |
|---|---|---|
| `natsstream` | 63.8% | Bom — usa JetStream embedded |
| `domain` | 44% | Só `fingerprint_test.go` + parte de errors |
| `usecase` | 28.3% | 6 de 20 services testados (ingest, auth, anomaly, sampling, savedviews, app) |
| `httpapi` | 3.7% | Só middleware novo + 1 handler antigo |
| `platform` | 9.8% | Só mailer factory |
| `ctlpostgres` | 0% | Sem infra |
| `clickhouse` | 0% | Sem infra |
| `blobstore` | 0% | Sem infra |
| `config` | 0% | Sem infra |
| `cmd/api`, `cmd/writer` | 0% | Sem infra |

### Frontend — sem infra de teste

- **Zero** testes: sem Vitest, sem Jest, sem Playwright.
- 22 páginas Next.js + 11 componentes reutilizáveis.

### Superfície do projeto

- **79 handlers HTTP** em `httpapi`
- **119 métodos de service** em `usecase`
- **81 métodos de repository** em `ctlpostgres`
- **21 queries analíticas** em `clickhouse`
- **78 rotas HTTP** registradas
- **22 páginas** no dashboard

---

## 3. Estratégia — pirâmide de testes

```
                       ┌──────────────┐
                       │     E2E      │   ~15 testes Playwright
                       │  (Playwright)│   ~8 min de execução
                       └──────────────┘
                    ┌──────────────────┐
                    │   Integration    │   ~30 testes Go
                    │ (testcontainers) │   ~3 min de execução
                    └──────────────────┘
             ┌──────────────────────────────┐
             │           Unit               │   ~180 testes Go
             │      (só go test + fakes)    │   ~30s de execução
             └──────────────────────────────┘
```

- **Base larga (unit)**: barato, rápido, roda em todo commit.
- **Meio (integration)**: só ligações reais com infra (Postgres,
  ClickHouse, MinIO). Pega bugs de SQL, migrations, alias, transações.
- **Topo estreito (E2E)**: valida jornadas de negócio de ponta a ponta.
  Roda em CI pré-merge. Vídeo/trace em caso de falha.

**Sem tests "middle ground" duplicados.** Um mesmo comportamento é
testado em uma camada só — a mais barata que faça sentido.

---

## 4. Metas mensuráveis

### Backend (após 4 sprints)

| Package | Meta | Justificativa |
|---|---|---|
| `domain` | 80% | Regras puras, custo trivial |
| `usecase` | 70% | Lógica de negócio, alto valor |
| `httpapi` | 60% | Fluxos críticos + guards |
| `ctlpostgres` | 50% | Métodos que mudam estado + ownership |
| `clickhouse` | 40% | Writers + reads principais |
| `blobstore` | 60% | Só 2 métodos (put/get) |
| `platform` | 60% | Logger, mailer, metrics |
| `config` | 70% | Parse + defaults |
| `natsstream` | 63.8% | Manter (já bom) |
| **Agregado ponderado** | **65-70%** | — |

### Frontend (após 3 sprints)

- **12-15 specs Playwright** cobrindo os fluxos que representam 80%
  do valor de negócio (não faz sentido metrificar % de páginas — nem
  toda página precisa E2E).
- **Zero testes flakey** aceitos em `main`.
- **CI < 12 min** total (unit + integration + e2e).

### Não-metas

- **Sem** unit testing de React components (componentes são thin;
  E2E cobre o fluxo).
- **Sem** teste de storybook / visual regression (roadmap futuro se
  quisermos).
- **Sem** load test / stress test (fora do escopo — já tem `natsstream`
  testado com JetStream embedded).

---

## 5. Ferramentas escolhidas

### Backend

- **`go test`** + **`httptest`** — nativo, zero deps novas.
- **`testcontainers-go`** — sobe Postgres, ClickHouse, MinIO reais
  durante o teste. Já usado no projeto? Não — mas é padrão Go 1.20+.
- **`stretchr/testify`** — opcional, só para asserções complexas.
  Preferir asserções nativas (mais legíveis).
- **Build tags** — `//go:build integration` separa unit (rápido) de
  integration (lento).
- **`go tool cover`** — cobertura nativa. Report HTML pra CI.

### Frontend

- **Playwright** (`@playwright/test`) — pedido explícito do usuário.
  Padrão de mercado para E2E JavaScript, melhor DX que Cypress hoje.
- **Docker Compose** — a stack completa sobe antes dos testes (fiel
  a produção, testa build real do Next.js).
- **Trace + screenshot on failure** — trace zip anexado ao CI em caso
  de falha, permite reprodução exata.

### CI/CD

- **GitHub Actions** ou **GitLab CI** — depende do host. Ndovu está
  em `git.gmz.corp` (Gitea + hipotético Woodpecker/Drone). A ser
  confirmado. Templates prontos pra ambos.

---

## 6. Sprint 1 — Domain + services críticos

**Duração:** 3-4 dias
**Meta:** `domain` → 80%, `usecase` → 60%

### Arquivos a criar/expandir

**Domain (novos):**

- `backend/internal/domain/controlplane_test.go`
  - `Role.Valid()`, `Role.AtLeastEditor()`
  - `FeedbackType.Valid()`, `FeedbackStatus.Valid()`
  - `AlertChannel.Valid()`
  - `AnomalyMetric.Valid()`, `AnomalyDirection.Valid()`

- `backend/internal/domain/errors_test.go`
  - `ValidationError.Error()` formata múltiplos issues
  - `errors.Is/As` com `ErrNotFound`, `ErrUnauthorized`, `ErrConflict`

- `backend/internal/domain/event_test.go`
  - Defaults de EventType

**Usecase (novos):**

- `backend/internal/usecase/permissions_test.go`
  - Grant/Revoke idempotência
  - Grant com role inválido rejeita
  - ListForUser devolve vazio quando não há grant
  - `ListAppNamesForUser` filtra por company (via fake)

- `backend/internal/usecase/apikeys_test.go`
  - CreateKey gera prefix + hash bcrypt
  - Cache com TTL 30s (mock clock)
  - RevokeKey invalida cache imediatamente
  - Rate limit por chave

- `backend/internal/usecase/feedback_test.go`
  - Ingest valida mensagem obrigatória, max 5000 chars
  - SetStatus rejeita status inválido
  - Ownership: filtro por SessionID

- `backend/internal/usecase/issues_test.go`
  - Fingerprint agrupa erros idênticos
  - Status transitions (open→investigating→resolved)
  - Regressão automática após release

- `backend/internal/usecase/retention_test.go`
  - Cálculo cohort D1/D7/D14/D30 com dados fake
  - Cohorts vazios devolvem array vazio (regressão do bug de hoje)

- `backend/internal/usecase/funnels_test.go`
  - Steps em janela devolvem conversão correta
  - Sequência fora de ordem não conta
  - Janela expirada não conta

- `backend/internal/usecase/query_test.go`
  - AppScope enforcement em `Events`, `Sessions`, `Overview`
  - Fallback de session via feedback quando ClickHouse não tem
  - Session vazia (só feedback) devolve shell + timeline vazia

**Usecase (expandir existentes):**

- `savedviews_test.go` — adicionar teste de ownership em Update/Delete
- `app_test.go` — adicionar `GetByName`, `ListByCompany`
- `auth_test.go` — adicionar `ListUsersByCompany`, ownership em UpdateUser

### Padrão de teste

```go
// backend/internal/usecase/permissions_test.go
package usecase

import (
    "context"
    "testing"

    "github.com/marcoscorrea/ndovu/backend/internal/domain"
)

func TestPermissions_GrantIdempotente(t *testing.T) {
    store := newFakePermissionStore()
    svc := NewPermissionService(store)

    // Primeiro grant
    _, err := svc.Grant(ctx, "u-1", "app-1", "viewer", "actor-1")
    if err != nil {
        t.Fatalf("primeiro grant falhou: %v", err)
    }

    // Segundo grant idêntico não deve falhar
    _, err = svc.Grant(ctx, "u-1", "app-1", "viewer", "actor-1")
    if err != nil {
        t.Fatalf("grant idempotente falhou: %v", err)
    }

    // Só um registro persistido
    list, _ := svc.ListForUser(ctx, "u-1")
    if len(list) != 1 {
        t.Fatalf("esperava 1 grant, veio %d", len(list))
    }
}
```

### Critério de aceite Sprint 1

- [ ] `go test ./internal/domain/... ./internal/usecase/... -cover` ≥ 60% cada
- [ ] Todos os testes rodam em < 5s combinados
- [ ] Zero uso de infra externa (sem Docker)
- [ ] Fakes localizados em `*_test.go` (não vazam pra produção)

---

## 7. Sprint 2 — HTTP handlers com fakes

**Duração:** 3-4 dias
**Meta:** `httpapi` → 60%

### Arquivos a criar

**Fixture central:**

- `backend/internal/adapter/httpapi/testfixture.go` (build tag test)
  - `newTestHandlers(t)` — devolve `*Handlers` com todos services
    fakes já injetados
  - `authenticateAs(t, role, companyID)` — retorna Bearer token pra
    injetar em requests
  - `newTestRequest(method, path, body)` — helper pra montar
    `*http.Request` com header + context

**Testes por conjunto de rotas:**

- `auth_handlers_test.go`
  - POST /v1/auth/login OK
  - POST /v1/auth/login senha errada → 401
  - POST /v1/auth/login user inativo → 401
  - GET /v1/auth/me sem token → 401
  - GET /v1/auth/me com token expirado → 401
  - GET /v1/auth/me OK devolve identity

- `ingest_handlers_test.go`
  - POST /v1/events 202 happy path
  - Chave inválida → 401
  - Cross-tenant guard (`envelope.app != key.app`) → 400
  - Payload > 1MB → 413
  - Contrato violado devolve `details[]` úteis
  - Rate limit por chave → 429 com `Retry-After`

- `admin_users_test.go`
  - GET /v1/admin/users como super vê tudo
  - GET /v1/admin/users como admin de company só vê users da company
  - POST /v1/admin/users força CompanyID = actor pra não-super
  - PATCH bloqueia cross-tenant → 403
  - PATCH aceita password → chama ResetPassword
  - Não permite desativar último admin ativo → 409

- `admin_apps_test.go`, `admin_keys_test.go`, `admin_companies_test.go`
  - Matriz **super/admin/editor/viewer × GET/POST/PATCH/DELETE**
  - Ownership em cada escrita

- `admin_extended_test.go`
  - Alerts, anomaly, sampling, source-maps, feedbacks
  - Filtragem por scope na listagem
  - `enforceAppInScope` bloqueia escrita fora do scope

- `admin_gdpr_test.go`
  - Não-super → 403 em export e delete

- `query_handlers_test.go`
  - Paginação keyset
  - Filtros combinados
  - `tenantScope` enforcement

### Padrão de teste

```go
// backend/internal/adapter/httpapi/admin_users_test.go
func TestAdminUsers_AdminNaoSuperVeSoOsSeus(t *testing.T) {
    h := newTestHandlers(t)
    h.auth.SeedUser(t, "A", "admin-a@x", "admin", "company-A")
    h.auth.SeedUser(t, "B", "admin-b@x", "admin", "company-B")

    req := h.newTestRequest("GET", "/v1/admin/users", nil)
    req = h.authenticateAs(req, "admin", "company-A", false)

    rr := httptest.NewRecorder()
    h.Router.ServeHTTP(rr, req)

    if rr.Code != 200 {
        t.Fatalf("esperava 200, veio %d: %s", rr.Code, rr.Body.String())
    }

    var got struct{ Users []struct{ Email string } }
    _ = json.Unmarshal(rr.Body.Bytes(), &got)

    if len(got.Users) != 1 || got.Users[0].Email != "admin-a@x" {
        t.Fatalf("esperava só admin-a@x, veio %+v", got.Users)
    }
}
```

### Critério de aceite Sprint 2

- [ ] `go test ./internal/adapter/httpapi/... -cover` ≥ 60%
- [ ] Cobertura dos guards de segurança (`enforceOwnership`,
      `enforceAppInScope`, `requireRole`, `requireAnyRole`) = 100%
- [ ] Testes rodam em < 10s
- [ ] Sem Docker

---

## 8. Sprint 3 — Adapters com testcontainers

**Duração:** 3-4 dias
**Meta:** `ctlpostgres` → 50%, `clickhouse` → 40%, `blobstore` → 60%

### Setup

- Adicionar `testcontainers-go` no `go.mod`
- Criar `backend/internal/adapter/testenv/testenv.go`:
  - `StartPostgres(t) *pgxpool.Pool` — sobe Postgres, roda migrations
  - `StartClickHouse(t) driver.Conn` — sobe ClickHouse, aplica schema
  - `StartMinIO(t) *minio.Client` — sobe MinIO, cria bucket
- Cada helper usa `t.Cleanup()` pra derrubar container após o teste

**Build tag:** `//go:build integration` em todos os testes deste sprint.

### Arquivos a criar

**Postgres (`ctlpostgres_test.go` + arquivos por store):**

- `users_test.go`
  - Migrations aplicam sem erro
  - CreateUser + GetUserByEmail
  - UNIQUE email → ErrConflict
  - CountActiveAdmins protege último admin
  - ListUsersByCompany filtra corretamente

- `apps_test.go`
  - CRUD completo
  - ListAppNamesByCompany devolve exatamente os da company
  - GetAppByName resolve OK / ErrNotFound

- `apikeys_test.go`
  - CreateAPIKey + FindActiveKeyByHash
  - RevokeAPIKey desativa
  - ListAPIKeysByCompany respeita JOIN

- `feedbacks_test.go`
  - CRUD + filtros (App, Status, SessionID)
  - GetFeedback

- `savedviews_test.go`, `funnels_test.go`, `alerts_test.go`,
  `anomaly_test.go`, `sampling_test.go`, `sourcemaps_test.go`
  - CRUD + Get* + ownership

- `audit_test.go`
  - Insert-only (não expõe update/delete)
  - Filtro por company do actor (JOIN users)

**ClickHouse (`clickhouse_test.go`):**

- `schema_test.go`
  - `EnsureSchema` idempotente
  - Storage policy `tiered` aplicada

- `writer_test.go`
  - Bulk insert 1000 eventos
  - Dedup por eventId em merges

- `reader_test.go`
  - FindEvents com todos os filtros (app, user, session, type, feature,
    status range, only errors, search full-text)
  - FindSessions **sem colidir alias** (regressão do bug de hoje)
  - GetSession + SessionTimeline
  - GetOverview devolve buckets + top routes + top errors
  - Retention devolve cohorts (regressão do bug retention:null)
  - FindReleases, FindWebVitals, RunFunnel, TraceTimeline

**Blobstore (`blobstore_test.go`):**

- PutSnapshot + GetSnapshot com bytes iguais
- GetSnapshot com key inexistente → erro
- Snapshot grande (5MB) OK

### Padrão de teste

```go
//go:build integration

package ctlpostgres_test

import (
    "context"
    "testing"

    "github.com/marcoscorrea/ndovu/backend/internal/adapter/ctlpostgres"
    "github.com/marcoscorrea/ndovu/backend/internal/adapter/testenv"
    "github.com/marcoscorrea/ndovu/backend/internal/domain"
)

func TestUsers_CountActiveAdminsProtegeUltimo(t *testing.T) {
    pool := testenv.StartPostgres(t)
    repo := ctlpostgres.NewFromPool(pool)

    ctx := context.Background()
    _, _ = repo.CreateUser(ctx, domain.User{
        Email: "a@x", Name: "A", Role: domain.RoleAdmin,
        Active: true, CompanyID: seedCompany(t, pool),
    }, "hash1")

    n, _ := repo.CountActiveAdmins(ctx)
    if n != 1 {
        t.Fatalf("esperava 1 admin, veio %d", n)
    }
}
```

### Critério de aceite Sprint 3

- [ ] `go test -tags=integration ./...` roda em < 3 min
- [ ] Cobertura de `ctlpostgres` ≥ 50%, `clickhouse` ≥ 40%
- [ ] Testcontainers sobe e derruba corretamente (sem containers
      órfãos após rodar)
- [ ] Testes rodam em CI (Docker-in-Docker disponível)

---

## 9. Sprint 4 — Config, platform, cmd

**Duração:** 1-2 dias
**Meta:** `config` → 70%, `platform` → 60%

### Arquivos a criar

- `config_test.go`
  - Load com env vars vazias usa defaults
  - Load com env vars válidas parseia
  - Validação de ranges (porta, TTL)
  - `NDOVU_MAILER_PROVIDER=auto` decide baseado em SendGrid/SMTP

- `platform/logger_test.go`
  - Níveis: debug/info/warn/error
  - JSON structured em prod
  - Texto colorido em dev

- `platform/metrics_test.go`
  - Histogram de latência
  - Counter incrementa
  - Render devolve formato Prometheus válido

- `platform/mailer_smtp_test.go` (opcional, com MailHog testcontainer)
  - Send OK, Send com host inválido devolve erro

### Critério de aceite Sprint 4

- [ ] `config` ≥ 70%, `platform` ≥ 60%
- [ ] Testes rodam em < 2s

---

## 10. Sprint 5 — Playwright setup + auth

**Duração:** 2 dias

### Setup

- Adicionar `@playwright/test` como devDependency no `dashboard/`
- Criar estrutura:
  ```
  dashboard/e2e/
    playwright.config.ts
    fixtures/
      test.ts              # fixture com apiClient + cleanup
    helpers/
      seed.ts              # seedCompany, seedApp, seedUser, seedKey
      auth.ts              # loginAs(page, email, password)
      wait.ts              # waitForApi(), waitForToast()
    tests/
      auth.spec.ts
      smoke.spec.ts
      ingest-and-view.spec.ts
  ```

- `playwright.config.ts`:
  - baseURL: `http://localhost:13000`
  - `webServer`: comando `docker compose up -d && wait-for-health`
    (opcional — pode ser manual antes do CI)
  - `reporter: [['list'], ['html', { open: 'never' }]]`
  - `trace: 'retain-on-failure'`
  - `video: 'retain-on-failure'`
  - `screenshot: 'only-on-failure'`
  - workers: 1 (evitar disputas por seed)

### Fixture com cleanup

```typescript
// dashboard/e2e/fixtures/test.ts
import { test as base, expect } from '@playwright/test';

type ApiClient = {
  createCompany(name: string): Promise<{ id: string; name: string }>;
  createApp(companyId: string, name: string): Promise<{ id: string; key: string }>;
  createUser(companyId: string, email: string, password: string, role: string): Promise<{ id: string }>;
};

type Fixtures = {
  api: ApiClient;
  cleanup: string[]; // resource paths para DELETE no afterEach
};

export const test = base.extend<Fixtures>({
  api: async ({ request }, use) => {
    // login como super-admin
    const login = await request.post('http://localhost:18081/v1/auth/login', {
      data: { email: 'admin@ndovu.local', password: 'admin12345' },
    });
    const { token } = await login.json();
    // ... implementação com token bearer
    await use(client);
  },
  cleanup: async ({ api }, use) => {
    const paths: string[] = [];
    await use(paths);
    // Cleanup depois do teste
    for (const p of paths.reverse()) {
      await api.delete(p);
    }
  },
});

export { expect };
```

### Testes iniciais

**`auth.spec.ts`:**

```typescript
import { test, expect } from '../fixtures/test';

test('login com credenciais válidas leva ao dashboard', async ({ page }) => {
  await page.goto('/login');
  await page.getByLabel('email').fill('admin@ndovu.local');
  await page.getByLabel('senha').fill('admin12345');
  await page.getByRole('button', { name: 'entrar' }).click();
  await expect(page).toHaveURL('/');
  await expect(page.getByText('Visão geral')).toBeVisible();
});

test('senha errada mostra erro', async ({ page }) => {
  await page.goto('/login');
  await page.getByLabel('email').fill('admin@ndovu.local');
  await page.getByLabel('senha').fill('errada');
  await page.getByRole('button', { name: 'entrar' }).click();
  await expect(page.getByText(/inválid|401/i)).toBeVisible();
  await expect(page).toHaveURL('/login');
});

test('sessão persiste após reload', async ({ page }) => {
  await loginAs(page, 'admin@ndovu.local', 'admin12345');
  await page.reload();
  await expect(page).toHaveURL('/');
});

test('logout limpa sessão', async ({ page }) => {
  await loginAs(page, 'admin@ndovu.local', 'admin12345');
  await page.getByRole('button', { name: /sair|logout/i }).click();
  await expect(page).toHaveURL('/login');
});
```

### Critério de aceite Sprint 5

- [ ] `npx playwright test` roda os 3 primeiros specs em < 60s
- [ ] Trace + screenshot gerados em caso de falha
- [ ] Cleanup automático (banco não acumula lixo entre runs)

---

## 11. Sprint 6 — Fluxos de negócio E2E

**Duração:** 3-4 dias
**Meta:** 12-15 specs cobrindo jornadas críticas

### Specs a implementar

1. **`admin-crud.spec.ts`** — cria company → app → chave → user editor
   → login como user → user vê os apps corretos
2. **`rbac-visibility.spec.ts`** — viewer não vê menu admin; editor vê
   botão "novo funil"; viewer não vê
3. **`cross-tenant-isolation.spec.ts`** — cria 2 companies com dados;
   admin A não vê nada de B em qualquer tela admin
4. **`password-reset.spec.ts`** — admin usa atalho "senha" → gera →
   copia → clipboard tem valor → user faz login com nova senha
5. **`n-apps-scope.spec.ts`** — editor sem grant vê ambos apps da
   company; com grant só em um vê apenas aquele; ao revogar volta
6. **`funnels.spec.ts`** — cria funil (2 steps) → executa → vê
   conversão calculada
7. **`feedback-widget.spec.ts`** — cliente envia feedback via API →
   admin vê em `/admin/feedbacks` → clica "resolver" → status muda
8. **`session-replay.spec.ts`** — SDK envia error + snapshot → admin
   abre issue → vê iframe com HTML sanitizado
9. **`issue-triage.spec.ts`** — editor muda status de issue → adiciona
   comentário → outro editor vê comentário
10. **`saved-views.spec.ts`** — viewer cria view privada; editor cria
    view compartilhada; viewer vê view compartilhada mas não pode
    editar
11. **`anomaly-detection.spec.ts`** — cria regra → ingere burst de
    erros → aparece em detecções
12. **`sampling.spec.ts`** — cria regra `sample_rate: 0.5` → ingere
    100 eventos → grava aproximadamente 50 (assert com tolerância)
13. **`api-keys-rotation.spec.ts`** — cria chave → revoga → SDK com
    chave antiga passa a receber 401 (após TTL de 30s do cache)

### Padrão

Cada teste:
- Cria dados via API (não via UI — muito mais rápido)
- Faz assertions via UI (isso é o que estamos testando)
- Cleanup no `afterEach`

```typescript
test('editor com grant só em app-1 vê apenas app-1', async ({ page, api, cleanup }) => {
  // Setup
  const company = await api.createCompany('TesteRBAC');
  cleanup.push(`/v1/admin/companies/${company.id}`);

  const app1 = await api.createApp(company.id, 'app-1');
  const app2 = await api.createApp(company.id, 'app-2');
  cleanup.push(`/v1/admin/apps/${app1.id}`, `/v1/admin/apps/${app2.id}`);

  const editor = await api.createUser(company.id, 'ed@x', 'senha1234', 'editor');
  cleanup.push(`/v1/admin/users/${editor.id}`);

  await api.grantPermission(editor.id, app1.id, 'editor');

  // Ingere 1 evento em cada app
  await api.ingestEvent(app1.key, app1.name);
  await api.ingestEvent(app2.key, app2.name);
  await page.waitForTimeout(2000); // deixa o writer persistir

  // UI: editor faz login e vai em /traces
  await loginAs(page, 'ed@x', 'senha1234');
  await page.goto('/traces');

  // Assert: só app-1 aparece
  await expect(page.locator('[data-testid="event-row"]')).toHaveCount(1);
  await expect(page.getByText('app-1')).toBeVisible();
  await expect(page.getByText('app-2')).not.toBeVisible();
});
```

### Critério de aceite Sprint 6

- [ ] 13 specs implementados e verdes
- [ ] Execução completa em < 6 min
- [ ] Zero flaky em 10 runs consecutivos
- [ ] Cada spec limpa o que criou

---

## 12. Sprint 7 — Regressão + robustez

**Duração:** 1-2 dias

### Testes de regressão dos bugs corrigidos

- `retention-empty.spec.ts` — página `/retention` carrega sem
  quebrar quando não há dados (bug do `cohorts: null`)
- `sessions-cross-tenant.spec.ts` — `/sessions` retorna 200 pra admin
  não-super (bug do alias `any(app) AS app`)
- `session-fallback.spec.ts` — `/sessions/{id}` de feedback-only
  mostra shell com "sessão sem eventos capturados"
- `favicon.spec.ts` — request pra `/favicon.ico` ou `/icon.svg` 200

### Robustez

- Configurar `retries: 2` em CI (0 em local)
- Adicionar `test.slow()` em testes que dependem de sampling/anomaly
  (janelas de tempo)
- Adicionar helper `waitForWriter()` que polla o ClickHouse até ver o
  evento (evita `page.waitForTimeout` mágico)

### Critério de aceite Sprint 7

- [ ] Todos os bugs corrigidos hoje têm teste de regressão
- [ ] Zero `page.waitForTimeout(N)` sem justificativa — só waits
      explícitos
- [ ] Suite total (unit + integration + e2e) roda em < 12 min em CI

---

## 13. CI/CD

### Workflow proposto (independente do host)

```yaml
# Pseudo-yaml (adaptar pra GitHub Actions ou Gitea Actions/Woodpecker)
jobs:
  backend-unit:
    steps:
      - checkout
      - setup-go 1.25
      - run: go test -race -coverprofile=cov.out ./...
      - run: go tool cover -func=cov.out
      - upload-artifact: coverage.html

  backend-integration:
    needs: backend-unit
    services:
      docker: {enabled: true}
    steps:
      - checkout
      - setup-go 1.25
      - run: go test -race -tags=integration -timeout=5m ./...

  e2e:
    needs: [backend-unit]
    steps:
      - checkout
      - run: docker compose up -d
      - run: ./scripts/wait-for-health.sh
      - setup-node 20
      - run: cd dashboard && npm ci
      - run: cd dashboard && npx playwright install --with-deps chromium
      - run: cd dashboard && npx playwright test
      - upload-artifact: dashboard/playwright-report/ (if failure)
      - upload-artifact: dashboard/test-results/ (if failure)
      - run: docker compose down -v
```

### Cobertura tracking

- Fazer upload do `cov.out` para um badge no README (Codecov, Coveralls,
  ou simples `go tool cover` publicado como HTML no artifact).
- Alerta se cobertura cai > 5% em um PR.

---

## 14. Convenções e padrões

### Nomeação

- **Backend:**
  - `TestX_ComportamentoEsperado_QuandoCondicao` — em português.
    Ex.: `TestPatchUser_Rejeita_QuandoCrossTenant`
  - Fakes em `test_*.go` (mesmo package, build tag test)

- **Frontend:**
  - `nome-do-fluxo.spec.ts` — kebab-case
  - `test('descrição do comportamento', async () => {...})`

### Estrutura de um teste

**Arrange → Act → Assert** com blocos separados por linha em branco:

```go
func TestX(t *testing.T) {
    // Arrange
    store := newFakeStore()
    svc := NewService(store)

    // Act
    got, err := svc.DoThing(ctx, "input")

    // Assert
    if err != nil {
        t.Fatalf("esperava sucesso, veio: %v", err)
    }
    if got.Field != "expected" {
        t.Fatalf("field: esperado 'expected', veio %q", got.Field)
    }
}
```

### Fakes

- Fakes vivem em `*_test.go` do mesmo package.
- Nomeados `fakeXStore` (minúsculo — não exportado).
- Implementam a interface completa; métodos não usados devolvem
  zero value ou panic com mensagem clara.

### Marcadores

- **Unit:** default (sem tag)
- **Integration:** `//go:build integration`
- **Slow (>10s):** `t.Skip()` se `testing.Short()`

### Data attributes no frontend

Adicionar `data-testid` nos elementos usados por E2E — evita quebrar
quando muda texto ou class:

```tsx
<button data-testid="btn-novo-funil">Novo funil</button>
```

Já usar essa convenção em componentes NOVOS a partir do Sprint 5.
Retrofit em componentes existentes sob demanda (quando um teste
precisar).

---

## 15. Cronograma total

| Sprint | Foco | Duração | Cumulativo |
|---|---|---|---|
| 1 | Domain + services críticos | 3-4 dias | 3-4 dias |
| 2 | HTTP handlers | 3-4 dias | 6-8 dias |
| 3 | Adapters + testcontainers | 3-4 dias | 9-12 dias |
| 4 | Config + platform + cmd | 1-2 dias | 10-14 dias |
| 5 | Playwright setup + auth | 2 dias | 12-16 dias |
| 6 | Fluxos de negócio E2E | 3-4 dias | 15-20 dias |
| 7 | Regressão + robustez | 1-2 dias | 16-22 dias |

**Total: ~3 semanas** de trabalho focado (1 pessoa full-time).

Pode paralelizar backend + frontend a partir do Sprint 5 se houver
2 pessoas — cortaria pra ~2 semanas.

---

## 16. Critérios de aceite por sprint

Cada sprint entrega:

1. **Código de teste** — verde localmente.
2. **Documentação de execução** — README do pacote com "como rodar".
3. **CI passando** — pipeline atualizada.
4. **Cobertura atingida** — meta específica do sprint.
5. **Revisão** — código passa em code review antes de merge.

Sprint só é considerado "done" quando **todos** os 5 itens estão
verdes.

---

## 17. Manutenção contínua

Depois dos 7 sprints, testes viram parte do processo normal:

- **Nova feature exige novo teste.** Se PR não tem teste, code review
  bloqueia.
- **Bug fix exige teste de regressão.** Antes de corrigir, escrever
  teste que reproduz. Depois de corrigir, teste passa.
- **Refactor não deve exigir mudança de teste** (se exigir, é sinal
  de acoplamento entre teste e implementação — sniff no teste).
- **Flaky is dead.** Teste que quebra intermitentemente é removido
  (não silenciado com `test.retry`).
- **Trimestralmente:** revisar cobertura, remover testes que só
  medem estrutura sem valor comportamental.

---

## Dependências novas que este plano adiciona

### Backend

```
go get github.com/testcontainers/testcontainers-go
go get github.com/testcontainers/testcontainers-go/modules/postgres
go get github.com/testcontainers/testcontainers-go/modules/clickhouse
go get github.com/testcontainers/testcontainers-go/modules/minio
```

### Frontend

```
cd dashboard
npm install -D @playwright/test
npx playwright install --with-deps chromium
```

---

## Riscos e mitigações

| Risco | Mitigação |
|---|---|
| Testcontainers lento em CI local (Mac) | Rodar `//go:build integration` só em CI pré-merge, não em cada commit local |
| Playwright flakey por timing | Sempre usar `page.waitForResponse` / `expect().toBeVisible()`, nunca `waitForTimeout(N)` |
| CI custoso (Docker-in-Docker) | Usar runner com Docker nativo (GitHub Actions ubuntu-latest já tem) |
| Fixtures acumulam lixo no banco | Cleanup no `afterEach`; reset diário do banco de teste como safety net |
| Testes de anomaly/sampling dependem de janela de tempo | Marker `test.slow()`; ou expor endpoint admin "trigger-once" só em ambiente de teste |
| Cobertura vira métrica de vaidade | Focar em "testes que apontam bug real" nas revisões, não em % |

---

## Próximos passos (aguardando aprovação)

1. **Aprovar este plano** — sprints, ferramentas, metas.
2. **Confirmar CI host** — GitHub Actions, Gitea Actions ou outro?
3. **Autorizar Sprint 1** — começo pelo backend fundação.

Cada sprint executado terá um commit próprio + PR próprio, com
descrição do que foi coberto + delta de cobertura.

---

Documento vivo. Última atualização: agosto/2026.
