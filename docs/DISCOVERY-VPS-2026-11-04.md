# Discovery da VPS e integração Ndovu — 2026-11-04

**Servidor:** `187.41.102.127` (`srv2106978`, alias `vps-prd`) · 31 GiB RAM · 394 GB (11% usado) · 28 containers.

## 1. Inventário

| Stack (compose) | Containers | Domínio público | Dados |
|---|---|---|---|
| `ndovu` | api, writer, dashboard, postgres, clickhouse, nats, mailhog | ndovu.solutions · api. · otlp. | Postgres `ndovu_prod` + ClickHouse `ndovu` |
| `ingress` | caddy | — (TLS/rotas de todos) | volumes caddy_data/config |
| `observability` | prometheus, grafana, loki, tempo, promtail, jaeger, otel-collector | grafana.rankinglpobrasil.com.br | volumes próprios |
| `felpma` | site, postgres, backup | felpma.com.br / www | Postgres `felpma` + 2 volumes de upload |
| `ranking-lpo` | portal, frontend, backend, postgres, backup | rankinglpobrasil.com.br / www · app. · api. | Postgres `ranking-lpo` |
| `elama` | web, api, db, backup, backup-offsite | elama.app / www | Postgres `elama` |

**smuls não está nesta VPS.** Não há container, diretório `/srv/apps/smuls` nem snippet no ingress. O repo tem o deploy pronto, mas o workflow é manual e o corte ainda não aconteceu; a produção atual segue na VPS antiga (Hostinger), sem acesso pela chave SSH atual.

## 2. Estado do Ndovu

Antes: **0 apps, 0 chaves** no control plane. A chave do felpma em produção (`ndk_df3d1bc8…`) não existia no banco e respondia **401** — nenhum evento chegava.

Depois (7 apps, chaves ativas):

| App | Superfície | Prefixo da chave | OTLP |
|---|---|---|---|
| `felpma` | browser Next.js | `ndk_84a7c854` | não |
| `ranking-lpo` | browser SvelteKit | `ndk_afddb402` | não |
| `ranking-lpo-backend` | Go (contrato v1) | `ndk_93451847` | não |
| `elama` | browser React/Vite | `ndk_cff3a7d2` | não |
| `elama-api` | Node/Fastify | `ndk_d85ed299` | sim |
| `smuls` | browser Next.js (prep) | `ndk_380e6ddc` | não |
| `smuls-api` | NestJS (prep) | `ndk_e8ccd21a` | sim |

Chaves em claro ficam fora do repositório:

- `~/.config/personal-projects/vps/ndovu-apps.json` (chmod 600)
- `~/.config/personal-projects/vps/smuls.ndovu.env` (chmod 600)
- nos `.env` de produção na VPS (600, dono `deploy`)

## 3. O que foi alterado

### felpma
- `.env` de produção: chave órfã substituída pela do app `felpma`; endpoint e app confirmados.
- Imagem `felpma-site` rebuildada e container recriado. SDK browser **1.3.0** (versão atual) já estava vendorizado.
- Chave nova confirmada no bundle e ingestão `202`.

### ranking-lpo
- SDK vendorizado atualizado de **1.0.0 → 1.3.0** (commit `7c07e97`), com release + Web Vitals no wrapper.
- `PUBLIC_NDOVU_*` (frontend) e `BACKEND_NDOVU_*` (Go) setados no `.env`.
- **Bug corrigido:** o backend enviava envelope sem `session.sessionId`; o ingest respondia **400** e descartava o lote. Agora usuário logado usa sessão estável (UUID v5) e anônimo usa a sessão do processo (commit `ac67caa`).
- **CSP corrigida:** `connect-src` não liberava `api.ndovu.solutions`, então o browser bloqueava o POST do SDK. Snippet do Caddy atualizado e reload aplicado (commit `f31ec76`).
- Stack rebuildada; frontend, portal e backend healthy.

### elama
- Browser SDK **1.3.0** vendorizado, `page_view` por rota no layout, sem `instrumentFetch` e sem snapshot de DOM (app financeiro).
- CSP do nginx libera `api.ndovu.solutions`.
- API: segundo exporter OTLP para `otlp.ndovu.solutions` com header `X-Api-Key`; Tempo continua recebendo os traces. Atributos `db.statement` e bodies são removidos antes de ir ao Ndovu.
- `VITE_NDOVU_*` como build args no Dockerfile/CI (chave via secret `NDOVU_API_KEY_ELAMA`).
- Deploy via CI concluído (`sha-462770f`); web e API healthy.

### smuls (sem deploy)
- App e chaves criados; secrets `NDOVU_API_KEY_SMULS` e `NDOVU_API_KEY_SMULS_API` configurados no GitHub.
- Branch `chore/ndovu-prep` (commit `55675e6`): SDK 1.3.0 no web, `page_view` no layout, build args no Dockerfile/workflow, placeholders no `provision-env.sh` e checklist em `docs/NDOVU.md`.
- A API ainda não exporta OTLP; chave reservada para o corte.

## 4. Validação

- Todas as 7 chaves: `POST /v1/events` → **202**.
- Eventos reais no ClickHouse: `felpma` (page_view + web vitals + action), `ranking-lpo` (page_view + vitals, com consentimento), `elama` (page_view /login), `ranking-lpo-backend` (action `login_failed`).
- OTLP: spans de `elama-api` no ClickHouse **e** no Tempo (dual export).
- Containers de felpma, ranking e elama healthy; endpoints públicos 200.
- Ranking backend sem nenhum `ndovu_http` de erro após o fix.

## 5. Pendências e recomendações

1. **smuls:** fazer o corte (DNS + `/srv/apps/smuls` + provision) e então publicar a branch `chore/ndovu-prep`; falta o exporter OTLP da API.
2. **Ranking:** o SDK só envia após consentimento LGPD (comportamento correto); validar o banner em produção.
3. **Elama:** o CSP bloqueia o beacon do Cloudflare Insights (`static.cloudflareinsights.com`) — problema pré-existente, não relacionado ao Ndovu. Decidir se libera ou remove o beacon.
4. **Chaves:** a antiga `ndk_df3d1bc8…` do felpma não existe no control plane; não há o que revogar. A rotação futura deve usar o painel (a chave nova aparece uma única vez).
5. **Backups:** as chaves em claro estão só na máquina local e nos `.env` da VPS; guardar cópia no cofre.

## 6. OTLP após a consolidação (2026-11-04 22:10)

O stack de observabilidade já aceitava OTLP nos três sinais; o problema era o
destino dos traces. O exporter do coletor foi trocado de **Jaeger (memória,
volátil)** para **Tempo (persistente, 72h)**.

| Sinal | Recepção | Destino | Retenção |
|---|---|---|---|
| Traces | coletor `:4317`/`:4318` + `otlp.ndovu.solutions` | Tempo + ClickHouse do Ndovu | 72h / 30d |
| Logs | `promtail` (Docker) + OTLP no coletor | Loki | 7d |
| Métricas | scrape Prometheus + OTLP no coletor | Prometheus | 15d / 2GB |

Validado com probes OTLP: log em `otlp-probe` no Loki, métrica
`ranking_lpo_probe_counter_total` no Prometheus e traces
`otlp-trace-probe`/`ranking-lpo-backend` no Tempo. O Jaeger continua no ar,
mas sem tráfego novo (só traces antigas em memória).

Pendências de observabilidade: logs de felpma e ndovu fora do Loki (sem label
`logging=promtail`); métricas de Elama/Ndovu não scrapeadas; Ndovu não aceita
OTLP logs (`/v1/logs` → 404, Fase 2 do roadmap).

## 7. Logs de felpma e ndovu no Loki

`felpma-site`, `ndovu-api`, `ndovu-writer` e `ndovu-dashboard` ganharam o label
`logging=promtail` (e `service=...`). O promtail descobriu os containers sem
restart e o Loki já lista os quatro serviços. Nada mudou no caminho de logs
dos apps: continua Docker json-file → promtail → Loki.

## 8. Métricas de Elama e Ndovu no Prometheus

- `ndovu-api` entrou na network `observability`; job `ndovu-api` em
  `ndovu-api-1:8080/metrics` (up).
- Elama API ganhou `/metrics` com `prom-client` (default metrics + requisições
  por método/rota/status); job `elama-api` em `elama-api-1:3001/metrics` (up).
- O endpoint do Elama é interno: o nginx só faz proxy de `/api/`.

## 9. Dashboards no Grafana

Novo provider/folder **VPS** (`dashboards-vps`), além do folder **Elama**:

| Dashboard | Conteúdo |
|---|---|
| **VPS — Visão geral** | alvos up/down, req/s e 5xx por serviço, p95, volume e erros de log por serviço |
| **Ndovu — API** | req/s e p95 por rota, 5xx, spans OTLP/s, logs de api+writer |
| **Elama — API** | req/s e p95 por rota, 5xx, memória RSS, logs da API |

## 10. Logs OTLP no Ndovu (Fase 2)

Implementado no backend e no dashboard:

- `POST /v1/logs` (OTLP HTTP protobuf/JSON, gzip) com a mesma `X-Api-Key`,
  flag `allow_otlp`, rate limit e limites de corpo do `/v1/traces`.
- Tabela `otel_logs` no ClickHouse (TTL 7d hot → 30d, partição por dia).
- Stream NATS `ndovu.logs.<app>` + consumer `ndovu-log-writer`, com DLQ.
- `GET /v1/logs` com filtros de app, serviço, severidade, texto, janela e
  scope de tenant.
- Página **/logs** no dashboard com filtros e salto para o trace.
- Redação das mesmas chaves sensíveis dos spans; corpo é truncado em 64 KB.

Configuração para um app instrumentado com OTel:

```
OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp.ndovu.solutions
OTEL_EXPORTER_OTLP_HEADERS=x-api-key=<chave de app com allow_otlp>
OTEL_LOGS_EXPORTER=otlp
```

### Validação da Fase 2 (2026-11-05 02:15)

- `POST https://otlp.ndovu.solutions/v1/logs` com a chave `elama-api` → **200**.
- Registro em `ndovu.otel_logs` com `severity_text=error`, `trace_id` e
  `authorization='***'` (redação aplicada).
- `GET /v1/logs?app=elama-api&severity=error` devolve o registro.
- Página **/logs** do dashboard renderiza a linha e o link “abrir” para o trace
  (validado com Chrome headless + login admin).
- Stream `NDOVU` com subjects `ndovu.events.>`, `ndovu.spans.>`,
  `ndovu.logs.>` e `ndovu.dlq.>`.

**Incidente no primeiro deploy:** o `EnsureSchema` concatenava `otel_spans` e
`otel_logs` porque faltava o `;` entre os dois `CREATE`. API e writer entraram
em crash-loop; a stack foi revertida para a imagem `7f79e3f2e49b` e o hotfix
`9987f51` (ponto e vírgula) foi publicado em seguida. O segundo release subiu
com `logs:true` e health 200.

**Pendência:** nenhum app ainda envia logs reais por OTLP; o receptor, a
tabela e a UI estão prontos. O próximo passo é ligar o exporter de logs no
Elama (o NodeSDK atual não tem `sdk-logs`) ou no backend Go do ranking.
