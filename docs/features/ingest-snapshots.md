# Snapshots de sessão (`POST /v1/snapshots`)

## Para que serve

Captura uma **foto do HTML** exatamente no momento em que um erro
aconteceu no frontend, para que o dev abra a issue no dashboard e
veja **o que o usuário via na tela**. É o "session replay MVP" do
Ndovu — não grava vídeo, grava uma foto do DOM (rápido, barato,
funciona sem instrumentação extra).

Habilitado automaticamente pelo SDK browser com
`captureSnapshots: true` (default): cada `error()` dispara o upload
em fire-and-forget.

## Onde fica

- Endpoints:
  - `POST /v1/snapshots` — upload (auth `X-Api-Key`).
  - `GET  /v1/snapshots/event/{eventId}` — metadata (auth Bearer).
  - `GET  /v1/snapshots/event/{eventId}/html` — HTML renderizado em
    iframe sandbox (auth Bearer, `Content-Type: text/html`).
- Handler: `backend/internal/adapter/httpapi/handlers.go` →
  `PostSnapshot`, `GetSnapshot`, `GetSnapshotMeta`.
- Serviço: `backend/internal/usecase/snapshots.go` (`SnapshotService`).
- SDK (montagem/sanitização do HTML): `sdk/ndovu-browser.js` →
  `captureDOM()` e `sendSnapshot()`.
- Blob storage: S3-compatível (MinIO local ou AWS S3).
- Env vars:
  - `NDOVU_S3_ENDPOINT` — endereço do MinIO/S3. **Vazio desliga**
    o serviço; o POST devolve `503`.
  - `NDOVU_S3_ACCESS_KEY`, `NDOVU_S3_SECRET_KEY`, `NDOVU_S3_USE_SSL`.
  - `NDOVU_SNAPSHOT_BUCKET` (default `ndovu-snapshots`).

## Como usar

Automático via SDK — não precisa código adicional:

```ts
const ndovu = createNdovu({
  endpoint: 'https://ndovu.exemplo.com',
  apiKey: '<chave>',
  app: 'portal-cliente',
  captureSnapshots: true, // default
});
ndovu.error('falha_pagamento', { code: 'GATEWAY_TIMEOUT', message: '...' });
// SDK envia POST /v1/events E POST /v1/snapshots com o mesmo eventId
```

Marcar elementos com PII fora de inputs (o snapshot mascara
`textContent`):

```html
<span data-ndovu-mask>CPF: {user.cpf}</span>
```

Upload manual via curl (para testar o endpoint):

```bash
EID="0d9e0a44-8c2b-4c33-a1f2-7e6b5d4c3b2a"
curl -sS -X POST http://localhost:18081/v1/snapshots \
  -H 'Content-Type: application/json' \
  -H 'X-Api-Key: dev-ingest-key' \
  -d "{
    \"eventId\": \"$EID\",
    \"sessionId\": \"b7f9c2e0-4d1a-4f3b-9c8d-2e5a7b1c3d4f\",
    \"app\": \"portal-cliente\",
    \"html\": \"<!doctype html><html><body><h1>Página do erro</h1></body></html>\",
    \"url\": \"https://app.com/checkout\",
    \"viewportW\": 1920, \"viewportH\": 1080,
    \"takenAt\": \"2026-08-24T14:32:11Z\"
  }"
```

Recuperar (dashboard faz isso ao abrir a issue):

```bash
TOKEN="eyJ..."
curl -sS "http://localhost:18081/v1/snapshots/event/$EID/html" \
  -H "Authorization: Bearer $TOKEN" \
  -o snapshot.html
open snapshot.html
```

## O que acontece

1. `apiKeyAuth` valida chave + rate limit (mesmo caminho da ingestão
   de eventos).
2. Cross-tenant guard: `req.App` precisa bater com `key.App` → `400`
   se diverge.
3. Se `SnapshotService` está desligado (sem `NDOVU_S3_ENDPOINT`),
   devolve `503` com mensagem clara — não trava a app, só sinaliza.
4. `SnapshotService.Save` valida obrigatórios (`eventId`, `sessionId`,
   `app`, `html` não-vazio), limite de 20 MB no HTML original.
5. **Gzip** do HTML (razão típica 5-10x). Salva o blob em
   `<ano>/<mes>/<dia>/<eventId>-<rand>.html.gz` no bucket S3.
6. Salva metadata em Postgres (`session_snapshots`): `eventId` (PK
   com dedup), `sessionId`, `app`, `objectKey`, `sizeBytes`,
   viewport, url, `takenAt`.
7. Retorna `202 Accepted` + metadata.

Recuperação:
- `GET /v1/snapshots/event/{eventId}` — metadata para o dashboard
  decidir se mostra o iframe.
- `GET /v1/snapshots/event/{eventId}/html` — gunzip + envia como
  `text/html` com CSP defensivo (`default-src 'none'; img-src * data:;
  style-src 'unsafe-inline' *; font-src *`) e
  `X-Content-Type-Options: nosniff`. O dashboard renderiza em
  `<iframe sandbox>`, defesa em profundidade.

Sanitização do lado do SDK (feita **antes** de enviar):
- Remove `<script>` e `<iframe>` do clone.
- Mascara `value` de inputs sensíveis (`type=password|email|tel|
  cc-number|cc-csc` ou `data-ndovu-mask`).
- Substitui `textContent` de qualquer elemento com `data-ndovu-mask`
  por `***`.

Tamanho típico: **5-50 KB gzipped** (HTML de app real varia
30-500 KB descomprimido).

## Como demonstrar

> "Quando o usuário reporta 'clico e não acontece nada', em vez de
> pedir print, você abre a issue e vê exatamente o HTML que estava
> na tela quando quebrou — inputs mascarados, scripts removidos,
> pronto pra inspecionar no devtools."

Roteiro de 60s:
1. Rode o curl de upload acima com um HTML de exemplo.
2. Rode o curl de download com o token do admin — abra o
   `snapshot.html` no browser.
3. Faça uma issue nova pelo SDK: force um erro em uma tela real e
   mostre o snapshot no drill da issue.
4. Mostre um `<input type="password">` no snapshot com `value="***"`
   — sanitização do SDK.
5. Fale que o storage é S3-compatível: em dev usa MinIO local, em
   produção aponta pra AWS S3 sem mudar código.

## Papéis (RBAC)

- **Upload** (`POST /v1/snapshots`) — auth por `X-Api-Key`; guard de
  app garante que a chave só sobe snapshot do próprio app.
- **Leitura** (`GET .../event/{id}` e `.../html`) — auth Bearer +
  `tenantScope`. Só usuários com acesso ao app conseguem ver o
  snapshot (mesmo scope das outras queries do dashboard).

## Dependências

- MinIO/S3 configurado (`NDOVU_S3_ENDPOINT` não-vazio); sem isso, os
  endpoints devolvem `503` e o SDK trata como best-effort (nada quebra).
- Bucket criado (`NDOVU_SNAPSHOT_BUCKET`; MinIO local cria via
  `docker-compose` do repo).
- Postgres com a migração de `session_snapshots`.
- Evento `error` já ingerido: o `eventId` do snapshot amarra a foto
  ao evento — sem o evento correspondente, o dashboard não sabe
  onde exibir.

## Perguntas frequentes

- **"E se o HTML for enorme?"** Limite prático de 20 MB de HTML
  bruto; acima disso o `Save` devolve validação. Depois do gzip
  típico de 5-10x, cabe em blob normal.
- **"Como desligo em produção?"** Duas formas: (a) deixe
  `NDOVU_S3_ENDPOINT` vazio — o backend responde `503` e o SDK
  ignora; (b) no SDK, `captureSnapshots: false`.
- **"Snapshot renderiza scripts do meu site?"** Não. O SDK remove
  `<script>` e o dashboard usa `<iframe sandbox>` + CSP proibindo
  scripts. É defesa em profundidade.
- **"Posso ver snapshots de outra empresa?"** Não. Leitura passa por
  `tenantScope`; só quem tem acesso ao `app` vê. Upload passa pelo
  guard `app-da-chave`.
- **"Snapshot fica pra sempre?"** Hoje sim (sem TTL automático). Em
  produção, política de lifecycle do bucket S3 é o caminho
  recomendado.

## Referências

- Handlers: `backend/internal/adapter/httpapi/handlers.go`
  (`PostSnapshot`, `GetSnapshot`, `GetSnapshotMeta`)
- Serviço: `backend/internal/usecase/snapshots.go`
- SDK: `sdk/ndovu-browser.js` → `captureDOM`, `sendSnapshot`
- Contrato: `docs/CONTRACT.md` §2
- Docs relacionadas: [sdk-browser.md](sdk-browser.md),
  [ingest-eventos.md](ingest-eventos.md),
  `docs/features/issues.md` (quando existir).
