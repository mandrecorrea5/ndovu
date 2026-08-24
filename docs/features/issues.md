# Issues (`/issues` e `/issues/[fingerprint]`)

## Para que serve

Erro isolado é ruído; **erro agrupado** é problema acionável. A tela de
issues consolida cada erro pelo `fingerprint` — hash de
`app + tipo + code + mensagem + rota` — e mostra quantas vezes ocorreu,
quantos usuários afetou e qual o **impacto** (log(count) × usuários ×
recência). É a fila de trabalho do time: triagem, atribuição,
comentários, resolução.

## Onde fica

- Rotas: `/issues` (lista), `/issues/[fingerprint]` (drill)
- Arquivos: `dashboard/src/app/issues/page.tsx`,
  `dashboard/src/app/issues/[fingerprint]/page.tsx`
- Endpoints:
  - `GET /v1/issues`
  - `PATCH /v1/issues/{fingerprint}` (status, assignee)
  - `GET/POST/DELETE /v1/issues/{fingerprint}/comments`
- Papel mínimo:
  - **viewer**: leitura
  - **editor/admin**: alterar status, atribuir, comentar
  - **admin**: reatribuir para outros usuários (usa `GET /v1/users`)

## Como usar

1. Em `/issues`, ajuste **Período**, **App**, e as flags **só abertas**
   / **atribuídas a mim** — a lista atualiza a cada 30s.
2. A tabela lista cada issue com **Erro**, **App**, **Ocorrências**,
   **Usuários**, **Impacto**, **Última vez**, **Status** (aberta,
   investigando, resolvida, ignorada) e **Responsável**.
3. Botões inline (editor+): **resolver**, **reabrir**, **ignorar**;
   link **ver eventos** já leva a `/traces?onlyErrors=true` com o
   `code` da issue no `search`.
4. Clique na linha para abrir o drill. Ali: 4 tiles (Ocorrências,
   Usuários, Impacto, Última vez), painel de **Triagem** (status +
   responsável via dropdown), **Comentários** com textarea e
   **Últimas ocorrências** (20 eventos do fingerprint).
5. Botões do topo do drill: **criar no Jira**, **criar no Linear**,
   **copiar link** — pré-preenche o issue tracker com o link da issue
   no ndovu.

## O que você vê

- **Status pill** colorido (aberta = vermelho, investigando = amarelo,
  resolvida = verde, ignorada = cinza).
- **Impacto** — score numérico ajuda a priorizar quando há dezenas de
  issues.
- **Comentários** com autor, data, botão remover (só quem escreveu).
- **Aviso RBAC** — quando viewer, o form de comentário some e mostra
  "Apenas editores e administradores podem comentar."

## Como demonstrar

> "Sentry cobra por seat. Aqui é a mesma ideia: agrupa erros, dá
> priorização por impacto, permite atribuir, comentar, resolver — e
> ainda gera o link pronto para abrir ticket no Jira ou Linear. Tudo
> na sua VPS."

Roteiro de 60s:
1. Em `/issues`, mostre a lista ordenada por impacto.
2. Clique em uma issue — aponte os 4 tiles.
3. Mude o status pra **investigando** no dropdown.
4. Digite um comentário curto ("Reproduzível no Chrome 131") e envie.
5. Clique **criar no Jira** — mostre o link que abre com
   título/descrição prontos.
6. Clique **ver eventos** para saltar em `/traces` filtrado.

## Papéis (RBAC)

- **viewer**: lê tudo, mas não altera status, não atribui, não
  comenta.
- **editor**: altera status, comenta e é atribuível.
- **admin**: tudo do editor + pode **reatribuir** issues para outros
  (o dropdown de responsável só carrega a lista de usuários se o role
  for admin).

## Dependências

- Fingerprint é calculado pelo backend na ingestão de eventos com
  `error`. Sem `error.code` ou `error.message`, cai em um bucket
  genérico.
- **criar no Jira** / **criar no Linear** dependem de as URLs base
  estarem configuradas em `dashboard/src/lib/tracker.ts` (env
  `NEXT_PUBLIC_JIRA_URL`, `NEXT_PUBLIC_LINEAR_URL`); se não há
  configuração, o botão simplesmente não aparece.
- Comentários e alterações escrevem no Postgres do backend
  (`issue_state`, `issue_comments`).

## Perguntas frequentes

- **"O que é o score de impacto?"**
  `log(count) × affectedUsers × recência`. Ele evita que um erro
  antigo com 1M ocorrências afogue um erro novo que já pegou 200
  usuários.
- **"Por que a issue sumiu depois que resolvi?"**
  A flag **só abertas** vem marcada por default. Desmarque para ver
  resolvidas/ignoradas.
- **"Se o mesmo erro aparecer em outra release, vira issue nova?"**
  Não — release não entra no fingerprint. Use [releases.md](releases.md)
  para comparar antes/depois.

## Referências

- Lista: `dashboard/src/app/issues/page.tsx`
- Drill: `dashboard/src/app/issues/[fingerprint]/page.tsx`
- Auth helpers: `dashboard/src/lib/auth.ts` (`canEdit`)
- Tracker links: `dashboard/src/lib/tracker.ts`
- Handler: `backend/internal/adapter/httpapi/handlers.go` →
  `ListIssues`, `PatchIssue`, `PostIssueComment`
- Docs relacionadas: [traces.md](traces.md), [releases.md](releases.md).
