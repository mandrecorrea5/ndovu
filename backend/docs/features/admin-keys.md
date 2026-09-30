# Chaves de API (`/admin/keys`)

## Para que serve

Cada frontend emissor autentica no ingestor via header
`X-Api-Key`. Essa tela cria, lista e **revoga** as chaves — uma por
app, ou várias (produção + staging + rotação). A chave só aparece em
claro **na hora da criação**; o backend guarda apenas o hash e um
**prefixo curto** para identificação. Revogar invalida o cache em
memória do writer imediatamente — é a base da rotação zero-downtime.

## Onde fica

- Rota: `/admin/keys`
- Arquivo: `dashboard/src/app/admin/keys/page.tsx`
- Endpoints:
  - `GET /v1/admin/api-keys`
  - `POST /v1/admin/api-keys` (retorna a chave em claro **uma vez**)
  - `DELETE /v1/admin/api-keys/{id}` (revoga)
- Papel mínimo: **admin**

## Como usar

1. Abra **Chaves de API**. A listagem traz app, descrição, prefixo
   (`ndk_abc12…`), status, data e ações.
2. Clique **Nova chave**. Selecione o **App emissor** e opcionalmente
   uma **Descrição** ("produção web", "ambiente stg de QA").
3. Clique **gerar chave** — aparece o card verde:  
   "Chave criada para X — copie agora, ela não será exibida de novo."
4. Clique **copiar** e cole no `.env` do frontend
   (`NDOVU_API_KEY=…`). Feche o card.
5. Para revogar: clique **revogar** na linha correspondente. A chave
   passa para `○ revogada` e todos os writers deixam de aceitá-la em
   segundos (invalidação de cache).

## O que você vê

- Cabeçalho com texto explicativo:  
  "Cada frontend emissor usa a própria chave no header `X-Api-Key`. A
  chave só aparece em claro na criação — guardamos apenas o hash."
- Card verde após criação com a chave em `<code>` monospace e botões
  **copiar** / **fechar**.
- Tabela: **App**, **Descrição**, **Prefixo** (`ndk_xxxxx…`),
  **Status** (`● ativa` / `○ revogada`), **Criada em**, **Ações**.
- Estado vazio: "Nenhuma chave ainda — comece pelo botão 'Nova chave'."

## Como demonstrar

> "Rotação de chave sem downtime: gero uma nova, atualizo o `.env` do
> front (deploy simples), e só depois revogo a antiga. O cache do writer
> invalida em segundos, o front nunca fica sem enviar."

Roteiro de 60s:
1. Filtre por um app na listagem, aponte a coluna **Prefixo** — só o
   começo é visível.
2. Clique **Nova chave** para `portal-cliente`, descrição "rotação
   agosto".
3. Copie a chave, cole num terminal com `curl -H "X-Api-Key: ..."` no
   endpoint de ingestão e mostre `202 Accepted`.
4. Clique **revogar** na chave antiga. Repita o `curl` — agora responde
   `401`.
5. Aponte para `/admin/audit-log` — os eventos `apikey.create` e
   `apikey.revoke` já estão lá com quem executou.

## Papéis (RBAC)

- Toda a tela é **admin only**. Editor/viewer não veem o link.
- Admin de Company A só vê chaves dos apps de A —
  `tenantScope` no back garante isolamento.
- `is_super` vê chaves de todas as empresas.

## Dependências

- Precisa de **app cadastrado** em [`/admin/apps`](admin-apps.md) — o
  dropdown "App emissor" só lista apps existentes.
- O ingestor (`/v1/events`) usa o hash para autenticar. Cache in-memory
  no writer é invalidado no DELETE — se você roda múltiplos writers,
  todos escutam o mesmo evento de invalidação.

## Perguntas frequentes

- **"Perdi a chave, como recupero em claro?"**  
  Impossível — só hash é armazenado. Gere uma nova e revogue a antiga.
- **"Posso ter várias chaves ativas para o mesmo app?"**  
  Sim. É recomendado durante rotação: nova ativa + antiga ainda ativa
  até o deploy concluir.
- **"O prefixo `ndk_abc12…` identifica a chave completa?"**  
  Não — é apenas para achar a linha certa quando você tem várias.
  A chave completa nunca é reexibida.
- **"Revogação é imediata?"**  
  Sim, na prática. O DELETE marca `revoked_at`, invalida o cache
  in-memory dos writers e desliga a chave em segundos.

## Referências

- Página: `dashboard/src/app/admin/keys/page.tsx`
- API client: `dashboard/src/lib/api.ts` → `listKeys`, `createKey`,
  `revokeKey`
- Handler backend: `backend/internal/adapter/httpapi/handlers.go` —
  handlers de `/v1/admin/api-keys` e ingestor
- Docs relacionadas: [admin-apps.md](admin-apps.md),
  [admin-audit-log.md](admin-audit-log.md)
