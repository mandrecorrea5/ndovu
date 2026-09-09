# ndovu-dashboard

Backoffice web do Ndovu. Next.js 15 (App Router) em modo `standalone`,
consumindo exclusivamente a API do `ndovu-backend`.

> Este README é o modelo para o root do repositório `ndovu-dashboard`.
> Origem: `dashboard/` do monorepo Ndovu.

## O que este repositório é

A interface de quem investiga um problema: login, visão geral, explorador de
eventos, rastro completo de uma sessão, issues, releases, performance, funis,
retenção e as telas administrativas (usuários, apps, empresas, chaves de API,
alertas, sampling, anomalias, source maps, GDPR, auditoria, feedbacks).

Não tem estado próprio e não fala com banco algum: **toda leitura e escrita
passa pela API**, autenticada por JWT (`Authorization: Bearer`). Isso o torna
um workload trivialmente escalável e descartável.

## Estrutura

```
src/app/          rotas do App Router (login, traces, sessions, issues,
                  releases, performance, funnels, retention, admin)
src/components/   componentes compartilhados
src/lib/          cliente HTTP da API, hooks de TanStack Query, formatação
e2e/              specs Playwright (auth, smoke, ingest→view, RBAC, tenant)
Dockerfile        multi-stage → node:22-alpine, output standalone
```

Stack: React 19, TanStack Query, Recharts, Tailwind, TypeScript.

## Desenvolvimento local

```bash
npm install
NEXT_PUBLIC_NDOVU_API=http://localhost:18081 npm run dev   # :3000
```

Precisa de uma API no ar — suba o `dev/compose.yml` do `ndovu-backend`, ou
aponte para o dashboard de dev do cluster.

```bash
npm run typecheck
npx playwright test        # E2E; exige stack completa
```

## Build da imagem

```bash
docker build --build-arg NEXT_PUBLIC_NDOVU_API=https://api-ndovu-dev.apps.<cluster> \
             -t ndovu-dashboard:$(git rev-parse --short HEAD)-dev .
```

## ⚠️ A configuração é build-time — e isso tem consequência

`NEXT_PUBLIC_NDOVU_API` é um `ARG` do Dockerfile, embutido no bundle
JavaScript durante `npm run build`. Duas implicações que atravessam todo o
processo de deploy:

1. **A imagem é específica de um ambiente.** A tag é `<sha>-<env>`, não `<sha>`.
2. **Não existe promoção hml→prd por digest.** Promover exige rebuild com a
   URL de produção — ou seja, o binário testado em homologação não é
   bit-a-bit o que vai para produção.

É a única quebra do princípio "mesma imagem em todos os ambientes" do
`ndovu-gitops/docs/DEPLOY.md`. Trate como dívida técnica priorizada, não como
o jeito certo de fazer.

**Correção:** mover a URL para runtime — um `/config.js` servido pelo próprio
servidor Next lendo `process.env` no boot, ou injeção via server component em
`window.__NDOVU_CONFIG__`. O ConfigMap `ndovu-dashboard-config` já existe no
gitops preparado para receber a variável; a mudança não altera o Deployment.

Detalhe importante: o valor é a URL **vista pelo browser do usuário** — a Route
pública da API. Nunca o Service interno do cluster; o browser não resolve
`ndovu-api.ndovu-app-prd.svc`.

## Deploy no OpenShift

Sem manifests aqui — eles vivem no `ndovu-gitops`.

| Item | Valor |
|------|-------|
| Deployment | `ndovu-dashboard`, 2 a 4 réplicas conforme o tier |
| Service/porta | 3000 |
| Route | `ndovu.<domínio>`, TLS edge com redirect |
| Probes | readiness e liveness em `GET /` |
| Escala | acompanha o número de **usuários do backoffice**, não o volume de eventos |

Números de CPU, memória e réplicas por tier: `ndovu-gitops/docs/CAPACITY.md`.

### Particularidades no OpenShift

- **`HOSTNAME=0.0.0.0` é obrigatório.** O `server.js` do output standalone
  escuta em `localhost` por default e a probe do Kubernetes falha sem isso.
- **Filesystem read-only** exige `emptyDir` montado em `/app/.next/cache`, que
  é onde o Next escreve cache de ISR/imagens em runtime.
- O `USER node` do Dockerfile é sobrescrito pelo UID aleatório da SCC
  `restricted-v2`. Funciona, desde que nada no runtime precise escrever fora
  dos volumes montados.
- CORS é decidido no **backend** (`NDOVU_CORS_ORIGINS`). Se o dashboard ganhar
  um domínio novo, a lista de origens da API precisa ser atualizada junto.

## Contratos com outros repositórios

| Repositório | Relação |
|-------------|---------|
| `ndovu-backend` | Única dependência de runtime. Consome `/v1/*` com JWT; a fonte da verdade das rotas é o `openapi.yaml` publicado lá |
| `ndovu-gitops` | Consome a imagem publicada aqui |

Mudança de contrato na API quebra este repositório em runtime, não em build —
os testes E2E do Playwright contra um backend real são a rede de proteção.
Rode-os na pipeline com uma stack efêmera, não só localmente.
