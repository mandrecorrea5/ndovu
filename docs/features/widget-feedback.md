# Widget de feedback (`POST /v1/feedbacks`)

## Para que serve

Fecha o loop "usuário reclamou → dev abriu no dashboard". Um botão
flutuante no seu app permite que o usuário final envie **bug,
sugestão, elogio ou comentário livre**, atrelado automaticamente à
sessão e ao **último eventId visto** — que é o gancho para o dev
navegar direto pro evento/erro que motivou a queixa.

Zero dependência no frontend: o widget é montado pelo próprio SDK.

## Onde fica

- Endpoint público (SDK): `POST /v1/feedbacks` (auth `X-Api-Key`).
- Endpoints admin (dashboard):
  - `GET    /v1/admin/feedbacks` — lista paginada.
  - `PATCH  /v1/admin/feedbacks/{id}` — muda status
    (`new|reviewing|resolved|spam`).
  - `DELETE /v1/admin/feedbacks/{id}` — apaga (útil pra spam).
- Handler: `backend/internal/adapter/httpapi/handlers.go` →
  `PostFeedback`, `GetFeedbacks`, `PatchFeedback`, `DeleteFeedback`.
- Serviço: `backend/internal/usecase/feedback.go` (`FeedbackService`).
- SDK — API e widget: `sdk/ndovu-browser.js` → `sdk.feedback(...)`,
  `sdk.mountFeedbackWidget(...)`.
- Contrato: `docs/CONTRACT.md` §3.

## Como usar

Montar o widget no seu app (opcional — o `feedback()` programático
funciona sem o widget):

```ts
import { createNdovu } from './vendor/ndovu-browser.js';

const ndovu = createNdovu({
  endpoint: 'https://ndovu.exemplo.com',
  apiKey: '<chave-do-app>',
  app: 'portal-cliente',
});

// Botão flutuante + modal (bottom-right por default)
const unmount = ndovu.mountFeedbackWidget({
  position: 'bottom-right',
  label: 'Feedback',
  title: 'Enviar feedback',
  placeholder: 'Descreva o que aconteceu…',
});
```

Envio programático (por exemplo, num CTA próprio):

```ts
await ndovu.feedback('O botão de pagamento não funciona no checkout', {
  type: 'bug',                   // bug | suggestion | praise | other
  email: 'cliente@exemplo.com',  // opcional
  // eventId: opcional — se omitir, o SDK usa o último eventId gerado
});
```

Envio direto via curl (útil pra teste do endpoint):

```bash
curl -sS -X POST http://localhost:18081/v1/feedbacks \
  -H 'Content-Type: application/json' \
  -H 'X-Api-Key: dev-ingest-key' \
  -d '{
    "app": "portal-cliente",
    "sessionId": "b7f9c2e0-4d1a-4f3b-9c8d-2e5a7b1c3d4f",
    "eventId": "0d9e0a44-8c2b-4c33-a1f2-7e6b5d4c3b2a",
    "type": "bug",
    "message": "Botão de pagar não funciona",
    "email": "cliente@exemplo.com",
    "url": "https://app.com/checkout",
    "viewportW": 1920, "viewportH": 1080
  }'
```

Resposta:

```
HTTP/1.1 202 Accepted
{"id":"fb_...", "status":"new", "createdAt":"2026-08-24T14:32:15Z", ...}
```

## O que acontece

**Ingestão (público, X-Api-Key)**
1. `apiKeyAuth` valida a chave; rate limit por chave.
2. Cross-tenant guard: `req.App` precisa bater com `key.App` → `400`.
3. `FeedbackService.Ingest` valida:
   - `app`, `sessionId`, `message` obrigatórios.
   - `type` default = `bug`; deve ser `bug|suggestion|praise|other`.
   - `message` até 5000 caracteres (excede → `400`).
4. Grava em Postgres com `status=new`.
5. Retorna `202` + objeto criado (id, createdAt, status).

**Triagem (admin, Bearer)**
- `GET /v1/admin/feedbacks?limit=&offset=&status=&app=` — lista
  paginada; `tenantScope` filtra por company do actor.
- `PATCH /v1/admin/feedbacks/{id}` — muda status; grava `resolvedBy`
  com o user do token e registra em `audit_log`.
- `DELETE` — apaga; também vai pra `audit_log`.

**Contexto anexado (feito pelo SDK)**
- `sessionId`: sempre — permite abrir a sessão no dashboard.
- `eventId`: o SDK guarda o **último eventId gerado** em `lastEventId`
  e envia por padrão. Efeito: se o usuário clica "Feedback" logo
  após um erro, o dev vê a queixa **já linkada ao erro**.
- `url`, `viewportW`, `viewportH`: contexto ambiental.

## Como demonstrar

> "Botão flutuante do lado do produto, POST no Ndovu, e no
> `/admin/feedbacks` aparece a mensagem já linkada ao último
> `eventId` da sessão — se foi bug logo depois de um erro, o dev
> clica e cai direto no drill do erro."

Roteiro de 60s:
1. Abra o app com o widget montado. Clique em "Feedback",
   escolha "Bug", digite "não consigo pagar", envie.
2. Vá em `/admin/feedbacks` — aparece a linha nova.
3. Clique no `eventId` do feedback → cai no drill do evento (erro
   que aconteceu antes), com snapshot, breadcrumbs, tudo.
4. Marque como "reviewing" — mostre o audit log registrando a mudança.
5. Fale que é a mesma `X-Api-Key` do ingest de eventos (não precisa
   configurar chave nova).

## Papéis (RBAC)

- **Envio** — não tem role (usuário final anônimo do produto);
  auth por `X-Api-Key`, guard de app.
- **Listagem/PATCH/DELETE** — `bearerAuth` + `requireRole(admin)` +
  `tenantScope`. Admin de company vê só feedbacks dos apps da
  própria company; super-admin vê tudo. `PatchFeedback` e
  `DeleteFeedback` bloqueiam se o feedback pertence a app fora do
  scope (via `enforceAppInScope`).

## Dependências

- Postgres com a migração de `user_feedbacks`.
- Chave `X-Api-Key` do app válida (mesma da ingestão de eventos).
- (Opcional mas recomendado) SDK montado com um `error()` ou
  `action()` recente, para `lastEventId` estar preenchido — sem
  isso, o feedback chega sem link para evento (ainda tem sessionId).

## Perguntas frequentes

- **"Preciso de uma chave separada pro widget?"** Não. Use a mesma
  chave do app (uma chave = um app; ela vale para eventos,
  snapshots e feedbacks).
- **"O usuário precisa estar logado no produto?"** Não. O widget
  funciona anônimo. `sessionId` amarra à sessão; `email` é opcional
  e vem digitado no formulário.
- **"Como filtro spam?"** `PATCH` para `status=spam` e/ou `DELETE`.
  Fica em `audit_log`. Anti-abuse mais forte fica por conta do
  rate limit da chave (`NDOVU_INGEST_RATE_RPS`) e de camadas na
  frente (Cloudflare, reCAPTCHA no seu app, etc.).
- **"O widget é acessível?"** Modal minimalista com foco no
  `<textarea>`, labels ligados, botão de cancelar por ESC/click no
  backdrop. Zero dependência = zero CSS externo. Se precisar
  customizar visual, monte o próprio botão e chame `ndovu.feedback()`.
- **"Posso enviar sem `eventId`?"** Pode. O campo é opcional. Sem
  ele, o feedback fica ligado só à sessão.

## Referências

- Handlers: `backend/internal/adapter/httpapi/handlers.go`
  (`PostFeedback`, `GetFeedbacks`, `PatchFeedback`, `DeleteFeedback`)
- Serviço: `backend/internal/usecase/feedback.go`
- Widget/SDK: `sdk/ndovu-browser.js` → `mountWidget`, `sdk.feedback`
- Contrato: `docs/CONTRACT.md` §3
- Docs relacionadas: [sdk-browser.md](sdk-browser.md),
  [ingest-eventos.md](ingest-eventos.md),
  [ingest-snapshots.md](ingest-snapshots.md),
  `docs/features/admin-feedbacks.md` (quando existir).
