# Ingestão de eventos (`POST /v1/events`)

## Para que serve

É a **única** porta de escrita de traces do Ndovu. Todo evento —
page_view, action, http_request, error, custom — entra por aqui, em
lotes, autenticado por `X-Api-Key`. Uma chave = um app.

Projetado para latência mínima (responde `202` sem esperar disco) e
para ser idempotente (reenvio de lote não duplica).

## Onde fica

- Endpoint: `POST /v1/events` (auth `X-Api-Key`).
- Handler: `backend/internal/adapter/httpapi/handlers.go` →
  `PostEvents`.
- Serviço: `backend/internal/usecase/ingest.go` (`IngestService.Ingest`).
- Middleware da chave: `backend/internal/adapter/httpapi/middleware.go`
  → `apiKeyAuth` (cache + rate limit por chave).
- Fila durável: `backend/internal/adapter/natsstream/stream.go` —
  publica com `Nats-Msg-Id` determinístico.
- Contrato: `docs/CONTRACT.md` §1.
- Env vars:
  - `NDOVU_MAX_BODY_BYTES` (default 1 MB) → 413 se estourar.
  - `NDOVU_INGEST_RATE_RPS` (default 50/s/chave) → 429 com
    `Retry-After: 1`.
  - `NDOVU_KEY_CACHE_TTL_SECONDS` (default 30s) — validação da chave
    cacheada; revogação leva no máximo esse tempo pra virar 401.
  - `NDOVU_STREAM_MAX_AGE_HOURS` (default 48h) — retenção do buffer
    NATS.

## Como usar

Envio mínimo via curl (envelope + 1 evento):

```bash
curl -sS -X POST http://localhost:18081/v1/events \
  -H 'Content-Type: application/json' \
  -H 'X-Api-Key: dev-ingest-key' \
  -d '{
    "app": "portal-cliente",
    "sdkVersion": "curl-demo",
    "session": {
      "sessionId": "b7f9c2e0-4d1a-4f3b-9c8d-2e5a7b1c3d4f",
      "userId": "user-42",
      "attributes": { "release": "1.0.0" }
    },
    "events": [{
      "eventId": "0d9e0a44-8c2b-4c33-a1f2-7e6b5d4c3b2a",
      "type": "action",
      "name": "clicou_botao_teste",
      "feature": "demo",
      "screen": "/",
      "timestamp": "2026-08-24T14:32:11Z",
      "metadata": { "origem": "curl" }
    }]
  }'
```

Resposta esperada:

```
HTTP/1.1 202 Accepted
{"accepted": 1}
```

Tipos aceitos em `events[].type`:

| type          | quando usar |
|---------------|-------------|
| `page_view`   | mudança de rota / tela |
| `action`      | interação do usuário (clique, submit, custom) |
| `http_request`| chamada HTTP (SDK envia com objeto `http`) |
| `error`       | erro JS, HTTP falho, unhandled rejection |
| `custom`      | tudo mais (métricas, Web Vitals, eventos de domínio) |

## O que acontece

1. `apiKeyAuth` valida `X-Api-Key` (com cache 30s) e injeta a chave
   no contexto. Sem chave → `401`. Chave revogada/expirada → `401`.
2. Rate limit por chave: se estourou `NDOVU_INGEST_RATE_RPS`,
   devolve `429` com `Retry-After`.
3. `PostEvents` decodifica o JSON. Body > `NDOVU_MAX_BODY_BYTES`
   → `413`.
4. **Cross-tenant guard**: `req.App` do envelope precisa bater com o
   `key.App` da chave. Diverge → `400` com `details` explicando.
5. Se veio header `traceparent`, é usado como fallback para eventos
   sem `trace` próprio (padrão OTel — vincula toda a request HTTP a
   um trace).
6. `IngestService.Ingest` valida o batch (`app`, `sessionId`,
   `events[].eventId` UUID válido, `type` conhecido, `name` e
   `timestamp` obrigatórios). Máximo `500` eventos por lote.
   Divergência → `400` com todos os `issues[]`.
7. Normaliza: propaga `sessionId`/`app`/`userId` para cada evento,
   extrai `release` de `session.attributes.release` quando o evento
   não trouxe, marca `receivedAt = now`.
8. Publica o lote no NATS JetStream com `Nats-Msg-Id` determinístico
   (hash do payload) — janela de dedup de 2 min.
9. Resposta: `202 Accepted` + `{"accepted": N}` — o writer persiste
   no ClickHouse assincronamente.

**Idempotência dupla**:
- `Nats-Msg-Id` descarta re-publicação idêntica dentro de 2 min
  (protege retry de rede).
- `eventId` (UUID por evento) descarta duplicata no ClickHouse via
  `ReplacingMergeTree` (protege reenvio parcial ou lotes montados
  diferente com mesmos eventos).

Consequência: o cliente pode implementar retry ingênuo sem gerar
duplicatas nem carga extra.

## Como demonstrar

> "Latência de resposta em milissegundos, sem tocar em disco. A
> ingestão é um POST → valida → publica em fila → 202. O writer,
> em outro processo, persiste do jeito dele. Retry de rede é gratuito:
> `Nats-Msg-Id` e `eventId` cuidam de dedup."

Roteiro de 60s:
1. Rode o curl acima. Mostre a resposta em <50ms.
2. Rode **o mesmo curl** de novo. Mesmo `eventId`, mesma `202` —
   mas o ClickHouse dedup: só 1 linha aparece na tela Eventos.
3. Troque o `app` do body para `"outro-app"` mantendo a mesma chave.
   Mostre `400` com "app do envelope não corresponde ao app da chave".
4. Rode 100 curl em loop rápido: mostre alguns `429` com header
   `Retry-After: 1`.
5. Fale que o backend também aceita header `traceparent`: quem
   propaga OTel do frontend correlaciona automaticamente com o
   backend na tela Traces.

## Papéis (RBAC)

Este endpoint **não** usa JWT — auth é por chave. Cada chave é
criada em `/admin/apps` (ou `/admin/api-keys`) por um admin e
pertence a um `app` de uma `company`. Revogação (`DELETE /v1/admin/api-keys/{id}`)
tem efeito em até `NDOVU_KEY_CACHE_TTL_SECONDS` (30s default).

Uma chave só pode enviar eventos com o `app` dela — não há
"chave global" no fluxo normal (só super-admin cria regras globais
em outros contextos).

## Dependências

- NATS JetStream disponível (`NDOVU_NATS_URL`).
- Postgres com a chave cadastrada (para `apiKeyAuth` validar).
- Writer rodando (processo separado) para drenar a fila e gravar no
  ClickHouse. Sem writer, o `202` continua funcionando, mas nenhum
  evento aparece nas telas — a fila enche até `NDOVU_STREAM_MAX_AGE_HOURS`.

## Perguntas frequentes

- **"Por que 202 e não 200?"** Porque o evento ainda **não** foi
  persistido — só enfileirado. `202` é o código correto de "aceito,
  processamento assíncrono".
- **"O que acontece se o ClickHouse estiver fora?"** O writer
  segura o consumo da fila (não faz ack). Quando o CH voltar,
  drena o backlog. A ingestão continua respondendo `202` normalmente
  até o NATS estourar (`StreamMaxAge` ou disco).
- **"Posso mandar mais de 500 eventos em um lote?"** Não — `400`.
  Quebre em lotes menores; o SDK faz isso automaticamente
  (`maxBatch: 20` default).
- **"O `eventId` precisa ser UUID?"** Sim. É a coluna de PK no
  ClickHouse e a chave de idempotência global. Formato inválido →
  `400` com item específico em `details[]`.
- **"Tem sampling?"** Sim, no lado servidor — regras em
  `/admin/sampling`. O SDK envia tudo; o servidor decide o que
  persiste. Regras podem preservar 100% dos erros (`keep_errors=true`).

## Referências

- Handler: `backend/internal/adapter/httpapi/handlers.go` (`PostEvents`)
- Serviço: `backend/internal/usecase/ingest.go` (`IngestService`)
- Middleware: `backend/internal/adapter/httpapi/middleware.go` (`apiKeyAuth`)
- Stream: `backend/internal/adapter/natsstream/stream.go`
- Contrato completo: `docs/CONTRACT.md` §1
- Docs relacionadas: [sdk-browser.md](sdk-browser.md),
  [ingest-snapshots.md](ingest-snapshots.md),
  [widget-feedback.md](widget-feedback.md), [ARQUITETURA.md](../ARQUITETURA.md).
