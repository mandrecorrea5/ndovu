# Sessões (`/sessions` e `/sessions/[id]`)

## Para que serve

Uma sessão é **uma janela contínua de uso** de um usuário no app
(fingerprint + userId + sessionId). Em vez de ler eventos soltos, aqui
você reconstitui o que a pessoa fez: por quais telas passou, o que
clicou, quais chamadas HTTP falharam. É a tela de troubleshooting
quando alguém abre um chamado — cola o `sessionId` e vê o filme
completo.

## Onde fica

- Rotas: `/sessions` (lista), `/sessions/[id]` (detalhe)
- Arquivos: `dashboard/src/app/sessions/page.tsx`,
  `dashboard/src/app/sessions/[id]/page.tsx`
- Endpoints: `GET /v1/sessions`, `GET /v1/sessions/{id}`
- Papel mínimo: qualquer papel autenticado

## Como usar

1. Em `/sessions`, ajuste **Período**, **App**, **Usuário (userId)** e
   marque **Com erros** para filtrar só as sessões que tiveram falha.
2. Ou informe **De/Até** em `datetime-local` para uma janela custom.
3. Clique **Consultar** — a tabela vira 25 linhas paginadas com
   sessionId, usuário, app, início/fim, duração, eventos, erros.
4. Clique no `sessionId` (link mono, truncado) para abrir o detalhe.
5. Na tela do detalhe, cada item da timeline é um botão que **expande
   o payload** — request/response body, error body, metadata,
   `receivedAt`.

## O que você vê

Lista (`/sessions`):
- Tabela: **Sessão**, **Usuário**, **App**, **Início**, **Fim**,
  **Duração**, **Eventos**, **Erros** (vermelho quando > 0).
- Paginação com **anterior/próxima** e contador `1–25 de N`.

Detalhe (`/sessions/[id]`):
- **7 tiles**: Usuário, App, Início, Fim, Duração, Eventos, Erros.
- **User agent** truncado com tooltip.
- **Timeline vertical** cronológica — trilho lateral, nó por evento
  (círculo vermelho quando falhou), rótulo `TYPE_META` (page_view,
  action, http_request, error…).
- Cada card mostra hora, tipo, nome, badge de status HTTP, duração e o
  primeiro `error.message` inline. Expande para JSON de request,
  response, error e metadata.

## Como demonstrar

> "Quando o usuário reclama, você quer o histórico completo, não
> evento a evento. Cola o sessionId e vê exatamente por onde ele passou,
> que HTTP falhou, o que o backend respondeu. Fim do 'não consegui
> reproduzir'."

Roteiro de 60s:
1. Em `/sessions`, marque **Com erros** e mostre a lista encurtar.
2. Abra uma sessão com erros — mostre os 7 tiles.
3. Percorra a timeline: aponte o nó vermelho.
4. Expanda o evento de erro e mostre `Error body`.
5. Volte, filtre por `userId` e mostre outra sessão do mesmo usuário.

## Papéis (RBAC)

- **viewer/editor/admin**: acesso completo à lista e ao detalhe. O
  tenant scope garante que só aparecem sessões dos apps da company do
  usuário.
- Nenhum papel altera sessão — dados imutáveis. Anotações e triagem
  ficam em [issues](issues.md).

## Dependências

- SDK enviando `sessionId` (obrigatório) e, idealmente, `userId` — sem
  userId a linha aparece como "anônimo".
- Se a sessão foi criada apenas via widget de feedback/snapshot sem
  captura de traces, a timeline vem vazia — a UI exibe um aviso
  explicando que os traces podem ter sido descartados por sampling
  adaptativo, e que feedback/snapshot seguem acessíveis pelos menus
  próprios.

## Perguntas frequentes

- **"Duração mostra negativa/zero, por quê?"**
  Sessão com um único evento tem `startedAt == lastEventAt`; a UI usa
  `Math.max(0, …)` para nunca mostrar negativo.
- **"Posso ver todas as sessões de um `userId` de uma vez?"**
  Sim — na lista, preencha **Usuário (userId)** e clique **Consultar**.
- **"Por que a lista limita a 25 por página?"**
  Para manter a resposta do backend rápida; a paginação usa
  `limit/offset`, então o total ainda é exato.

## Referências

- Lista: `dashboard/src/app/sessions/page.tsx`
- Detalhe: `dashboard/src/app/sessions/[id]/page.tsx`
- Componentes: `JsonView`, `TYPE_META`, `StatusBadge`, `ErrorPill`
- Handler: `backend/internal/adapter/httpapi/handlers.go` →
  `ListSessions`, `GetSession`
- Docs relacionadas: [traces.md](traces.md), [issues.md](issues.md).
