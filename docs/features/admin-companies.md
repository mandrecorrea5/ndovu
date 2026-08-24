# Empresas (`/admin/companies`)

## Para que serve

Multi-tenant leve. Toda operação do Ndovu (usuários, apps, chaves,
eventos) pertence a uma **empresa**. Essa tela cadastra as empresas
cliente e ativa/desativa acesso sem apagar histórico. Existe sempre a
empresa **Padrão** — criada no bootstrap para receber o admin inicial e
os primeiros apps antes de qualquer segregação por cliente.

## Onde fica

- Rota: `/admin/companies`
- Arquivo: `dashboard/src/app/admin/companies/page.tsx`
- Endpoints:
  - `GET /v1/admin/companies`
  - `POST /v1/admin/companies`
  - `PATCH /v1/admin/companies/{id}`
  - `DELETE /v1/admin/companies/{id}`
- Papel mínimo: **admin**

## Como usar

1. Abra **Empresas** no menu admin — a listagem traz nome, documento,
   status (ativa/inativa) e data de criação.
2. Clique em **Nova empresa**, informe **Nome** (obrigatório) e
   opcionalmente **Documento (CNPJ)**. Marque **ativa**.
3. Use **editar** para alterar nome/documento ou desmarcar **ativa** —
   empresa inativa deixa de aparecer no seletor de "Empresa" ao cadastrar
   usuário ou app.
4. **remover** só passa se a empresa não tiver usuários nem apps
   vinculados. A API responde `409` quando há dependência — o feedback
   aparece em vermelho no topo da tela.

## O que você vê

- Cabeçalho **Empresas** com o botão **+ Nova empresa** à direita.
- Tabela com colunas **Nome**, **Documento**, **Status** (`● ativa` ou
  `○ inativa`), **Criada** e **Ações**.
- Estado vazio: "Nenhuma empresa cadastrada ainda." — só acontece em
  ambiente novo antes do bootstrap.
- Modal de criação/edição com **Nome**, **Documento (CNPJ, opcional)** e
  checkbox **ativa**.

## Como demonstrar

> "Empresa é o átomo de multi-tenant do Ndovu. Todo usuário e todo app
> pertencem a uma — e essa tela é onde a operação recorta os clientes
> antes de convidar gente. Uma empresa inativa desaparece dos seletores
> mas os dados históricos ficam auditáveis."

Roteiro de 60s:
1. Aponte para a linha **Padrão** — "essa vem no bootstrap, guarda o
   admin inicial e os primeiros apps".
2. Clique em **Nova empresa**, cadastre "Acme S.A." — mostre que ela já
   aparece na lista.
3. Vá em **/admin/apps → Novo app** e mostre que "Acme S.A." apareceu no
   dropdown **Empresa**.
4. Volte, **desative** Acme e mostre que sumiu do dropdown mas ainda
   está listada aqui.
5. Tente **remover** uma empresa com app vinculado — o erro `409` do
   backend prova a proteção.

## Papéis (RBAC)

- Toda a tela exige **admin**. Editor/viewer sequer veem o item de menu.
- Um admin de Company A **não** vê nem edita Company B — o
  `tenantScope` filtra no back. A tela é multi-tenant safe.
- Um `is_super` (flag no user) bypassa o scope e vê todas as empresas —
  usado apenas pelo operador do Ndovu self-hosted.

## Dependências

- Bootstrap do backend cria a empresa **Padrão** e o admin inicial
  vinculado a ela — sem isso não há como logar. Veja `backend/cmd/api`.
- Cadastro/edição de **usuários** (`/admin/users`) e **apps**
  (`/admin/apps`) exige que a empresa exista e esteja ativa.

## Perguntas frequentes

- **"Por que não consigo excluir a empresa Padrão?"**  
  A API bloqueia empresas com usuários ou apps atrelados — a Padrão
  quase sempre tem pelo menos o admin do bootstrap.
- **"Desativar apaga os eventos ingeridos?"**  
  Não. `active=false` só impede novas vinculações; o ClickHouse mantém
  o histórico. Para apagar dados de end-user use `/admin/gdpr`.
- **"Preciso preencher CNPJ?"**  
  Não é obrigatório. Serve só para exibir na listagem/nota fiscal
  interna do operador; o Ndovu não valida o dígito.

## Referências

- Página: `dashboard/src/app/admin/companies/page.tsx`
- API client: `dashboard/src/lib/api.ts` → `listCompanies`,
  `createCompany`, `updateCompany`, `deleteCompany`
- Handler backend: `backend/internal/adapter/httpapi/handlers.go` →
  handlers de `/v1/admin/companies`
- Docs relacionadas: [admin-users.md](admin-users.md),
  [admin-apps.md](admin-apps.md)
