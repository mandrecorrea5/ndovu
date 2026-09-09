# Usuários e permissões (`/admin/users`)

## Para que serve

Gerencia contas do backoffice: quem entra no dashboard, com qual papel
e com acesso a quais apps. Suporta o RBAC de 3 níveis do Ndovu
(**admin**, **editor**, **viewer**) e, por cima disso, permite
**grants granulares por app** — para restringir um viewer a ver
somente `portal-cliente`, por exemplo. Também é onde admin reseta senha
sem precisar de fluxo de e-mail (self-hosted, direto).

## Onde fica

- Rota: `/admin/users`
- Arquivo: `dashboard/src/app/admin/users/page.tsx`
- Componente auxiliar: `dashboard/src/components/UserPermissionsPanel.tsx`
- Endpoints:
  - `GET /v1/admin/users`
  - `POST /v1/admin/users`
  - `PATCH /v1/admin/users/{id}` (nome, papel, senha, active, companyId)
  - `GET /v1/admin/users/{id}/permissions`
  - `PUT /v1/admin/users/{id}/permissions/{appId}`
  - `DELETE /v1/admin/users/{id}/permissions/{appId}`
- Papel mínimo: **admin**

## Como usar

1. Abra **Usuários**. A tabela lista nome, e-mail, empresa, papel,
   status e data.
2. **Novo usuário**: informe e-mail, nome, empresa (dropdown), papel e
   senha inicial. Use **gerar** para uma senha aleatória de 12
   caracteres sem ambiguidade (sem 0/O/1/l/I) e **copiar** para levar
   ao canal seguro.
3. **editar** abre o mesmo modal — o e-mail vira read-only (hint:
   "e-mail não pode ser alterado"). Deixando a senha em branco, ela não
   muda. Para editor/viewer aparece o painel **Apps concedidos a este
   {papel}**: adicione um app pelo dropdown ou clique **remover** ao lado
   de um app já concedido.
4. **senha** na linha da tabela abre o modal "Resetar senha" com uma
   senha nova já pré-gerada. Copie e clique **resetar senha**.
5. **desativar / reativar** alterna `active`. Usuário inativo não loga
   mas o histórico (audit log, comentários) permanece.

## O que você vê

- Botão **+ Novo usuário** e toast verde "Senha de X atualizada com
  sucesso" quando reset dá certo.
- Tabela com **Nome**, **E-mail**, **Empresa**, **Papel** (uppercase),
  **Status** (`● ativo` / `○ inativo`), **Criado em** e ações
  (**editar**, **senha**, **desativar/reativar**).
- Marca **(você)** ao lado do próprio usuário para evitar
  autodesligamento acidental.
- Modal de edição embute o **UserPermissionsPanel** — só para
  editor/viewer; admin exibe: "Admin da empresa vê todos os apps
  automaticamente".

## Como demonstrar

> "Onboarding de operador em 30 segundos: escolho a empresa, gero a
> senha, copio e mando. Se o cliente quer um viewer que só olhe um
> app específico, restrinjo por grant. Se não restringir, ele vê todos
> os apps da empresa — default útil, sem burocracia."

Roteiro de 60s:
1. Cadastre `ana@acme.com`, papel **viewer**, empresa "Acme S.A.",
   clique **gerar** e **copiar**.
2. Salve e faça login em aba anônima com essa conta — mostre que ela
   já entra e vê todos os apps de Acme (default).
3. Volte no admin, edite Ana e adicione o grant só de
   `portal-cliente`. Recarregue a aba anônima — outros apps somem.
4. Clique **senha** na linha dela, gere uma nova, mostre o toast de
   sucesso e o registro em `/admin/audit-log`.
5. **desativar** — a aba anônima cai no próximo request.

## Papéis (RBAC)

- Toda a tela exige **admin**. Editor/viewer não veem o link.
- **admin** — gerencia tudo dentro do próprio tenant (users, apps,
  chaves, regras, source maps, alertas, etc.).
- **editor** — consulta tudo + triagem de issues + funnels. Não mexe
  em admin.
- **viewer** — só consulta. Sem escrita.
- **Grants por app**: só se aplicam a editor/viewer. Sem grant nenhum
  = vê todos os apps da company (default útil); com grant, o scope
  restringe a lista.
- Nunca é permitido apagar/desligar o **último admin ativo** — o back
  retorna erro; a UI mostra em vermelho.
- Não existe DELETE de usuário. Só `PATCH active=false` — histórico
  preservado para audit.

## Dependências

- Empresa cadastrada em [`/admin/companies`](admin-companies.md).
- Apps cadastrados em [`/admin/apps`](admin-apps.md) para poder
  atribuir grants granulares.
- O `tenantScope` no backend consome `permissions` para restringir
  queries de eventos, sessions, issues, etc.

## Perguntas frequentes

- **"Posso mudar o e-mail depois?"**  
  Não pela UI — o input fica desabilitado. E-mail é chave lógica de
  login; para trocar, crie novo usuário e desative o antigo.
- **"Um viewer sem nenhum grant vê o quê?"**  
  Vê **todos os apps da empresa dele** (fallback). O painel de
  permissões avisa isso explicitamente.
- **"Reset de senha manda e-mail?"**  
  Não. O Ndovu é self-hosted sem SMTP obrigatório — o admin gera a
  senha e envia por canal seguro. O valor não fica salvo em claro.
- **"Como faço um usuário cross-tenant (ver várias empresas)?"**  
  Pela flag `is_super` no banco (não editável pela UI). Só o operador
  do Ndovu deve ter — dá bypass de tenant scope.

## Referências

- Página: `dashboard/src/app/admin/users/page.tsx`
- Painel de grants: `dashboard/src/components/UserPermissionsPanel.tsx`
- API client: `dashboard/src/lib/api.ts` → `listUsers`, `createUser`,
  `updateUser`, `listUserPermissions`, `grantUserPermission`,
  `revokeUserPermission`
- Handler backend: `backend/internal/adapter/httpapi/handlers.go`
- Docs relacionadas: [admin-companies.md](admin-companies.md),
  [admin-apps.md](admin-apps.md),
  [admin-audit-log.md](admin-audit-log.md)
