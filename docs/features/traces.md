# Explorador de traces (`/traces`)

## Para que serve

A tela em que você **procura um evento específico** entre milhões.
Quando um usuário relata que "clicou no botão e nada aconteceu", ou
quando o suporte precisa confirmar se uma request chegou ao backend, é
aqui que se vem. Todos os eventos capturados pelo SDK — page views,
ações, HTTP, erros, web vitals — entram numa mesma tabela filtrável.
Cada linha expande e mostra o payload cru.

## Onde fica

- Rota: `/traces`
- Arquivo: `dashboard/src/app/traces/page.tsx`
- Endpoint: `GET /v1/events` (com cursor de paginação `nextCursor`)
- Papel mínimo: qualquer papel autenticado

## Como usar

1. Abra `/traces` — a janela default é **24h** e mostra os eventos mais
   recentes, 50 por página.
2. Combine filtros na barra do topo: **Período**, **App**, **Tipo**,
   **Funcionalidade**, **Usuário (userId)**, **Rota (endpoint)**,
   **Busca livre**, checkbox **Somente erros**.
3. Toda mudança de filtro vira querystring (`?range=7d&app=…`) — a URL
   é o estado; compartilhe o link para reproduzir a mesma consulta.
4. Clique **Consultar** para reforçar a aplicação ou **limpar** para
   zerar. Use **Views** (★) para salvar a combinação atual como
   [saved view](saved-views.md).
5. Clique em qualquer linha para expandir e ver `request/response body`,
   erro cru e metadata. Use **Carregar mais** para paginar via cursor.

## O que você vê

- **Barra de filtros** com dropdowns, inputs e o menu **Views**.
- **Tabela de eventos** (min-width 900px): timestamp, tipo, nome, rota,
  duração, status HTTP, badge de erro.
- **Contador** "N eventos carregados" e botão **Carregar mais** enquanto
  houver `nextCursor` — quando acabar aparece "fim dos resultados".
- **Estados**: `LoadingState`, `ErrorState`, `EmptyState` ("Nenhum
  evento com esses filtros. Ajuste o período ou limpe os filtros.").

## Como demonstrar

> "Essa é a lupa. Se o cliente diz 'travou às 14:32 quando cliquei
> Salvar', em três filtros — período, userId, somente erros — eu chego
> no evento exato, expando e vejo o payload que o backend recebeu.
> Nenhum servidor separado, nenhum ELK: os dados já estão indexados."

Roteiro de 60s:
1. Abra `/traces` sem filtro e mostre o volume que passa.
2. Filtre por **App** e por **Tipo = error**; conte os erros no tile.
3. Cole um `userId` de exemplo e mostre só o rastro dele.
4. Expanda uma linha de erro — mostre `Error body` e `Metadata`.
5. Copie a URL na barra do navegador e destaque que os filtros vivem
   ali — link compartilhável com o time.
6. Clique **Views → Salvar filtros atuais como view** para prometer
   o retorno em um clique.

## Papéis (RBAC)

- **viewer/editor/admin**: enxergam a tabela e todos os filtros; o
  tenant scope garante que só aparecem eventos de apps aos quais o
  usuário tem acesso.
- Nenhum papel edita evento — traces são imutáveis por design. Ações
  de escrita (resolver, comentar) ficam em [issues](issues.md).

## Dependências

- Precisa de dados ingeridos via SDK ou seed
  (`node tools/seed/seed.mjs`).
- Filtros de dropdown (`App`, `Tipo`, `Funcionalidade`) vêm do endpoint
  `GET /v1/events/filters` — se um valor não aparece, é porque ninguém
  o enviou ainda na janela.
- Views compartilhadas dependem do serviço de [saved views](saved-views.md).

## Perguntas frequentes

- **"Filtrei e a tabela não recarrega."**
  A tabela reage a mudanças de querystring — se o botão **Consultar**
  não foi apertado depois de digitar em `userId`, `route` ou `search`,
  aperte enter no campo ou clique **Consultar**.
- **"Por que a paginação usa 'carregar mais' e não páginas?"**
  O backend devolve `nextCursor` (keyset pagination) — mais estável
  para tabelas que crescem em tempo real; offset ficaria fora do lugar
  a cada evento novo.
- **"Consigo exportar CSV?"**
  Ainda não. Salve a URL da consulta ou uma view e reproduza a query
  via `GET /v1/events` para exportar por script.

## Referências

- Página: `dashboard/src/app/traces/page.tsx`
- Barra: `dashboard/src/components/FiltersBar.tsx`
- Linha: `dashboard/src/components/EventRow.tsx`
- Menu de views: `dashboard/src/components/SavedViewsMenu.tsx`
- Handler: `backend/internal/adapter/httpapi/handlers.go` → `ListEvents`
- Docs relacionadas: [saved-views.md](saved-views.md),
  [sessoes.md](sessoes.md), [issues.md](issues.md).
