# Procedimento de Deploy no OpenShift

Como o código sai do commit e chega em produção. Vale para os três ambientes;
o que muda entre eles é o overlay e a exigência de aprovação.

---

## 1. Modelo geral

```
  repo de aplicação                 registry                 ndovu-gitops                cluster
  ─────────────────                 ────────                 ────────────                ───────
  push/merge  ──▶ CI: test+build ──▶ imagem:<sha>  ──▶ bump da tag no overlay ──▶ Argo CD sync ──▶ pods
                                                          (PR, revisado)
```

Três princípios:

1. **A imagem é imutável e identificada pelo SHA do commit.** Nada de `:latest`
   nem de retag. O mesmo digest que passou em hml é o que vai para prd.
2. **Nenhum `oc apply` manual.** O cluster reflete o `ndovu-gitops`; qualquer
   divergência é drift e o Argo CD reverte.
3. **Promover é um PR.** Subir para hml ou prd é alterar uma linha de tag no
   overlay correspondente e passar pela revisão daquele ambiente.

> Exceção viva: o `ndovu-dashboard` precisa de build por ambiente enquanto o
> risco R2 não for corrigido (a URL da API é embutida em build time). A tag
> dele é `<sha>-<env>`, e a promoção hml→prd **rebuilda**. É a única quebra do
> princípio 1 e deve ser tratada como dívida, não como padrão.

## 2. Pipeline por repositório

### `ndovu-backend`

| Etapa | Comando | Bloqueia merge? |
|-------|---------|-----------------|
| Vet | `go vet ./...` | sim |
| Testes unitários | `go test -race -count=1 ./...` | sim |
| Testes de integração | `go test -tags=integration ./...` (testcontainers: Postgres, ClickHouse, MinIO) | sim |
| Build | `go build ./...` | sim |
| Validação do OpenAPI | lint do `api/openapi.yaml` | sim |
| Imagem | build multi-stage → `ndovu-backend:<sha>` | — |
| Scan | Trivy/Quay scanner, falha em CVE crítica | sim em hml/prd |
| SBOM | anexado ao artefato | não |

Uma imagem, dois entrypoints: `/ndovu-api` (default) e `/ndovu-writer`. O
Deployment do writer sobrescreve o `command`. Isso evita duas pipelines para
código que sempre sobe junto.

### `ndovu-dashboard`

`npm ci` → `npm run typecheck` → `npm run build` → Playwright (specs E2E já
existentes) contra um backend efêmero → imagem `<sha>-<env>`.

### `ndovu-sdk`

`node --check` em cada SDK, testes de contrato contra o `openapi.yaml`
publicado pelo backend, `npm publish` no registry interno com semver. **Não
gera imagem nem deploy.**

### `ndovu-platform` e `ndovu-gitops`

`kustomize build` de cada overlay + `kubeconform`/`oc apply --dry-run=server`
+ política (Conftest/OPA: proíbe `:latest`, container sem `requests`, Secret em
texto claro, `hostPath`). Merge no `main` do gitops é o gatilho do Argo CD.

## 3. Estrutura do `ndovu-gitops`

```
ndovu-gitops/
├── apps/                        # Applications do Argo CD (app-of-apps)
│   ├── dev.yaml  hml.yaml  prd.yaml
├── base/                        # manifests sem valor de ambiente
│   ├── api/                     # Deployment, Service, ServiceAccount, PDB, HPA
│   ├── writer/
│   ├── dashboard/
│   └── migrate/                 # Job de migração
└── overlays/
    ├── dev/                     # ConfigMaps, tags, réplicas, Routes, quotas
    ├── hml/
    └── prd/
```

Cada overlay contém: os ConfigMaps do [`ENVS.md`](ENVS.md), as referências de
Secret (ExternalSecret ou SealedSecret), as tags de imagem, os números do tier
correspondente do [`CAPACITY.md`](CAPACITY.md), Routes, NetworkPolicies,
ResourceQuota e LimitRange.

O `ndovu-platform` é referenciado como Application separada, apontando para os
namespaces `ndovu-data-<env>` — camada de dados e camada de aplicação
sincronizam de forma independente, para que um rollout de aplicação nunca
toque no banco.

## 4. Ordem de subida

Da primeira vez em um ambiente novo, nesta ordem — cada passo é pré-requisito
do seguinte:

| # | O quê | Verificação antes de seguir |
|---|-------|-----------------------------|
| 1 | Criar projects, quotas, LimitRanges, ServiceAccounts, NetworkPolicies | `oc get quota` nos dois namespaces |
| 2 | Secrets (ESO/Sealed) | Todos os Secrets do `ENVS.md` presentes |
| 3 | `ndovu-platform`: PostgreSQL, ClickHouse, NATS, buckets | Conexão validada de um pod debug nos três; `storage.xml` carregado (`SELECT * FROM system.storage_policies` mostra `tiered`) |
| 4 | Job `ndovu-migrate` | Job `Completed`; `schema_migrations` no Postgres com todas as versões; `trace_events` existe no ClickHouse com a policy `tiered` |
| 5 | `ndovu-writer` | Log `ndovu-writer consumindo`; consumer visível no `nats stream info` |
| 6 | `ndovu-api` | `/health` 200; `/v1/auth/login` com o admin de bootstrap |
| 7 | `ndovu-dashboard` | Login pela Route; tela de visão geral carrega |
| 8 | Route de ingestão + chave real | `seed` (do `ndovu-sdk`) gera sessões e elas aparecem no explorador |
| 9 | ServiceMonitor, alertas, backup | Métricas no console do OpenShift; um restore de backup testado |

**Writer antes da API** é deliberado: se a API subir primeiro e receber
ingestão, as mensagens ficam no stream sem consumidor — funciona, mas o
primeiro sinal de saúde do sistema fica atrasado. Com o writer no ar, o
primeiro evento ingerido já aparece no dashboard.

Nos deploys seguintes, o Argo CD faz tudo: PreSync roda o Job de migração,
Sync aplica os Deployments, rollout do writer é `Recreate` e o da API/dashboard
é `RollingUpdate`.

## 5. Migrações de schema

Hoje as migrações rodam **no boot** de api e writer (`ctlpostgres.Connect` →
`migrate()` com advisory lock; `clickhouse.EnsureSchema`). O Postgres está
protegido contra concorrência; o ClickHouse **não** — várias réplicas subindo
juntas podem executar o mesmo `ALTER TABLE` simultaneamente.

Padrão adotado:

- Job `ndovu-migrate` como **PreSync hook** do Argo CD, mesma imagem, entrypoint
  que só aplica schema e sai.
- `NDOVU_SKIP_MIGRATIONS=true` nos Deployments de api e writer (variável a ser
  implementada — risco R3). Até lá, `maxSurge: 1`/`maxUnavailable: 0` na API
  reduz a janela de concorrência sem eliminá-la.
- Migrações **sempre compatíveis para trás**: o Job roda antes do código novo,
  então o código antigo precisa continuar funcionando com o schema novo por
  alguns minutos. Coluna nova, sim; `DROP COLUMN` no mesmo deploy, não —
  quebra em dois releases.

Rollback de aplicação (`argocd app rollback` ou revert da tag) **não desfaz
migração**. Migração destrutiva exige plano de reversão escrito antes do merge.

## 6. Probes

| Workload | Readiness | Liveness | Startup |
|----------|-----------|----------|---------|
| `ndovu-api` | `GET /health` a cada 10s | `GET /health` a cada 30s, 3 falhas | 30 × 2s (aguarda ClickHouse/NATS/Postgres) |
| `ndovu-writer` | — (sem HTTP, risco R4) | `exec` no processo | — |
| `ndovu-dashboard` | `GET /` a cada 10s | `GET /` a cada 30s | 15 × 2s |

O startup probe generoso na API existe porque o boot conecta em NATS,
ClickHouse e Postgres e aplica schema antes de escutar — sem ele, o Kubernetes
mata o pod no meio da inicialização quando o banco está lento.

`terminationGracePeriodSeconds`: 30 na API (o `ShutdownTimeout` do código é
15s) e 60 no writer, para ele terminar o lote em voo e dar ACK antes de morrer.

## 7. Observabilidade

- **Métricas da API**: `/metrics` em formato Prometheus, coletado por
  `ServiceMonitor` (habilitar *user workload monitoring* no cluster). São
  métricas in-process por réplica — agregue no PromQL, não espere um total
  global de um pod só.
- **Writer**: sem endpoint hoje (R4). Enquanto isso, o sinal de saúde é o
  `num_pending` do consumer no NATS, via nats-exporter. **Este é o alerta mais
  importante do sistema**: pending crescendo = writer parado ou ClickHouse
  fora, e o relógio de perda de dados é o `STREAM_MAX_AGE_HOURS`.
- **Logs**: `slog` em JSON no stdout, coletados pelo stack de logging do
  cluster. `NDOVU_LOG_LEVEL=info` em hml/prd.
- **Alertas mínimos**: pending do JetStream acima do limiar; uso do PVC do
  ClickHouse hot > 75%; uso do PVC do NATS > 70%; taxa de 5xx na ingestão;
  writer com 0 réplicas prontas; Job de migração falhado.

## 8. Segurança e isolamento

| Item | Definição |
|------|-----------|
| SCC | `restricted-v2` para api, writer e dashboard (o binário Go estático não se importa com UID aleatório). Banco de dados conforme exigido pelo operador escolhido |
| ServiceAccount | Uma por workload, sem permissão de API do cluster |
| NetworkPolicy | `ndovu-data-<env>` só aceita ingresso de `ndovu-app-<env>`; egresso da aplicação restrito a dados + S3 + SMTP + DNS |
| Routes | TLS edge, redirect de HTTP; `/metrics`, `/docs`, `/openapi.yaml` fora da Route pública |
| Imagens | Registry interno com scan; base distroless no backend |
| Secrets | Nunca em ConfigMap, nunca em Git em texto claro (seção 6 do `ENVS.md`) |

## 9. Backup e recuperação

| Dado | Estratégia | RPO | RTO |
|------|-----------|-----|-----|
| PostgreSQL (control plane) | Backup contínuo (WAL) para bucket, via operador | 5 min | 1 h |
| ClickHouse (traces) | `BACKUP TABLE` para S3, diário; cold tier já está em S3 | 24 h | 4 h |
| NATS JetStream | Sem backup — é buffer transitório | — | — |
| Snapshots de sessão | Versionamento + replicação do bucket | — | — |
| Configuração | O próprio `ndovu-gitops` | 0 | minutos |

Perder o control plane é o pior cenário: sem ele não há usuários, apps nem
chaves de API, e a ingestão para. Perder o ClickHouse custa histórico de
traces, mas o serviço volta a funcionar imediatamente (o JetStream segura os
eventos da janela de retenção enquanto o banco não volta).

Testar o restore do Postgres a cada trimestre — um backup que nunca foi
restaurado não é um backup.

## 10. Ambiente local depois da separação

O `docker-compose.yml` vai para `ndovu-backend/dev/compose.yml`, subindo
Postgres, ClickHouse, NATS, MinIO, MailHog e os dois binários locais. O
dashboard, quando não estiver sendo desenvolvido, entra como **imagem
publicada** do `ndovu-dashboard`, apontando para a API local.

Quem desenvolve o dashboard roda `npm run dev` com `NEXT_PUBLIC_NDOVU_API`
apontando para `http://localhost:18081` — mesmo fluxo de hoje, sem precisar do
repositório do backend clonado se usar a imagem publicada da API.

Manter esse caminho funcionando é o principal custo recorrente da separação em
múltiplos repositórios. Vale um teste de CI que sobe o compose e roda o `seed`
de ponta a ponta, para que ele não apodreça sem ninguém perceber.
