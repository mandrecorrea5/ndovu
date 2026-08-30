# READMEs dos repositórios novos

Cada arquivo aqui é o **README pronto para o root do repositório
correspondente**, a ser copiado no momento da criação (onda 1 do plano).

| Repositório | README | Produz | Aparece no gitops? |
|-------------|--------|--------|--------------------|
| `ndovu-backend` | [`ndovu-backend/README.md`](ndovu-backend/README.md) | 1 imagem, 2 entrypoints (api + writer) | sim |
| `ndovu-dashboard` | [`ndovu-dashboard/README.md`](ndovu-dashboard/README.md) | 1 imagem (por ambiente, ver risco R2) | sim |
| `ndovu-sdk` | [`ndovu-sdk/README.md`](ndovu-sdk/README.md) | pacote npm | não |
| `ndovu-platform` | [`ndovu-platform/README.md`](ndovu-platform/README.md) | manifests da camada de dados | Application separada |
| `ndovu-gitops` | [`ndovu-gitops/README.md`](ndovu-gitops/README.md) | estado desejado dos clusters | é o gitops |
| `ndovu-docs` *(opcional)* | — | site estático | não |

## Sobre o `ndovu-docs`

Não tem README modelo aqui porque a decisão de criá-lo pode esperar. O que
existe hoje em `docs/` se divide em três naturezas:

| Conteúdo | Destino natural |
|----------|-----------------|
| `CONTRACT.md`, `INTEGRATION.md` | `ndovu-backend` — versionam junto com o `openapi.yaml` embedado |
| `ARQUITETURA.md`, `TECNICA.md`, `TESTING.md` | `ndovu-backend` |
| `features/*.md`, `FUNCIONAL.md`, `EXECUTIVA.md`, `USO.md` | `ndovu-docs`, se houver interesse em um portal; senão permanecem no backend |

A documentação que descreve **como o software funciona** deve viajar com o
código que a torna verdadeira. Só documentação de produto e de uso ganha com
um repositório próprio, e só quando existir público que não é do time.

## Ao copiar

Cada README referencia `ndovu-gitops/docs/{ENVS,CAPACITY,DEPLOY}.md`. Esses
três documentos são [`../ENVS.md`](../ENVS.md), [`../CAPACITY.md`](../CAPACITY.md)
e [`../DEPLOY.md`](../DEPLOY.md) deste diretório — mova-os para `docs/` do
`ndovu-gitops` na onda 1, e os links passam a resolver.

Substitua os placeholders `<cluster>` e `<domínio>` pelos valores reais do
ambiente antes do primeiro commit.
