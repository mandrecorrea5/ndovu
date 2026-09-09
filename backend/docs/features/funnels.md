# Funis de conversão (`/funnels`)

## Para que serve

Um funil responde: **de 100 pessoas que abriram a tela X, quantas
chegaram até Y?** — e, quando caem, **onde caem?** O ndovu monta funis
sobre os eventos que o SDK já envia (page_view, action, http_request,
error, custom). Você define uma sequência de passos, cada passo é um
matcher AND (tipo obrigatório + name/screen/feature/httpUrl opcionais),
e a UI mostra sessões que passaram por cada passo, taxa de conversão
acumulada e drop-off entre passos.

## Onde fica

- Rota: `/funnels`
- Arquivo: `dashboard/src/app/funnels/page.tsx`
- Endpoints:
  - `GET /v1/funnels` (lista)
  - `POST /v1/funnels` (criar) · `PATCH /v1/funnels/{id}` (editar) ·
    `DELETE /v1/funnels/{id}` (remover)
  - `GET /v1/funnels/{id}/results?from=&to=`
- Papel mínimo:
  - **viewer**: listar e ver resultados
  - **editor/admin**: criar, editar, remover

## Como usar

1. Em `/funnels`, escolha **App** e **Período** no topo. A coluna da
   esquerda lista todos os funis do app.
2. Clique um funil — direita mostra 3 tiles (**Sessões no passo 1**,
   **Chegaram no último**, **Conversão total**) e o painel **Drop-off
   por passo** com barras horizontais e contadores por passo.
3. Editor/admin: clique **+ Novo funil** para abrir o modal do editor
   — informe **App** (não editável em edição), **Nome**, **Janela
   (segundos)** (tempo entre passo 1 e o último; mínimo 60).
4. Adicione entre **2 e 8 passos**. Cada passo tem **Rótulo**, **Tipo
   de evento** (page_view / action / http_request / error / custom) e
   opcionais **name**, **screen**, **feature**, **httpUrl contains**
   — todos combinados com AND.
5. **salvar** cria/atualiza. **remover** exige confirmação.

## O que você vê

- **Sidebar** com nome, app e janela (`ex.: janela 1800s`) de cada
  funil.
- **Header do funil selecionado** com botões **editar** / **remover**
  (só editor+).
- **Barra de conversão** por passo, cor accent, largura proporcional
  a `overallRate`. Drop-off inline em vermelho ("↓ perdeu N") quando
  o passo perde sessões em relação ao anterior; verde "(sem drop)"
  quando manteve.
- **Modal de editor** com validação inline: erros da API aparecem em
  `text-critical` (ex.: nome duplicado, janela abaixo do mínimo).
- Vazio (viewer): "Nenhum funil cadastrado. Peça a um editor ou admin
  para criar." Vazio (editor+): "Nenhum funil ainda. Comece pelo
  botão 'Novo funil'."

## Como demonstrar

> "Onboarding, checkout, ativação — todo mundo tem um funil crítico.
> Em 30 segundos eu monto um: page_view '/login' → action
> 'clicou_confirmar' → http_request '/api/checkout'. A UI mostra na
> hora onde 40% dos usuários somem. E é totalmente self-service para
> o editor — nada de pedir SQL pro time de dados."

Roteiro de 60s:
1. Selecione um funil existente — mostre as barras e o drop-off.
2. Clique **+ Novo funil**, dê nome, escolha app, deixe janela 1800s.
3. Passo 1: `page_view` com `screen = /produto`. Passo 2: `action`
   com `name = adicionar_carrinho`. Passo 3: `http_request` com
   `httpUrl = /api/pedido`.
4. Salve e clique no funil recém-criado.
5. Mostre a conversão total no tile e o drop-off da barra.

## Papéis (RBAC)

- **viewer**: lista e resultados; sem botão "Novo funil", sem editar,
  sem remover.
- **editor/admin**: tudo — criar, editar (nome/janela/steps; app é
  imutável em edição), remover (com confirm).

## Dependências

- Precisa de eventos com `type`, `name`, `screen`, `feature`,
  `http.url` populados conforme o passo pede. Se o matcher não
  encontra nada, o funil retorna 0 sessões no passo — não é erro, é
  ausência de dado.
- Backend calcula sessões que satisfazem cada passo **na ordem** e
  dentro da **janela** — sessão que faz passo 3 antes do passo 2 não
  conta.

## Perguntas frequentes

- **"Por que o app não pode mudar na edição?"**
  Fingerprints e agrupamentos são por app; trocar o app romperia a
  série histórica. Crie um funil novo se precisar migrar.
- **"Qual janela usar?"**
  Ação instantânea (login → home) cabe em 300s. Checkout com
  aprovação bancária pode precisar de 3600s+.
- **"Como faço um passo 'clicou botão X'?"**
  No SDK, dispare `ndovu.track('clicou_botao_x', { feature: 'checkout' })`
  — no matcher, `type = action`, `name = clicou_botao_x` (e opcional
  `feature = checkout`).

## Referências

- Página: `dashboard/src/app/funnels/page.tsx`
- Modal: `dashboard/src/components/Modal.tsx`
- Auth: `dashboard/src/lib/auth.ts` (`canEdit`)
- Handler: `backend/internal/adapter/httpapi/handlers.go` →
  `ListFunnels`, `CreateFunnel`, `UpdateFunnel`, `DeleteFunnel`,
  `FunnelResults`
- Docs relacionadas: [retention.md](retention.md),
  [saved-views.md](saved-views.md).
