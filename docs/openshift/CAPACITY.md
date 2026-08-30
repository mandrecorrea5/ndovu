# Mapeamento de Capacity por Projeto

Dimensionamento de CPU, memória, disco e réplicas para cada workload, em três
tiers de volume. As premissas estão explícitas na seção 1 — se o seu volume
real for outro, a seção 7 mostra como recalcular sem refazer o documento.

---

## 1. Premissas do cálculo

| Premissa | Valor | De onde vem |
|----------|-------|-------------|
| Tamanho médio do evento em JSON (na rede e no JetStream) | **1,5 KB** | Contrato v1 com `request_body`/`response_body` redigidos e truncados pelo SDK |
| Teto por requisição | 1 MB | `NDOVU_MAX_BODY_BYTES` (default) |
| Tamanho médio comprimido no ClickHouse | **~300 B/evento** | `CODEC(ZSTD(3))` nos payloads + `LowCardinality` nas dimensões (`schema.sql`) |
| Fator da PROJECTION `by_session` | **×2** | A projection é `SELECT * ORDER BY (session_id, occurred_at)` — **duplica a tabela inteira em disco** |
| **Custo efetivo em disco** | **600 B/evento** | 300 B × 2 |
| Retenção | hot 0–7d · warm 7–30d · cold 30–90d (S3) · delete 90d+ | TTL em `schema.sql` + policy `tiered` |
| Folga para merges no volume hot | ×1,7 | ReplacingMergeTree precisa de espaço temporário durante merge |
| Folga no warm | ×1,2 | Poucos merges, dados já consolidados |
| Fator de pico sobre a média | ×3 | Concentração em horário comercial |

> **A projection é o item mais caro do schema e o mais fácil de esquecer.**
> Ela existe para que a timeline de uma sessão não varra a janela de tempo
> inteira — é o caso de uso central do produto. Se o disco apertar, a decisão
> é entre pagar 2× de storage ou aceitar consultas de sessão muito mais lentas.
> Não é um desperdício a ser cortado sem medir.

## 2. Tiers

| Tier | Perfil | Eventos/dia | Pico (ev/s) | Volume efetivo/dia |
|------|--------|-------------|-------------|--------------------|
| **P** | Piloto · dev · hml | 2 M | ~100 | 1,2 GB |
| **M** | Produção padrão — dezenas de apps, milhares de usuários | 20 M | ~700 | 12 GB |
| **G** | Escala — centenas de apps ou app de massa | 100 M | ~3.500 | 60 GB |

Use **P** em `ndovu-app-dev` e `ndovu-app-hml`; escolha M ou G para prd a
partir da medição da seção 7.

---

## 3. Projeto `ndovu-app-<env>` — aplicação

### 3.1 `ndovu-api` (repo `ndovu-backend`)

Stateless. Escala horizontal por HPA em CPU. É o único workload que precisa
absorver o pico de ingestão em tempo real.

| | Tier P | Tier M | Tier G |
|---|--------|--------|--------|
| Réplicas (min–max) | 2 (fixo) | 3–8 (HPA 70% CPU) | 6–20 (HPA 70% CPU) |
| `requests` CPU / memória | 100m / 256Mi | 300m / 512Mi | 500m / 1Gi |
| `limits` CPU / memória | 500m / 512Mi | 1500m / **2Gi** | 2 / **3Gi** |
| PodDisruptionBudget | — | `minAvailable: 2` | `minAvailable: 4` |

**Sobre a memória:** o limite não é ditado pela ingestão (parse + publish é
barato), e sim pelas **consultas**. `GET /v1/sessions/{id}` monta até 10.000
eventos em memória, cada um podendo carregar `request_body`/`response_body`
descomprimidos. Uma sessão longa e verbosa é o pior caso e é o que causa
OOMKill. Comece nos valores acima, monitore `container_memory_working_set_bytes`
no p99 e suba o limite antes de reduzir réplicas.

**Rate limit:** `NDOVU_INGEST_RATE_RPS` é **por API key, por réplica** (o
limiter é in-process). Com 6 réplicas e `RPS=200`, o teto real por key é
~1.200 rps. Dimensione dividindo o teto desejado pelo número mínimo de réplicas.

### 3.2 `ndovu-writer` (repo `ndovu-backend`)

**Réplica fixa em 1 nos três tiers**, por causa do risco R1 (os loops de
alerta, anomalia e digest rodam aqui e duplicariam com N réplicas).

| | Tier P | Tier M | Tier G |
|---|--------|--------|--------|
| Réplicas | 1 | 1 | 1 |
| `requests` CPU / memória | 100m / 256Mi | 500m / 1Gi | 1 / 2Gi |
| `limits` CPU / memória | 500m / 1Gi | 2 / 2Gi | 4 / 4Gi |
| `NDOVU_WRITER_BATCH` | 64 | 256 | 1024 |
| Estratégia de rollout | `Recreate` | `Recreate` | `Recreate` |

Memória = `WRITER_BATCH` × tamanho da mensagem, mais o pico das consultas do
avaliador de anomalias (que roda a cada 300s e faz agregações no ClickHouse).
Com `BATCH=1024` e mensagens de 1,5 KB o buffer é pequeno (~1,5 MB); a folga
é para os ciclos de alerta/anomalia.

**Throughput não é o gargalo** — um writer só insere muito acima de 3.500 ev/s
em bulk. O problema de ficar em 1 réplica é **disponibilidade**: durante o
restart, o JetStream acumula (por isso o PVC do NATS na seção 4.2). `Recreate`
em vez de `RollingUpdate` é deliberado: garante que nunca existam dois writers
simultâneos disparando o mesmo alerta.

Depois de extrair `cmd/scheduler` (correção do R1): o writer passa a HPA 2–6 no
tier M e 4–12 no tier G, e o scheduler vira um workload de 1 réplica com
100m/256Mi em qualquer tier.

### 3.3 `ndovu-dashboard` (repo `ndovu-dashboard`)

| | Tier P | Tier M | Tier G |
|---|--------|--------|--------|
| Réplicas | 2 | 2–3 | 4 |
| `requests` CPU / memória | 100m / 256Mi | 250m / 512Mi | 500m / 1Gi |
| `limits` CPU / memória | 500m / 512Mi | 1 / 1Gi | 1 / 1Gi |

Escala com o número de **usuários do backoffice**, não com o volume de eventos
— por isso cresce muito menos que a API entre os tiers. Montar `emptyDir` em
`/app/.next/cache` se o filesystem do container for read-only.

### 3.4 `ndovu-migrate` (Job, repo `ndovu-backend`)

Roda no PreSync de cada deploy, uma vez. `requests` 200m/256Mi,
`limits` 500m/512Mi, `backoffLimit: 2`, `activeDeadlineSeconds: 600`.
Igual nos três tiers.

### 3.5 ResourceQuota e LimitRange do namespace

| | Tier P | Tier M | Tier G |
|---|--------|--------|--------|
| `requests.cpu` | 1 | 4 | 14 |
| `requests.memory` | 2Gi | 8Gi | 28Gi |
| `limits.cpu` | 4 | 18 | 50 |
| `limits.memory` | 6Gi | 16Gi | 52Gi |
| `pods` | 15 | 25 | 45 |

`limits.cpu` fica bem acima de `requests.cpu` de propósito: os picos de
ingestão são curtos e o cluster absorve por overcommit. `limits.memory`, não —
memória não é compressível, e a soma dos limites deve caber na capacidade real
alocada ao projeto.

LimitRange sugerido (default para container sem requests declarados):
`default` 500m/512Mi, `defaultRequest` 100m/128Mi, `max` 4/8Gi.

---

## 4. Projeto `ndovu-data-<env>` — camada de dados

### 4.1 ClickHouse

O componente mais caro em todas as dimensões.

| | Tier P | Tier M | Tier G |
|---|--------|--------|--------|
| Topologia | 1 pod | 1 shard × 2 réplicas | 2 shards × 2 réplicas |
| `requests` CPU / memória (por pod) | 2 / 8Gi | 4 / 16Gi | 8 / 32Gi |
| `limits` CPU / memória (por pod) | 4 / 16Gi | 8 / 32Gi | 16 / 64Gi |
| `max_memory_usage` por consulta | 4G | 8G | 16G |
| PVC hot (`default`, SSD) | 30Gi | 250Gi | 500Gi/pod |
| PVC warm (`/warm`, HDD/SSD barato) | 50Gi | 500Gi | 1Ti/pod |
| StorageClass hot | SSD / NVMe | SSD / NVMe | NVMe local se disponível |
| Bucket cold (S3/ODF) | 150 GB | 1,5 TB | 6 TB |

Memória da consulta é o que derruba ClickHouse. `max_memory_usage` em ~50% do
limite do container deixa espaço para mark cache, índices e merges — uma
consulta que estoura falha com erro em vez de OOMKillar o pod inteiro.

Derivação do disco (tier M): 12 GB/dia efetivos × 7 dias hot = 84 GB × 1,7 de
folga de merge = **143 GB → PVC 250Gi**. Warm: 12 × 23 dias = 276 GB × 1,2 =
331 GB → **PVC 500Gi**. Cold: 12 × 60 = 720 GB → **bucket 1,5 TB** com folga.

No tier G cada shard guarda metade dos dados, e cada réplica guarda o shard
inteiro — por isso o PVC por pod é ~metade do total, mas há 4 pods.

### 4.2 NATS JetStream

| | Tier P | Tier M | Tier G |
|---|--------|--------|--------|
| Réplicas | 1 | 3 (cluster raft) | 3 (cluster raft) |
| `requests` CPU / memória | 100m / 256Mi | 500m / 1Gi | 1 / 2Gi |
| `limits` CPU / memória | 500m / 1Gi | 2 / 2Gi | 4 / 4Gi |
| `NDOVU_STREAM_MAX_AGE_HOURS` | 48 | **12** | **12** |
| Ingestão bruta/dia | 3 GB | 30 GB | 150 GB |
| Retido na janela | 6 GB | 15 GB | 75 GB |
| PVC file store (por pod) | 20Gi | **50Gi** | **200Gi** |

**Este é o número que mais surpreende.** O JetStream guarda o JSON **bruto**
(1,5 KB/evento, sem compressão colunar), não os 300 B do ClickHouse. Manter os
48h de default no tier G significaria 300 GB de PVC só de buffer.

A janela de retenção é diretamente **o tempo que o ClickHouse pode ficar fora
sem perda de dados**. 12h cobre com folga qualquer manutenção planejada e a
maior parte dos incidentes; se o SLA exigir mais, o custo é linear
(24h no tier G = 400Gi de PVC).

Dimensione o PVC para ~2,5× o retido na janela: o `MaxAge` só descarta na
próxima verificação, e um backlog real (writer fora do ar) enche o disco antes.

### 4.3 PostgreSQL (control plane)

| | Tier P | Tier M | Tier G |
|---|--------|--------|--------|
| Topologia | 1 instância | 1 primária + 1 réplica | 1 primária + 2 réplicas |
| `requests` CPU / memória | 250m / 512Mi | 1 / 2Gi | 2 / 4Gi |
| `limits` CPU / memória | 1 / 1Gi | 2 / 4Gi | 4 / 8Gi |
| PVC dados | 10Gi | 50Gi | 100Gi |
| PVC WAL/backup | 5Gi | 20Gi | 50Gi |

Guarda usuários, apps, empresas, chaves, triagem de issues, saved views,
funis, regras de sampling/anomalia, metadados de snapshot, feedbacks e
**audit log**. Só o audit log e os feedbacks crescem com o uso — o resto é
praticamente constante. 50Gi no tier M cobre anos; a política de expurgo do
audit log é o que decide se isso continua verdade.

### 4.4 Object storage (bucket)

| Bucket | Tier P | Tier M | Tier G | Conteúdo |
|--------|--------|--------|--------|----------|
| `ndovu-cold-<env>` | 150 GB | 1,5 TB | 6 TB | Parts do ClickHouse com 30–90 dias |
| `ndovu-snapshots-<env>` | 20 GB | 200 GB | 1 TB | HTML de replay de sessão (`/v1/snapshots`) |

Snapshots são estimados em ~200 KB por snapshot comprimido; ajuste depois de
medir o uso real do replay, que depende de quanto o time ativa a captura.

Provisionar via **ObjectBucketClaim** (ODF/NooBaa) ou apontar para o S3
corporativo. O MinIO do `docker-compose` é ferramenta de desenvolvimento e não
deve subir no OpenShift.

### 4.5 ResourceQuota do namespace de dados

| | Tier P | Tier M | Tier G |
|---|--------|--------|--------|
| `requests.cpu` | 3 | 7 | 36 |
| `requests.memory` | 10Gi | 22Gi | 136Gi |
| `limits.cpu` | 6 | 16 | 72 |
| `limits.memory` | 20Gi | 44Gi | 272Gi |
| `requests.storage` | 150Gi | 1Ti | 8Ti |
| `persistentvolumeclaims` | 6 | 10 | 16 |

---

## 5. Consolidado por tier

Soma dos **requests** — é isso que ocupa capacidade real no cluster.

| Ambiente | CPU | Memória | Disco (PVC) | Object storage |
|----------|-----|---------|-------------|----------------|
| dev (P) | 4 | 12Gi | 115Gi | 170 GB |
| hml (P) | 4 | 12Gi | 115Gi | 170 GB |
| prd tier M | 11 | 30Gi | 870Gi | 1,7 TB |
| **Total com prd M** | **19 vCPU** | **54Gi** | **1,1Ti** | **2,1 TB** |
| prd tier G | 50 | 164Gi | 6,4Ti | 7 TB |
| **Total com prd G** | **58 vCPU** | **188Gi** | **6,6Ti** | **7,3 TB** |

Nós do cluster: no cenário prd tier M, três worker nodes de 8 vCPU/32Gi já
comportam tudo com folga de failover. No tier G, o ClickHouse pede nós
dedicados (taint/toleration + nodeAffinity) de pelo menos 16 vCPU/64Gi, porque
os pods do banco não devem competir por I/O com a aplicação.

## 6. Rede e Routes

| Fluxo | Tier M | Tier G | Nota |
|-------|--------|--------|------|
| Ingestão (entrada na Route) | ~1 MB/s no pico | ~5 MB/s no pico | 1,5 KB × pico ev/s |
| Consultas do dashboard | baixo | baixo | dezenas de usuários |
| Writer → ClickHouse | ~0,4 MB/s | ~2 MB/s | já comprimido no insert |
| ClickHouse → cold (S3) | ~12 GB/dia | ~60 GB/dia | migração de TTL, assíncrona |

Banda não é restrição em nenhum tier. O que exige atenção nas Routes:

- **Route de ingestão separada** (`ingest.<domínio>`) da Route de consulta
  (`api.<domínio>`): permite timeout, rate limit e limite de corpo distintos,
  e permite bloquear `/v1/admin` na borda da rota pública de ingestão.
- `haproxy.router.openshift.io/timeout` maior na rota de ingestão (lotes
  grandes) e menor na de consulta.
- Limite de corpo da Route ≥ `NDOVU_MAX_BODY_BYTES`, senão o proxy corta o
  lote antes de a aplicação responder.
- `/metrics`, `/docs` e `/openapi.yaml` **não devem sair na Route pública**
  (risco R5) — restringir por `path` ou usar Service interna separada para o
  scraping.

## 7. Como recalcular para o seu volume

Meça primeiro, em uma janela representativa de 7 dias:

```sql
-- eventos/dia e bytes reais por evento (rode no ClickHouse)
SELECT toDate(occurred_at) AS dia,
       count() AS eventos
FROM trace_events
GROUP BY dia ORDER BY dia;

-- tamanho comprimido real por evento, já incluindo a projection
SELECT sum(bytes_on_disk) / sum(rows) AS bytes_por_evento
FROM system.parts
WHERE table = 'trace_events' AND active;
```

Depois aplique:

| Grandeza | Fórmula |
|----------|---------|
| Volume efetivo diário | `eventos_dia × bytes_por_evento` (a query acima **já inclui** a projection) |
| PVC hot | `volume_diário × 7 × 1,7` |
| PVC warm | `volume_diário × 23 × 1,2` |
| Bucket cold | `volume_diário × 60 × 1,3` |
| PVC NATS (por pod) | `eventos_dia × 1,5 KB × (MAX_AGE_HOURS / 24) × 2,5` |
| Réplicas mínimas da API | `pico_ev_s / 2000`, mínimo 2 |
| Memória da API | medir p99 de working set com a maior sessão do sistema |

Revisar o dimensionamento a cada onboarding de app novo e sempre que o volume
diário variar mais de 50% — o disco do ClickHouse é o item com maior tempo de
correção (expandir PVC é fácil; reduzir, não).
