# Apps (`/admin/apps`)

## Para que serve

Cada frontend que emite telemetria para o Ndovu é um **app**: um portal
React, um app mobile web, uma landing Next.js. Essa tela cadastra os
apps, associa cada um a uma empresa dona, marca a tecnologia (react,
vue, angular, etc.) e — no ato do cadastro — gera **automaticamente a
primeira chave de API** para o time integrador começar a enviar eventos.

## Onde fica

- Rota: `/admin/apps`
- Arquivo: `dashboard/src/app/admin/apps/page.tsx`
- Endpoints:
  - `GET /v1/admin/apps`
  - `POST /v1/admin/apps` (retorna app + chave em claro)
  - `PATCH /v1/admin/apps/{id}`
  - `DELETE /v1/admin/apps/{id}` (revoga todas as chaves)
- Papel mínimo: **admin**

## Como usar

1. Abra **Apps** — a listagem mostra nome, tecnologia, empresa,
   responsável e data.
2. Clique em **Novo app**. Preencha:
   - **Nome do app** (ex.: `portal-cliente`) — obrigatório e único.
   - **Tecnologia** (dropdown com `react`, `vue`, `angular`, etc.).
   - **Empresa** — selecione uma empresa ativa; se preferir digitar,
     deixe o select em branco e use o input de fallback.
   - **Responsável** (e-mail do owner).
3. Ao salvar, aparece um card verde com a **chave gerada em claro** —
   copie na hora, ela não volta a ser exibida.
4. Use **editar** para trocar tecnologia/empresa/responsável.
   **excluir** revoga todas as chaves do app antes de removê-lo (com
   confirmação: "Todas as chaves dele serão revogadas.").

## O que você vê

- Cabeçalho **Apps** com botão **+ Novo app**.
- Card verde após criação com a chave copiável (só uma vez).
- Tabela com **Nome**, **Tecnologia**, **Empresa**, **Responsável**,
  **Criado em** e **Ações** (editar/excluir).
- Estado vazio: "Nenhum app cadastrado — comece pelo botão 'Novo app'."
- Modal de cadastro/edição com os quatro campos + fallback de empresa
  em texto livre.

## Como demonstrar

> "Cadastrar um app leva 20 segundos e já sai daqui com a chave pronta
> pra colar no SDK. O Ndovu não faz onboarding em duas etapas — app e
> primeira chave nascem juntos, é onde perdemos zero fricção."

Roteiro de 60s:
1. Clique em **Novo app**, batize "portal-demo", tecnologia `react`,
   empresa "Padrão".
2. Salve e mostre o **card verde** com a chave — copie.
3. Cole a chave no snippet SDK (`initNdovu({ apiKey: '...' })`) para
   provar a integração.
4. Volte, edite o app para trocar o responsável — mostre que **nome
   não muda** (é chave lógica).
5. Vá em `/admin/keys` e confirme que a chave inicial aparece com
   status **ativa**.

## Papéis (RBAC)

- Só **admin** entra na tela. Editor/viewer não veem o link de menu.
- Um admin de Company A cria/edita/exclui apenas apps de A —
  `tenantScope` filtra no back.
- `is_super` enxerga apps de todas as empresas.

## Dependências

- Precisa de pelo menos uma **empresa** cadastrada em
  `/admin/companies` para o select mostrar opções (o fallback em texto
  livre permite cadastrar antes, mas o vínculo formal fica fraco).
- A chave gerada aparece também em `/admin/keys` — a gestão contínua
  (revogar, criar novas) acontece lá.

## Perguntas frequentes

- **"Perdi a chave que apareceu na criação — como recupero?"**  
  Não é possível. Gere uma nova em `/admin/keys` e revogue a antiga.
  O Ndovu guarda só o hash — não há como reexibir em claro.
- **"O que acontece com os eventos já ingeridos se eu excluir o app?"**  
  Os eventos históricos permanecem no ClickHouse (imutáveis), mas as
  chaves são revogadas — nada novo entra em nome desse app.
- **"Posso ter dois apps com o mesmo nome?"**  
  Não. `name` é único no tenant. Use sufixos (`portal-cliente-prod`,
  `portal-cliente-stg`) para separar ambientes.

## Referências

- Página: `dashboard/src/app/admin/apps/page.tsx`
- API client: `dashboard/src/lib/api.ts` → `listApps`, `createApp`,
  `updateApp`, `deleteApp`
- Lista de tecnologias: `dashboard/src/lib/constants.ts` → `TECHNOLOGIES`
- Handler backend: `backend/internal/adapter/httpapi/handlers.go`
- Docs relacionadas: [admin-keys.md](admin-keys.md),
  [admin-companies.md](admin-companies.md)
