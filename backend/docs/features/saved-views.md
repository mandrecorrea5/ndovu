# Views salvas (menu ★ Views)

## Para que serve

Filtro bom é filtro que você **volta**. Em vez de digitar de novo
"últimos 7 dias, app X, só erros, código FATURA_502", você salva a
combinação inteira como uma view. Ela vira privada (só sua) ou
compartilhada (aparece pro time). Cada tela que usa `FiltersBar` tem
seu próprio menu ★ Views — os snapshots são segregados por
`viewType` (ex.: `traces`) para não misturar filtros de contextos
diferentes.

## Onde fica

- Componente: `dashboard/src/components/SavedViewsMenu.tsx`
- Uso principal: dentro do `FiltersBar` em telas como `/traces`
- Endpoints:
  - `GET /v1/saved-views?viewType=` (lista)
  - `POST /v1/saved-views` (criar)
  - `PATCH /v1/saved-views/{id}` (renomear/mudar compartilhamento)
  - `DELETE /v1/saved-views/{id}` (remover)
- Papel mínimo:
  - **viewer/editor/admin**: criar view **privada**
  - **editor/admin**: marcar view como **compartilhada** (isShared)

## Como usar

1. Ajuste os filtros da tela até chegar no recorte desejado — só as
   chaves conhecidas do `FiltersBar` (range, app, release, type,
   feature, userId, route, search, onlyErrors) são persistidas.
2. Clique **★ Views → + Salvar filtros atuais como view**.
3. No modal, dê um **Nome** (ex.: "Erros 5xx do portal") e, se for
   editor/admin, marque **compartilhar com o time**. Viewer vê o
   aviso "Sua view fica privada. Editores e admins podem
   compartilhar com o time."
4. Depois de salva, a view aparece no dropdown em duas seções:
   **Minhas views** (com botão ✕ de remover) e **Compartilhadas pelo
   time** (só aplica; remoção reservada ao dono).
5. Clique numa view para aplicar — a `FiltersBar` popula os campos
   conhecidos e limpa os que a view não define.

## O que você vê

- **Botão ★ Views** no canto direito da barra, com contador de views
  disponíveis.
- **Dropdown** com atalho "+ Salvar filtros atuais como view" no
  topo, seguido das seções.
- Rótulo "· compartilhada" ao lado do nome quando `isShared = true`.
- Vazio: "Ainda não há views salvas — comece por 'salvar filtros
  atuais'."
- Erro (nome duplicado, por ex.): "Erro ao salvar (nome duplicado?)"
  em vermelho no modal.

## Como demonstrar

> "Cada time tem 3-4 recortes que abre 10 vezes ao dia. Aqui você
> salva o filtro com um nome, compartilha com o time, e o próximo
> plantonista já entra na tela certa. Zero configuração global —
> cada usuário monta o próprio conjunto."

Roteiro de 60s:
1. Em `/traces`, monte um filtro (7d + app X + onlyErrors).
2. Clique ★ Views → **+ Salvar filtros atuais como view**.
3. Nomeie "Plantão - erros portal 7d", marque compartilhar, salve.
4. Limpe os filtros, reabra o menu, aplique a view — mostre a URL
   voltando ao estado salvo.
5. Alterne de conta (login editor → login viewer) e mostre que a
   view compartilhada aparece pra ambos, mas o viewer não pode
   compartilhar as próprias.

## Papéis (RBAC)

- **viewer**: cria views privadas, aplica próprias e as
  compartilhadas do time, remove só as próprias.
- **editor/admin**: idem + marca `isShared` na criação/edição para
  publicar pro time (a checkbox de compartilhamento só aparece se
  `canShareViews(me)` é true).
- **Ninguém** remove view alheia — nem admin. É por design; se
  precisar limpar, chame a API direto no backend.

## Dependências

- `viewType` **segrega** o namespace no backend — cada tela chama
  `SavedViewsMenu` com seu próprio tipo. Hoje `traces` é o principal;
  novas telas com `FiltersBar` recebem tipos próprios.
- O snapshot salvo é **apenas as chaves conhecidas** (constante
  `VIEW_FILTER_KEYS` no `FiltersBar`) — cursor de paginação e outras
  props transientes ficam de fora, então aplicar a view não te joga
  no meio da lista.

## Perguntas frequentes

- **"Consigo editar uma view existente sem apagar?"**
  Renomear/mudar isShared, sim, via `PATCH /v1/saved-views/{id}`. A
  UI atual não expõe form de edição; aplique + salve com novo nome
  como workaround, ou chame a API.
- **"Uma view minha some quando eu perco o papel de editor?"**
  Não some — permanece privada. Se estava compartilhada, você não
  consegue mais marcar como shared em novas views, mas as antigas
  ficam.
- **"Views compartilhadas atravessam companies?"**
  Não. Elas respeitam o tenant scope, então só aparecem para membros
  da mesma company do dono.

## Referências

- Componente: `dashboard/src/components/SavedViewsMenu.tsx`
- Barra: `dashboard/src/components/FiltersBar.tsx` (constante
  `VIEW_FILTER_KEYS` e função `collectFilters`)
- Auth helper: `dashboard/src/lib/auth.ts` → `canShareViews`
- Handler: `backend/internal/adapter/httpapi/handlers.go` →
  `ListSavedViews`, `CreateSavedView`, `PatchSavedView`,
  `DeleteSavedView`
- Docs relacionadas: [traces.md](traces.md), [funnels.md](funnels.md).
