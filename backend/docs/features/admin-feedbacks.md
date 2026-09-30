# Feedback do usuário (`/admin/feedbacks`)

## Para que serve

Uma inbox de mensagens que o **end-user** enviou pelo widget do SDK
(`sdk.mountFeedbackWidget()`) — bug, sugestão, elogio ou outro. Cada
feedback traz **sessionId** e **último eventId** vistos, permitindo
pular direto para a sessão + timeline (session replay quando
disponível). É o ponto onde produto e engenharia entram em contato
com a voz real do usuário sem sair do dashboard.

## Onde fica

- Rota: `/admin/feedbacks`
- Arquivo: `dashboard/src/app/admin/feedbacks/page.tsx`
- Endpoints admin:
  - `GET /v1/admin/feedbacks?status=&app=&limit=&offset=`
  - `PATCH /v1/admin/feedbacks/{id}` (troca status)
  - `DELETE /v1/admin/feedbacks/{id}`
- Endpoint público (chega via widget SDK): `POST /v1/feedbacks` com
  header `X-Api-Key` do app emissor
- Papel mínimo: **admin**

## Como usar

1. Abra **Feedback do usuário**. Filtros no topo: **Status** (default
   `novos`) e **App** (default todos). Refresh automático a cada 60s.
2. Cada feedback vira um **card** com:
   - Ícone por tipo (`🐞 bug`, `💡 suggestion`, `❤ praise`, `• other`).
   - Metadados (app, tipo, status, data).
   - **Mensagem** do usuário (preserva quebras).
   - E-mail (link `mailto:`), URL da página (link externo), viewport.
   - Rodapé: link "ver sessão + timeline", `event <id>…` e ações.
3. Ações no card:
   - **triagem** — marca `triaging`.
   - **resolver** — marca `resolved`.
   - **descartar** — marca `dismissed`.
   - **remover** — deleta (com confirmação).
4. Use os filtros para percorrer só os `novos`, ou isolar um app
   específico durante uma release problemática.

## O que você vê

- Cabeçalho: "Mensagens enviadas pelo widget do SDK
  (bug/sugestão/elogio). Cada feedback traz o sessionId e o último
  eventId — clique pra ir direto na sessão ou no replay do erro que
  motivou."
- Filtros: Status (`novos`, `em triagem`, `resolvidos`, `descartados`,
  `todos`) + App + contagem à direita (`N feedbacks`).
- Cards com tons semânticos:
  - **novo**: `● novo` em vermelho.
  - **triagem**: `◐ triagem` em amarelo.
  - **resolvido**: `✓ resolvido` em verde.
  - **descartado**: `✕ descartado` cinza.
- Estado vazio: "Nenhum feedback com os filtros atuais. Monte o
  widget nas apps com `sdk.mountFeedbackWidget()`."

## Como demonstrar

> "Cliente clica no botão flutuante, escreve 'não consigo finalizar
> compra', envia. Em segundos aparece aqui, com a sessão e o último
> evento anexos. Um clique em 'ver sessão + timeline' e você vê o que
> o usuário fez até bater no problema — sem precisar de suporte por
> e-mail."

Roteiro de 60s:
1. No portal demo, clique no widget flutuante e envie um bug com
   e-mail preenchido.
2. Volte no dashboard — o feedback já está no topo com status `novo`.
3. Aponte para a URL (link externo), o viewport (ex.: `375×812`) e o
   `event xxxxxxxx…` no rodapé.
4. Clique **ver sessão + timeline** — abre a sessão do usuário. Mostre
   o erro que ele viu.
5. Volte, clique **triagem** e depois **resolver** — o card muda de
   cor. Filtre por `resolvidos` para provar persistência.

## Papéis (RBAC)

- **admin only**. Editor/viewer não veem o link.
- Admin de Company A só vê feedbacks dos apps de A —
  `tenantScope` filtra tanto listagem quanto PATCH/DELETE.
- `is_super` vê feedbacks cross-tenant.

## Dependências

- **Widget** precisa estar montado no frontend
  (`sdk.mountFeedbackWidget({ appKey, position })`). Sem widget, sem
  feedback.
- Cada feedback carrega `sessionId` e `eventId` — para o link "ver
  sessão + timeline" funcionar com contexto rico, é preciso que o SDK
  esteja emitindo eventos com session tracking.
- **Session replay** (opcional) enriquece a timeline com snapshots do
  DOM quando disponível.

## Perguntas frequentes

- **"Feedback fica anônimo?"**  
  Se o usuário não preencher e-mail, sim. Mas `sessionId`, URL,
  viewport e último `eventId` sempre são anexados.
- **"Posso responder pelo dashboard?"**  
  Não. O card tem link `mailto:` para responder por e-mail direto.
- **"Diferença entre `dismissed` e `remover`?"**  
  `dismissed` marca como resolvido sem ação; o registro fica auditável.
  `remover` apaga do banco — irreversível.
- **"Feedback conta para LGPD?"**  
  Sim. Contém `userId` (se logado), e-mail e mensagem — está sujeito
  a export/forget via [`/admin/gdpr`](admin-gdpr.md).

## Referências

- Página: `dashboard/src/app/admin/feedbacks/page.tsx`
- API client: `dashboard/src/lib/api.ts` → `listFeedbacks`,
  `setFeedbackStatus`, `deleteFeedback`
- Widget SDK: `sdk/src/feedback-widget` (consumidor de
  `POST /v1/feedbacks` com `X-Api-Key`)
- Docs relacionadas: [admin-gdpr.md](admin-gdpr.md),
  [visao-geral.md](visao-geral.md)
