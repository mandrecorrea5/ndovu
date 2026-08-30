# ndovu-gitops

Fonte da verdade do que está rodando no OpenShift. Overlays por ambiente,
ConfigMaps, referências de Secret, Routes, NetworkPolicies, quotas e as
Applications do Argo CD.

> Este README é o modelo para o root do repositório `ndovu-gitops`.

## O que este repositório é

O único lugar onde se descreve **o estado desejado dos clusters**. Se um
objeto existe no OpenShift e não está aqui, é drift — e o Argo CD o remove.

Nenhuma linha de código de aplicação vive aqui, e nenhum valor de ambiente
vive nos repositórios de aplicação. A separação é o ponto: `ndovu-backend`
responde "o que o software faz"; este repositório responde "com que
configuração, em qual ambiente, com quantos recursos".

Também hospeda a documentação operacional que os outros repositórios
referenciam:

| Documento | Conteúdo |
|-----------|----------|
| `docs/ENVS.md` | Cada variável, seu ConfigMap/Secret de destino e o valor por ambiente |
| `docs/CAPACITY.md` | CPU, memória, disco e réplicas por workload e por tier |
| `docs/DEPLOY.md` | Pipeline, ordem de subida, migrações, probes, observabilidade, DR |

## Estrutura

```
apps/                     app-of-apps do Argo CD
  dev.yaml  hml.yaml  prd.yaml
base/                     manifests sem nenhum valor de ambiente
  api/                    Deployment, Service, ServiceAccount, PDB, HPA
  writer/                 Deployment (replicas 1, strategy Recreate)
  dashboard/              Deployment, Service
  migrate/                Job (PreSync hook)
overlays/
  dev/  hml/  prd/
    kustomization.yaml    tags de imagem, patches de réplica e recursos
    config/               ConfigMaps (common, api, writer, dashboard)
    secrets/              ExternalSecret ou SealedSecret — nunca valor claro
    network/              Routes, NetworkPolicies
    quota/                ResourceQuota, LimitRange
docs/                     ENVS.md, CAPACITY.md, DEPLOY.md
policy/                   regras Conftest/OPA validadas na CI
```

`base/` não contém um único valor que mude entre ambientes — nem tag de
imagem, nem número de réplica, nem endpoint. Tudo isso é patch de overlay. É o
que garante que ler `overlays/prd/` conte a história completa de produção.

## Ambientes e projects

| Overlay | Projects | Tier de capacity | Aprovação para merge |
|---------|----------|------------------|----------------------|
| `dev` | `ndovu-app-dev` + `ndovu-data-dev` | P | 1 revisor |
| `hml` | `ndovu-app-hml` + `ndovu-data-hml` | P | 1 revisor |
| `prd` | `ndovu-app-prd` + `ndovu-data-prd` | M ou G | 2 revisores, um deles SRE |

A camada de dados é uma Application separada, apontando para o repositório
`ndovu-platform` — rollout de aplicação nunca toca no banco.

## Como promover uma versão

1. A pipeline do repositório de aplicação publica `ndovu-backend:<sha>`.
2. Abra PR neste repositório alterando a tag em `overlays/<env>/kustomization.yaml`.
3. A CI roda `kustomize build`, `kubeconform` e as políticas de `policy/`.
4. Merge em `main` → Argo CD sincroniza.

O mesmo digest sobe em dev, hml e prd. **Exceção:** o `ndovu-dashboard` usa tag
`<sha>-<env>` porque embute a URL da API em build time — dívida rastreada no
README daquele repositório.

Rollback: reverter o commit da tag. Migração de banco **não é revertida** por
isso — mudança destrutiva de schema exige plano de reversão escrito antes do
merge.

## Configuração

Três fontes por workload, nesta precedência:

1. `ndovu-common-config` — comum a api e writer (endpoints de ClickHouse, NATS
   e S3, log level, URL do dashboard)
2. `ndovu-<workload>-config` — específico do workload
3. Secrets **por domínio** (`ndovu-postgres-credentials`,
   `ndovu-clickhouse-credentials`, `ndovu-auth-credentials`,
   `ndovu-s3-credentials`, `ndovu-smtp-credentials`)

Consumo por `envFrom`, não `env` item a item: adicionar variável vira mudança
de ConfigMap, sem tocar no Deployment.

Secrets por domínio e não um por aplicação porque rotacionar a senha do
Postgres não deve exigir tocar no segredo do JWT, e o time de banco precisa de
permissão de escrita apenas no seu.

Tabela completa com valor por ambiente: `docs/ENVS.md`.

### Secrets nunca ficam em texto claro aqui

| Opção | Quando |
|-------|--------|
| External Secrets Operator + Vault/CyberArk | Se a organização tem cofre. O Git guarda só o ponteiro |
| Sealed Secrets | Sem cofre. O Git guarda o cifrado; só o controller decifra |
| Gerados por operador | Postgres (CloudNativePG) e buckets (OBC) já geram os seus; o overlay referencia |

A CI bloqueia qualquer manifest que traga um `Secret` com `stringData` legível.

## Políticas validadas na CI

- Nenhuma imagem com tag `:latest` ou mutável
- Todo container com `requests` e `limits` de CPU e memória declarados
- Nenhum `Secret` em texto claro, nenhum `hostPath`
- ConfigMap sem chave que contenha `password`, `secret`, `token` ou `key`
- Toda Route com TLS e redirect de HTTP
- Todo namespace com `ResourceQuota` e `LimitRange`
- `ndovu-writer` com exatamente 1 réplica e estratégia `Recreate`

A última é uma trava de segurança operacional, não estilo: o writer hospeda os
loops de alerta, anomalia e digest, e duas réplicas disparam tudo em
duplicidade. A regra sai quando o `cmd/scheduler` for extraído no
`ndovu-backend`.

## Rede

| Regra | Definição |
|-------|-----------|
| Ingresso em `ndovu-data-<env>` | Apenas de `ndovu-app-<env>` |
| Egresso de `ndovu-app-<env>` | Dados do mesmo ambiente + S3 + SMTP + DNS |
| Route de consulta | `api.<domínio>`, timeout curto |
| Route de ingestão | `ingest.<domínio>`, timeout maior e limite de corpo ≥ `NDOVU_MAX_BODY_BYTES` |
| Route do dashboard | `ndovu.<domínio>`, TLS edge |
| Fora de qualquer Route pública | `/metrics`, `/docs`, `/openapi.yaml` |

Rotas separadas para ingestão e consulta permitem timeout, rate limit e limite
de corpo distintos, e permitem bloquear `/v1/admin` na borda da rota que está
exposta aos SDKs dos clientes.

## Repositórios consumidos

| Repositório | O que entra aqui |
|-------------|------------------|
| `ndovu-backend` | Imagem `ndovu-backend:<sha>` (api, writer, migrate) |
| `ndovu-dashboard` | Imagem `ndovu-dashboard:<sha>-<env>` |
| `ndovu-platform` | Application separada, camada de dados |
| `ndovu-sdk` | Nenhum — publica npm, não tem workload |
