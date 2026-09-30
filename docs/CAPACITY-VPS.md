# Capacity — VPS única com Docker Compose (produção)

Tradução do [`CAPACITY.md`](./CAPACITY.md) do OpenShift para a trilha atual:
**uma VPS, todos os serviços em um Docker Compose.** Sem HPA, sem réplicas de
banco, sem cluster NATS. O documento original vale como referência de
premissas (§1) e fórmulas (§7) — aqui só mudam as topologias e os limites.

Premissas de tamanho por evento (valem igual): 1,5 KB no stream · ~600 B
efetivos no ClickHouse (ZSTD + projection ×2).

**Hardware contratado: 4 vCPUs · 8 GB RAM · 200 GB SSD.**

**Veredito honesto contra os tiers do CAPACITY.md original:**
- **RAM 8 GB ≈ tier P+ apertado** (o tier M do OpenShift pede 16 Gi só de
  requests). Este documento dimensiona **até 5 M eventos/dia**.
- **4 vCPUs** servem 5 M/dia com folga (a carga é dominada por ClickHouse em
  consultas, não por ingestão).
- **200 GB SSD é OK para 5 M/dia** — desde que a janela do stream seja
  reduzida (§4). Acima de 5 M/dia o hot estoura (§5).

## 1. O que muda numa VPS única

| Componente | OpenShift tier M | VPS (este plano) |
|---|---|---|
| ClickHouse | 1 shard × 2 réplicas, 4/16Gi | **1 instância** |
| NATS JetStream | cluster 3 nós, MaxAge 12h | **1 instância** com `MAX_AGE=12h` (§4) |
| API | 3–8 réplicas HPA | **1 processo** |
| Writer / Dashboard | 1–3 / 2–3 | **1 / 1** (writer já é 1 por design) |
| Postgres | primária + réplica | **1 instância** + backup diário age (obrigatório: é a única cópia) |
| Monitoramento | Prometheus de cluster | **obrigatório externo** (a VPS é SPOF) |

**Regra de segurança desta topologia:** a soma dos `limits` de memória de
todos os serviços **precisa caber em ~85% da RAM física com swap = 0** —
sem orquestrador, quem estoura é o OOM killer aleatório do kernel, e ele
mata o maior consumidor: com sorte a consulta do ClickHouse (erro de
consulta), com azar o Postgres (a ingestão **para inteira** — control plane
morto = chaves não validam).

## 2. Limites de memória — 8 GB

| Serviço | limite atual | **proposto (8 GB)** | por quê |
|---|---|---|---|
| postgres | 768M | **1G** | control plane pequeno (tier P do original: 1Gi limit) |
| clickhouse | 2560M | **4G** | o maior leitor de RAM (mark cache + consultas) — 4G com `max_memory_usage=1.5G` por consulta (§2.1) |
| api | 512M | **1G** | sessão longa monta 10k eventos em memória (pico documentado no original §3.1) |
| writer | 512M | **1G** | batch + avaliador de anomalias |
| dashboard | 512M | **768M** | 1 processo Next |
| nats | **sem limite** | **1G** | 20 M/dia? não roda aqui. 5 M/dia = 7,5 GB/dia brutos na janela de 12h ≈ 4 GB de **disco**, não de RAM |
| caddy | 128M | 128M | inalterado |
| **soma** | 5,0G (+nats livre) | **8,0G** | = 8 GB nominais — a soma é deliberadamente no teto: limites de Go/Node raramente ficam todos no pico ao mesmo tempo; **swap=0 + monitor de pressão de memória é condição de aceite** |

Folga real vem do monitoramento + revisão a cada onboarding (§6). Se
preferir 10% de folga nominal: dashboard 640M, api 900M, caddy 96M.

### 2.1 ClickHouse: teto de consulta

Com o limite de 4G, **configurar o teto por consulta** — otherwise uma
consulta analítica estoura os 4G e derruba o servidor:

- criar `infra/clickhouse/tuning-prod.xml` com
  `<max_memory_usage>1500000000</max_memory_usage>` (1,5G ≈ 40% do limite)
- montar no `docker-compose.prod.yml` em
  `/etc/clickhouse-server/config.d/tuning.xml:ro`
- (opcional recomendado) `max_concurrent_queries_for_user = 2`

## 3. CPU — 4 vCPUs

CPU é overcommitável (memória não). Somando 6 você divide o time-slice
com pesos relativos — por isso ClickHouse leva o maior.

| Serviço | limite (cap.) | peso (share) |
|---|---|---|
| clickhouse | 3 | 300 |
| api | 1.5 | 150 |
| writer | 1 | 100 |
| dashboard | 1 | 100 |
| postgres | 1 | 100 |
| nats | 0.5 | 50 |
| caddy | 0.5 | 50 |
| **soma** | **8.5 (2× overcommit)** | — |

O SO/Docker fica com o que sobrar — com 4 vCPUs, o 1º alerta de
capacidade é **CPU steal do hypervisor** do provedor (monitorar).

## 4. Janela do stream × disco

- 5 M ev/dia × 1,5 KB = 7,5 GB/dia **brutos**.
- Default do código (`NDOVU_STREAM_MAX_AGE_HOURS=48`): 15 GB retidos + 40%
  de folga (verificação do MaxAge + backlog) ≈ 21 GB — ok, mas a janela
  grande é o que permite backlog silencioso.
- **Aplicar `NDOVU_STREAM_MAX_AGE_HOURS: "12"`** em **api e writer** no
  `docker-compose.prod.yml`: retido ≈ 4–8 GB + folga. Custo: ClickHouse
  fora por **mais de 12h = perda de dados mais antigos** — restrição
  assumida da topologia 1×1×1 (e o motivo de o runbook de backlog ter
  alerta de idade).
- Confirmar aplicado: `docker compose ... exec nats nats stream info NDOVU`
  → campo `Max Age`.

## 5. Orçamento de disco — 200 GB

| Item | 5 M/dia (nosso plano) |
|---|---|
| ClickHouse **hot** (7d × 1,7 de merge) | 60 GB |
| ClickHouse **warm** (23d × 1,2) — **mesmo SSD**, a política atual não separa discos na VPS | 83 GB |
| NATS (12h + 40% folga) | 8 GB |
| Postgres + WAL | 10 GB |
| Docker (imagens, logs json) + SO + folga | 25 GB |
| **total** | **~186 GB — 93%** ⚠️ |

**Aprovação com ressalva:** 186/200 com 93% de uso **não é aceitável**.
Alavancas, na ordem de preferência:

1. **Mover o warm para mais cedo** (ex.: 14d em vez de 30d):
   warm 14d × 1,2 × 3 GB/dia efetivo = **50 GB** → total **~153 GB (77%)**.
   É 1 `ALTER TABLE ... MODIFY TTL` (a ferramenta de TTL do ClickHouse é
   idempotente e roda em background). **Recomendado.**
2. **Volume extra (250–500 GB de bloco) montado como `chdata-warm`** —
   resolve sem mexer em retenção (~US$ 10–20/mês). Melhor caminho se o
   1º cliente contratual exigir os 90d completos quentes/frios.
3. **Encurtar a retenção global 90d→60d** — decisão de produto (LGPD/SLA),
   não operacional.

## 6. Limites por tenant com 1 só stack

Single VPS = N tenants compartilham CPU/IOPS/cache — **sem isolamento de
recursos por empresa**. O que protege o vizinho:
- `NDOVU_INGEST_RATE_RPS` **por chave** é a única válvula real (default 50
  rps por app; revisar por contrato, não por tenant).
- Amostra por app para o barulhento que não pode ser reduzido (regra de
  sampling do produto).
- Alerta externo de P95 de ingestão por app → detectar barulhento antes
  que ele afete os outros.

## 7. Quando a VPS única deixa de servir (gatilhos de upgrade)

- **> 5 M ev/dia sustentado** (RAM é o teto primeiro: 8 GB).
- 1º OOM kill em postgres/api, ou 2º em 90d qualquer.
- Volume de snapshots de replay crescendo (bucket `ndovu-snapshots` é
  R2 — não pesa no SSD, mas a consulta pesa).
- 1º cliente contratual com SLA — disponibilidade 1×1×1 não entrega.
- Manutenção > 12h (janela do stream).

Esses são os gatilhos da trilha OpenShift (`docs/openshift/`). Migração =
backfill de ClickHouse + restore de Postgres — com 1 sprint de
antecedência, nunca em incidente.

## 8. Mudanças de configuração a aplicar (este documento autoriza)

**Estado (2026-09-30):**

- ✅ `docker-compose.prod.yml`: novos limits de memória/CPU (§2, §3)
- ✅ env `NDOVU_STREAM_MAX_AGE_HOURS: "12"` em `api` e `writer`
- ✅ `infra/clickhouse/tuning-prod.xml` (`max_memory_usage` 1,5G +
  `max_concurrent_queries_for_user=2`) montado no ClickHouse
- ✅ validado com `docker compose config` (limites renderizados corretos)

**Ainda a fazer:**

- Após 1 semana de medição: rodar a query de eventos/dia
  (`docs/openshift/CAPACITY.md` §7) e revisar §5 com volume real.
- **Mover warm para 14d (§5.1) exige decisão de produto** (encurta a
  permanência local dos dados, não a retenção total).
- **Nenhuma** mudança de 90d sem decisão de produto (§5.3).
