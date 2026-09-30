# ndovu-platform

Camada de dados do Ndovu no OpenShift: ClickHouse, PostgreSQL, broker de stream
(NATS JetStream default; Kafka e RabbitMQ alternativos) e buckets de objeto.
e object storage. Provisionamento, tuning, retenção, backup e runbooks.

> Este README é o modelo para o root do repositório `ndovu-platform`.
> Origem: `infra/` do monorepo + os serviços de infraestrutura do
> `docker-compose.yml`.

## O que este repositório é

Tudo que tem **estado**. É o repositório com o maior custo de erro e o menor
ritmo de mudança — por isso vive separado da aplicação, com dono e cadência
próprios: um rollout de aplicação nunca deve poder tocar no banco.

Vive nos namespaces `ndovu-data-dev`, `ndovu-data-hml` e `ndovu-data-prd`.
Nenhum componente daqui tem Route: acesso só por ClusterIP, e só a partir do
namespace de aplicação do mesmo ambiente (NetworkPolicy).

### Fronteira com o `ndovu-backend`

| Responsabilidade | Dono |
|------------------|------|
| Subir e operar o servidor ClickHouse, disks, policies, tuning | **este repositório** |
| Definir e migrar `trace_events` (schema, TTL, projection, índices) | `ndovu-backend` |
| Subir e operar o PostgreSQL, réplicas, backup | **este repositório** |
| Migrações do control plane (`migrations/*.sql`) | `ndovu-backend` |
| Criar o stream JetStream, tuning do servidor, cluster raft | dividido: servidor aqui, stream criado pelo `EnsureStream` da aplicação |
| Buckets, credenciais, ciclo de vida | **este repositório** |

Resumindo: **servidor aqui, schema lá.** Se um dia essa linha ficar confusa, o
teste é "quem precisa saber disso para compilar?" — se a resposta for o código
Go, é do backend.

## Componentes

### ClickHouse — armazém de traces

Provisionado pelo **Altinity ClickHouse Operator** (`ClickHouseInstallation`),
não por StatefulSet manual: ele resolve topologia, réplicas, configuração
distribuída e a compatibilidade com as SCCs do OpenShift.

O ativo mais importante daqui é a **política de storage em camadas**
(`clickhouse/config/storage.xml`, hoje em `infra/clickhouse/`):

| Idade do dado | Volume | Disco |
|---------------|--------|-------|
| 0–7 dias | `default` (hot) | SSD/NVMe, PVC |
| 7–30 dias | `warm` | PVC em StorageClass mais barata |
| 30–90 dias | `cold` | Bucket S3 |
| 90 dias+ | — | `DELETE` por TTL |

A migração entre volumes é automática (`TTL ... TO VOLUME`, aplicado pelo
schema do `ndovu-backend`). O primeiro volume **precisa** se chamar `default`
para que a policy possa ser trocada sem reprocessar as parts existentes.

No `docker-compose` o `storage.xml` traz endpoint e credenciais do MinIO em
texto claro. No OpenShift ele vira **template preenchido pelo overlay do
ambiente** a partir do Secret da ObjectBucketClaim — nenhuma credencial
commitada.

Tuning por tier em `clickhouse/config/tuning/`: `max_memory_usage` (≈50% do
limite do container, para que uma consulta grande falhe com erro em vez de
OOMKillar o pod), `background_pool_size`, `merge_max_block_size`.

### NATS JetStream — buffer durável de ingestão (backend default)

File store em PVC. **A janela de retenção do stream é, literalmente, o tempo
que o ClickHouse pode ficar fora sem perda de dados** — é uma decisão de SLA,
não um parâmetro técnico. Ela é configurada pela aplicação
(`NDOVU_STREAM_MAX_AGE_HOURS`) e o PVC dimensionado aqui precisa acompanhar.

Cuidado de capacidade: o JetStream guarda o JSON **bruto** (~1,5 KB/evento),
não os ~300 B comprimidos do ClickHouse. Manter 48h em produção de alto volume
faz deste PVC o maior disco do ambiente. Ver `ndovu-gitops/docs/CAPACITY.md`.

Em dev, 1 réplica. Em hml/prd, cluster de 3 com raft.

> **Kafka e RabbitMQ são alternativas suportadas** (`NDOVU_STREAM_BACKEND`),
> provisionadas por esta camada de dados quando o ambiente as seleciona. A
> aplicação (`ndovu-backend`) é agnóstica; só o broker em si muda. A retenção
> dimensiona pelo mesmo volume (~1,5 KB/evento × janela), com mecânica própria
> (retenção por tópico no Kafka, filas/TTL no RabbitMQ).

### PostgreSQL — control plane

Guarda o que faz a plataforma existir: usuários do backoffice, empresas, apps,
chaves de API, permissões, triagem de issues, saved views, funis, regras de
sampling e anomalia, metadados de snapshot, feedbacks e audit log.

Provisionado pelo **CloudNativePG** (ou pela imagem `rhel9/postgresql-16`
suportada pela Red Hat). **Não usar `postgres:16-alpine`**: ela assume UID fixo
e falha sob a SCC `restricted-v2`.

É pequeno em disco e crítico em disponibilidade: **sem control plane não há
validação de chave de API, e a ingestão para**. Backup contínuo por WAL,
réplica em standby a partir de hml.

Crescimento vem quase todo do audit log e dos feedbacks. Definir política de
expurgo do audit log antes que ela seja definida por um disco cheio.

### Object storage

Duas ObjectBucketClaims por ambiente:

| Bucket | Uso | Quem escreve |
|--------|-----|--------------|
| `ndovu-cold-<env>` | Cold tier do ClickHouse (parts de 30–90 dias) | ClickHouse |
| `ndovu-snapshots-<env>` | HTML de replay de sessão | API (`POST /v1/snapshots`) |

Via ODF/NooBaa, ou apontando para o S3 corporativo. **O MinIO do
`docker-compose` é ferramenta de desenvolvimento e não sobe no OpenShift.**

A OBC gera automaticamente o ConfigMap (endpoint, nome do bucket) e o Secret
(chaves) que a aplicação consome — não crie essas credenciais à mão.

### SMTP

O digest semanal precisa de saída de e-mail. Em produção, relay corporativo
(Service `ExternalName` + NetworkPolicy de egresso) ou SendGrid. O MailHog do
compose é só desenvolvimento.

## Estrutura

```
clickhouse/
  installation/         ClickHouseInstallation por ambiente
  config/storage.xml    policy tiered (hot/warm/cold)
  config/tuning/        parâmetros por tier
postgres/
  cluster/              CloudNativePG por ambiente
  backup/               destino e agenda do WAL archiving
nats/
  helm-values/          values por ambiente (JetStream, cluster, PVC)
buckets/                ObjectBucketClaims
network/                NetworkPolicies dos namespaces de dados
runbooks/               procedimentos operacionais
```

## Deploy

Sincronizado pelo Argo CD como **Application separada** da aplicação,
apontando para `ndovu-data-<env>`. Mudanças aqui exigem revisão do time de
plataforma e janela combinada — expandir PVC, trocar StorageClass ou alterar
topologia do ClickHouse não são operações de rollout comum.

Ordem na criação de um ambiente novo: namespaces e quotas → Secrets → buckets
→ PostgreSQL → ClickHouse (com `storage.xml`) → NATS. Só então o
`ndovu-backend` pode rodar o Job de migração.

Validação antes de liberar para a aplicação:

- `SELECT * FROM system.storage_policies` mostra `tiered` com os três volumes
- `SELECT 1` no PostgreSQL a partir de um pod do namespace de aplicação
- `nats stream ls` responde, e o PVC tem o tamanho esperado
- Escrita e leitura em ambos os buckets a partir de um pod debug
- NetworkPolicy nega acesso a partir de qualquer outro namespace

## Runbooks obrigatórios

| Situação | Documento |
|----------|-----------|
| PVC hot do ClickHouse acima de 75% | `runbooks/clickhouse-disk.md` |
| Backlog crescendo no JetStream | `runbooks/jetstream-backlog.md` |
| Restore do control plane | `runbooks/postgres-restore.md` |
| Migração do cold tier entre buckets | `runbooks/cold-tier-migration.md` |
| Expansão de PVC / troca de StorageClass | `runbooks/storage-expansion.md` |

**Restore do PostgreSQL testado a cada trimestre.** Um backup que nunca foi
restaurado não é um backup — e este é o único dado da plataforma cuja perda
derruba a ingestão inteira.

## Capacity

Todos os números — CPU, memória, PVC por tier, tamanho de bucket e a
derivação de cada um — em `ndovu-gitops/docs/CAPACITY.md`. Não duplicar aqui:
uma tabela de dimensionamento em dois lugares diverge em três meses.
