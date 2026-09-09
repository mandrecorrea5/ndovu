# Ndovu — Contrato de Ingestão v1

Qualquer frontend (web, mobile, desktop, TV, kiosk) pode enviar dados para o
Ndovu, independente de tecnologia, desde que respeite este contrato. Esta é
a **spec de referência** — todos os SDKs oficiais (`ndovu-browser`,
`ndovu-node`, `ndovu-next`, `ndovu-react-native`) implementam este contrato,
mas nenhum é obrigatório: qualquer cliente HTTP que envie o JSON esperado
está integrado.

Este documento cobre **três endpoints de ingestão**:

- `POST /v1/events` — eventos capturados (a rota principal).
- `POST /v1/snapshots` — HTML do DOM no momento de um erro (session replay).
- `POST /v1/feedbacks` — feedback do usuário via widget.

Todos autenticam por `X-Api-Key`.

---

## 1. `POST /v1/events`

### 1.1 Endpoint

```
POST /v1/events
Content-Type: application/json
X-Api-Key: <chave de ingestão>
```

O corpo é um **batch**: um envelope de sessão + uma lista de eventos. O SDK do
frontend deve acumular eventos e enviar em lotes (ex.: a cada 5s ou 20 eventos),
com `navigator.sendBeacon`/fetch no unload.

### 1.2 Envelope

```json
{
  "app": "portal-cliente",
  "sdkVersion": "1.2.0",
  "session": {
    "sessionId": "b7f9c2e0-4d1a-4f3b-9c8d-2e5a7b1c3d4f",
    "userId": "user-12345",
    "userAgent": "Mozilla/5.0 ...",
    "attributes": {
      "plano": "premium",
      "canal": "web",
      "release": "1.2.3"
    }
  },
  "events": [ { ... }, { ... } ]
}
```

| Campo               | Tipo   | Obrigatório | Descrição |
|---------------------|--------|-------------|-----------|
| `app`               | string | sim         | Identificador do frontend emissor. **Deve bater com o app da chave.** |
| `sdkVersion`        | string | não         | Versão do SDK/instrumentação (ex.: `"1.2.0"`) |
| `session.sessionId` | string | sim         | ID único da sessão (UUID gerado no frontend) |
| `session.userId`    | string | não*        | ID do usuário autenticado. *Recomendado; eventos anônimos são aceitos e correlacionáveis pela sessão |
| `session.userAgent` | string | não         | User agent / device info |
| `session.attributes`| object | não         | Metadados livres da sessão (plano, canal, release, etc). Se enviar `attributes.release`, o Ndovu extrai automaticamente para a coluna `release` — habilita release tracking |

### 1.3 Evento

```json
{
  "eventId": "0d9e0a44-8c2b-4c33-a1f2-7e6b5d4c3b2a",
  "type": "http_request",
  "name": "gerar_fatura",
  "feature": "faturas",
  "screen": "/faturas/segunda-via",
  "timestamp": "2026-08-22T14:32:11.482-03:00",
  "durationMs": 843,
  "http": {
    "method": "POST",
    "url": "/api/v2/faturas/segunda-via",
    "statusCode": 200,
    "requestBody": { "contratoId": "789", "mes": "2026-08" },
    "responseBody": { "faturaId": "F-2026-08-789", "valor": 189.9 }
  },
  "error": null,
  "trace": {
    "traceId": "0af7651916cd43dd8448eb211c80319c",
    "spanId": "b7ad6b7169203331",
    "parentSpanId": null
  },
  "metadata": { "tentativa": 1 }
}
```

| Campo        | Tipo    | Obrigatório | Descrição |
|--------------|---------|-------------|-----------|
| `eventId`    | string (uuid) | sim  | ID único do evento — chave de idempotência (reenvio de lote não duplica) |
| `type`       | enum    | sim         | `page_view` \| `action` \| `http_request` \| `error` \| `custom` |
| `name`       | string  | sim         | Nome de negócio: `login_success`, `segunda_via`, `gerar_fatura`… |
| `feature`    | string  | não         | Agrupador funcional: `login`, `faturas`, `pagamentos`… |
| `screen`     | string  | não         | Tela/rota do frontend |
| `timestamp`  | RFC3339 | sim         | Momento do evento no cliente (com fuso) |
| `durationMs` | int     | não         | Duração em milissegundos |
| `http`       | object  | não         | Presente quando o evento envolve chamada HTTP |
| `error`      | object  | não         | Presente quando o evento representa falha |
| `trace`      | object  | não         | Correlação W3C — habilita rastreio ponta-a-ponta com backend instrumentado com OpenTelemetry |
| `metadata`   | object  | não         | Qualquer contexto adicional (JSON livre) |

### 1.4 Objeto `http`

| Campo          | Tipo   | Descrição |
|----------------|--------|-----------|
| `method`       | string | GET / POST / PUT / PATCH / DELETE |
| `url`          | string | Endpoint chamado (path; o SDK deve remover host/PII de query) |
| `statusCode`   | int    | Status HTTP retornado |
| `requestBody`  | any    | Payload enviado (JSON). Redigido pelo SDK antes de sair (senha, token, cvv…) |
| `responseBody` | any    | Payload retornado (JSON) |

### 1.5 Objeto `error`

| Campo     | Tipo   | Descrição |
|-----------|--------|-----------|
| `code`    | string | Código de erro (HTTP ou de negócio): `AUTH_401`, `FATURA_INDISPONIVEL`, `NETWORK_ERROR`, `JS_ERROR`… |
| `message` | string | Mensagem legível |
| `body`    | any    | Payload bruto: para erro JS inclui `stack`, `source`, `line`, `column`; para erro HTTP inclui o response body |

### 1.6 Objeto `trace` (correlação W3C)

Formato do header W3C `traceparent`: `00-{traceId}-{spanId}-01`.

| Campo          | Tipo   | Descrição |
|----------------|--------|-----------|
| `traceId`      | string | 32 caracteres hex (16 bytes). Idêntico ao traceId propagado ao backend |
| `spanId`       | string | 16 caracteres hex (8 bytes). Único por request |
| `parentSpanId` | string | Opcional. Se o evento é filho de outro span |

Quando o SDK usa `instrumentFetch()`, gera automaticamente `traceparent` como
header outgoing na requisição e grava os mesmos IDs no evento — permitindo
correlacionar frontend↔backend se o backend também for instrumentado com
OpenTelemetry (Jaeger, Tempo, Honeycomb, Datadog APM).

### 1.7 Respostas

| Status | Body | Significado |
|--------|------|-------------|
| `202 Accepted` | `{"accepted": N}` | Lote validado e enfileirado. Persistência assíncrona — resposta em milissegundos |
| `400 Bad Request` | `{"error":"...", "details": [...]}` | Contrato violado. Inclui divergência `app` envelope × `app` chave |
| `401 Unauthorized` | `{"error":"unauthorized"}` | `X-Api-Key` ausente, inválida ou revogada |
| `413 Payload Too Large` | `{"error":"payload too large"}` | Lote > `NDOVU_MAX_BODY_BYTES` (default 1 MB) |
| `429 Too Many Requests` | `{"error":"rate limit exceeded"}` | Rate limit por chave excedido (default 50 req/s). Header `Retry-After` |

### 1.8 Vinculação app ↔ chave (cross-tenant guard)

Cada chave de API pertence a **um app**. A ingestão **exige** que o campo `app`
do envelope seja idêntico ao `app` da chave apresentada em `X-Api-Key`:

- Se bater → `202`.
- Se divergir → `400` com `details` explicando a divergência.

Isso garante que cada frontend use a própria chave e não consiga rotular
eventos em nome de outro app (mesmo que a chave vaze, ela só vale para o app
dono). Para enviar eventos de outro frontend, gere uma chave própria para ele.

---

## 2. `POST /v1/snapshots`

Envia o HTML do DOM no momento em que um erro foi capturado. Habilita o
**session replay MVP** — quando o admin abrir a issue no dashboard, verá uma
foto visual do que estava na tela.

### 2.1 Endpoint

```
POST /v1/snapshots
Content-Type: application/json  (ou application/gzip para HTML gzipped)
X-Api-Key: <chave de ingestão>
```

### 2.2 Corpo

```json
{
  "eventId": "0d9e0a44-8c2b-4c33-a1f2-7e6b5d4c3b2a",
  "sessionId": "b7f9c2e0-4d1a-4f3b-9c8d-2e5a7b1c3d4f",
  "app": "portal-cliente",
  "html": "<!doctype html><html>...",
  "url": "https://app.com/checkout",
  "viewportW": 1920,
  "viewportH": 1080,
  "takenAt": "2026-08-22T14:32:11.500-03:00"
}
```

| Campo       | Tipo   | Obrigatório | Descrição |
|-------------|--------|-------------|-----------|
| `eventId`   | string | sim         | Deve casar com um `event.eventId` já enviado do tipo `error` |
| `sessionId` | string | sim         | ID da sessão |
| `app`       | string | sim         | Deve bater com a chave |
| `html`      | string | sim         | HTML sanitizado. **Responsabilidade do SDK**: remover `<script>`, mascarar inputs sensíveis, mascarar elementos com `data-ndovu-mask` |
| `url`       | string | não         | URL atual no momento do snapshot |
| `viewportW` | int    | não         | Largura do viewport (px) |
| `viewportH` | int    | não         | Altura do viewport (px) |
| `takenAt`   | RFC3339| não         | Momento da captura no cliente |

### 2.3 Sanitização (obrigatório no SDK)

O SDK deve, **antes** de enviar:

1. Remover todos os `<script>` e `<iframe>`.
2. Mascarar `value` de inputs sensíveis: `type=password|email|tel|cc-number|cc-csc`.
3. Mascarar `textContent` de qualquer elemento com atributo `data-ndovu-mask`.

O dashboard renderiza o HTML dentro de `<iframe sandbox>` (defesa em
profundidade), mas confia no SDK para a sanitização inicial.

### 2.4 Respostas

| Status | Significado |
|--------|-------------|
| `200 OK` | Snapshot armazenado. Metadata em Postgres, blob em S3/MinIO |
| `400` | Payload inválido, `app` não bate |
| `401` | Chave inválida |
| `413` | HTML > limite (recomenda-se gzip do lado do cliente) |

---

## 3. `POST /v1/feedbacks`

Envia feedback do usuário final via widget do SDK — bug, sugestão, elogio ou
outro. Atrelado à sessão e (opcionalmente) ao último evento visto.

### 3.1 Endpoint

```
POST /v1/feedbacks
Content-Type: application/json
X-Api-Key: <chave de ingestão>
```

### 3.2 Corpo

```json
{
  "app": "portal-cliente",
  "sessionId": "b7f9c2e0-4d1a-4f3b-9c8d-2e5a7b1c3d4f",
  "eventId": "0d9e0a44-8c2b-4c33-a1f2-7e6b5d4c3b2a",
  "type": "bug",
  "message": "O botão de pagamento não funciona no checkout",
  "email": "cliente@example.com",
  "url": "https://app.com/checkout",
  "viewportW": 1920,
  "viewportH": 1080
}
```

| Campo       | Tipo   | Obrigatório | Descrição |
|-------------|--------|-------------|-----------|
| `app`       | string | sim         | Deve bater com a chave |
| `sessionId` | string | sim         | ID da sessão atual |
| `eventId`   | string | não         | Último `eventId` visto. Permite deep-link no dashboard |
| `type`      | enum   | sim         | `bug` \| `suggestion` \| `praise` \| `other` |
| `message`   | string | sim         | Texto do feedback (max 5000 caracteres) |
| `email`     | string | não         | Email de contato do usuário |
| `url`       | string | não         | URL onde o feedback foi enviado |
| `viewportW` | int    | não         | Largura do viewport |
| `viewportH` | int    | não         | Altura do viewport |

### 3.3 Respostas

| Status | Body | Significado |
|--------|------|-------------|
| `200 OK` | Objeto criado com `id`, `status: "new"`, `createdAt` | Feedback armazenado |
| `400` | `details[]` | Contrato inválido (message ausente, type inválido, message > 5000 chars) |
| `401` | — | Chave inválida |

---

## 4. Regras gerais

### 4.1 Imutabilidade

A API de ingestão só insere. Não existe endpoint público de update/delete de
eventos. A API de consulta é 100% read-only. As únicas exceções (LGPD) são
endpoints administrativos autenticados com JWT admin.

### 4.2 Idempotência

- **`eventId`** é a chave global. Reenvio do mesmo evento (mesmo id) é
  deduplicado no ClickHouse via `ReplacingMergeTree`.
- **Reenvio de lote inteiro** também é seguro: dedup do NATS via `Nats-Msg-Id`
  (janela de 2min) descarta re-publicação idêntica.
- **Consequência prática:** o SDK pode implementar retry ingênuo — não gera
  duplicatas nem carga extra.

### 4.3 Tolerância

- Campos desconhecidos são **ignorados** (não retornam erro).
- Evolução do contrato acontece por **versão de path** (`/v1`, `/v2`), sem
  quebrar o formato vigente.
- Payloads com campos faltando não-obrigatórios são aceitos (usam defaults).

### 4.4 Privacidade

**Mascarar dados sensíveis é responsabilidade do SDK**, antes do envio. O SDK
de referência já redige recursivamente campos que combinam:

```
/pass(word)?|senha|token|secret|authorization|cvv|card|cart[aã]o/i
```

Aplicado em `requestBody`, `responseBody` e `error.body`. Não coloque dados
sensíveis em `metadata` ou `name` — o SDK não redige texto plano fora de
objetos com chave-valor.

### 4.5 Relógio do cliente

- `timestamp` é o relógio do **cliente** (frontend).
- A API grava adicionalmente `received_at` (relógio do **servidor**).
- Isso permite detectar drift de relógio de cliente e ordenar por tempo real
  de recebimento se necessário.

### 4.6 Limites operacionais

| Limite | Default | Env var |
|---|---|---|
| Payload máximo por request | 1 MB | `NDOVU_MAX_BODY_BYTES` |
| Rate limit por chave | 50 req/s | `NDOVU_INGEST_RATE_RPS` |
| Retenção do NATS (buffer entre API e writer) | 48h | `NDOVU_STREAM_MAX_AGE_HOURS` |
| Cache de validação de chave | 30s | `NDOVU_KEY_CACHE_TTL_SECONDS` |
| Retenção de eventos (ClickHouse) | 90 dias | Schema DDL |

### 4.7 Sampling (server-side)

O servidor pode **descartar** eventos aceitos, conforme regras de sampling
configuradas em `/admin/sampling`. Regras têm precedência
(`app+type > app > type > global`) e podem preservar 100% de erros
(`keep_errors=true`) mesmo com `sample_rate` baixo.

Do ponto de vista do SDK, isso é transparente: envia normalmente, o servidor
decide o que persiste. `eventId` retornado em `accepted` inclui apenas os que
passaram no sampling (não há resposta explícita "descartado").

---

## 5. Referência rápida

- **Endpoint OpenAPI (spec):** `GET /openapi.yaml`
- **Swagger UI:** `GET /docs`
- **Documentação de integração:** [`INTEGRATION.md`](INTEGRATION.md)
- **Guia de uso por tecnologia:** [`USO.md`](USO.md)
- **Arquitetura detalhada:** [`ARQUITETURA.md`](ARQUITETURA.md)
- **Referência técnica completa:** [`TECNICA.md`](TECNICA.md)

---

Documento vivo. Última atualização: agosto/2026.
Contrato v1 estável desde o início do projeto; endpoints `/v1/snapshots` e
`/v1/feedbacks` adicionados nas Fases 3 e 4 respectivamente.
