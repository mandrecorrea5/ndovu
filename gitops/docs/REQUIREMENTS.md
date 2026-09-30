# O que precisamos para subir o Ndovu no OpenShift

Checklist consolidada de **pré-requisitos** para levar a aplicação do zero a um
ambiente OpenShift de pé. Este documento responde "o que precisamos ter antes
de começar" — não *como* configurar (veja `DEPLOY.md`), nem *quais valores*
(`ENVS.md`), nem *quanto* dimensionar (`CAPACITY.md`).

> Cada ambiente (dev, hml, prd) exige este mesmo conjunto. O que muda entre
> eles é só o tamanho (CAPACITY), o overlay e a exigência de aprovação.

---

## 1. Acesso e permissões

| Pré-requisito | Quem | Nota |
|---------------|------|------|
| `oc` CLI (ou `kubectl`) autenticado no cluster | Desenvolvedor/SRE | `oc version` responde com o cluster alvo |
| Acesso de criação de **Projects** (namespaces) | Plataforma/SRE | ou os 7 projects já criados com RBAC delegado |
| Permissão de leitura nos clusters para o Argo CD | SRE | ServiceAccount do Argo CD com `cluster-admin` ou `admin` nos projects |
| Acesso ao **registry de imagens** (push/pull) | CI | pipeline publica; cluster faz pull |
| Acesso a um **cofre** (Vault/CyberArk) ou ao Sealed Secrets controller | SRE | origem dos valores dos Secrets (ver `ENVS.md` §6) |
| Acesso a **DNS externo** para as Routes | Plataforma | criar os domínios `api.<domínio>`, `ingest.<domínio>`, `ndovu.<domínio>` |

## 2. Projects (namespaces) necessários

| Project | Conteúdo |
|---------|----------|
| `ndovu-app-dev` / `-hml` / `-prd` | api, writer, dashboard, migrate |
| `ndovu-data-dev` / `-hml` / `-prd` | clickhouse, broker de stream, postgres, buckets |
| `ndovu-cicd` | Argo CD Applications, ImageStreams compartilhadas |

## 3. Operadores e Charts instalados no cluster

São **dependências de plataforma** — ou já existem no cluster (preferível) ou
precisam ser instalados antes da onda 2/3.

| Componente | Instalação | Namespace |
|------------|-----------|-----------|
| **Altinity ClickHouse Operator** | operador | `ndovu-data-<env>` |
| **NATS** (JetStream) | Helm chart (StatefulSet, file store) | `ndovu-data-<env>` |
| **Kafka/Redpanda** *(alternativa)* | Redpanda/Strimzi Operator | `ndovu-data-<env>` |
| **RabbitMQ** *(alternativa)* | RabbitMQ Cluster Operator | `ndovu-data-<env>` |
| **CloudNativePG** | operador | `ndovu-data-<env>` |
| **Argo CD** | operador | `ndovu-cicd` (ou cluster-wide) |
| **External Secrets Operator** ou **Sealed Secrets** | operador | cluster-wide |
| **ObjectBucketClaim (ODF/NooBaa)** ou S3 corporativo externo | plataforma | `ndovu-data-<env>` |

> **Decisão a tomar antes de instalar**: qual broker de stream? O NATS é o
> default; Kafka e RabbitMQ são alternativas (o backend é agnóstico via
> `NDOVU_STREAM_BACKEND`). Instale **apenas o broker escolhido** para o
> ambiente — não os três.

## 4. Serviços externos (não gerenciados por nós)

| Serviço | Uso | Origem |
|---------|-----|--------|
| **Relay SMTP** corporativo ou **SendGrid** | digest semanal | fornecido pela organização |
| **S3 corporativo** ou ODF/NooBaa | cold tier do ClickHouse + snapshots | OBC ou endpoint externo |
| **DNS/Route** para `api`, `ingest`, `ndovu` | acesso público | plataforma |

## 5. Secrets a criar (valores fora do Git)

Origem dos valores em `ENVS.md` §6. Não criar manualmente em prd — usar
ESO/Sealed Secrets.

| Secret | Domínio | Chaves principais |
|--------|---------|-------------------|
| `ndovu-postgres-credentials` | Postgres | `NDOVU_POSTGRES_URL` (+ `sslmode=require` em prd) |
| `ndovu-clickhouse-credentials` | ClickHouse | `NDOVU_CLICKHOUSE_USER`, `NDOVU_CLICKHOUSE_PASSWORD` |
| `ndovu-auth-credentials` | Auth | `NDOVU_AUTH_SECRET`, `NDOVU_ADMIN_EMAIL`, `NDOVU_ADMIN_PASSWORD`, `NDOVU_BOOTSTRAP_INGEST_KEY` |
| `ndovu-s3-credentials` | Buckets | `NDOVU_S3_ACCESS_KEY`, `NDOVU_S3_SECRET_KEY` |
| `ndovu-smtp-credentials` | Mailer | `NDOVU_SMTP_*` ou `NDOVU_SENDGRID_API_KEY` |

> **Obrigatório**: `NDOVU_AUTH_SECRET` aleatório (≥32 bytes), por ambiente.
> A API sobe com default inseguro se o Secret não for montado — a pipeline
> deve **bloquear** o deploy quando ausente (ver `ENVS.md` §2.3).

## 6. SCC e segurança

| Item | Exigência |
|------|-----------|
| SCC `restricted-v2` | api, writer e dashboard (binário Go estático roda com UID aleatório) |
| Banco de dados | SCC conforme o operador escolhido |
| ImageStreams / scan de imagem | registry interno com scan de CVE (bloqueia em hml/prd) |

## 7. O que a aplicação cria sozinha (não é pré-requisito)

Estes **não** precisam ser provisionados manualmente — o código garante na
subida:

- **Schema do ClickHouse** (`EnsureSchema`) e **migrações do Postgres**
  (`ctlpostgres.Connect`) — aplicados pelo Job `ndovu-migrate` (PreSync).
- **Stream/tópicos/filas** do broker: `EnsureStream` (NATS), `EnsureTopics`
  (Kafka), `EnsureQueues` (RabbitMQ) criam no boot.
- **Buckets** `ndovu-cold` e `ndovu-snapshots` (só o *endpoint/credenciais* vêm
  da OBC; a aplicação cria se ausente, em dev).

## 8. Observabilidade (pós-subida, mas planejar desde o início)

| Item | Pré-requisito |
|------|---------------|
| `ServiceMonitor` para `/metrics` da API | habilitar *user workload monitoring* no cluster |
| Alerta de lag do broker | nats-exporter (NATS), consumer lag (Kafka), métricas de fila (RabbitMQ) |
| Alertas de PVC (ClickHouse hot > 75%, broker > 70%) | Prometheus + Alertmanager |

## 9. Ordem de validação (resumo)

A sequência completa de subida está em `DEPLOY.md` §4. Em uma frase: infra →
operadores → projects → secrets → `ndovu-platform` → `ndovu-migrate` → writer →
api → dashboard → observabilidade.

## 10. Checklist final antes de `ndovu-data-prd` e `ndovu-app-prd` receberem tráfego

- [ ] Os 7 projects criados com RBAC e quotas
- [ ] Operadores instalados (ClickHouse, CloudNativePG, broker, Argo CD, ESO/Sealed)
- [ ] Todos os Secrets de `ENVS.md` presentes (nenhum com default inseguro)
- [ ] `NDOVU_STREAM_BACKEND` decidido e o broker correspondente provisionado
- [ ] `NDOVU_CORS_ORIGINS` com lista explícita (sem `*`)
- [ ] `NDOVU_POSTGRES_URL` com `sslmode=require`
- [ ] `NDOVU_S3_USE_SSL=true`
- [ ] `NDOVU_STREAM_MAX_AGE_HOURS` casado com o PVC do broker (`CAPACITY.md` §4.2)
- [ ] Routes com TLS edge; `/metrics`, `/docs`, `/openapi.yaml` fora da Route pública
- [ ] SCC `restricted-v2` aplicado; nenhum container com UID fixo
- [ ] `ServiceMonitor` + alertas mínimos configurados
- [ ] Restore de backup do Postgres testado
