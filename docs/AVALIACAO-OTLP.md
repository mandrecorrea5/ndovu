# Avaliação — receber OTLP no Ndovu

Status: **aprovada · Fases 0 e 1 concluídas** (2026-10-22) · decisões em §6 · resultado da Fase 1 em §8

## 1. Por que

Hoje o Ndovu enxerga o frontend inteiro e o backend só pelo que o SDK Node
emite (um `http_request` por request, desde a 1.4.0 com o mesmo `traceId` do
browser). Quem quer ver *dentro* do backend — query no banco, chamada a outro
serviço, fila — precisa de Jaeger/Tempo/Datadog ao lado, e o clique do usuário
e o span do banco ficam em ferramentas diferentes.

Receber OTLP muda isso: qualquer backend instrumentado com OpenTelemetry (em
qualquer linguagem, sem SDK nosso) manda spans e logs para o Ndovu, e o
`traceId` que o browser já propaga liga as duas pontas numa timeline só. É o
diferencial — o Sentry/Datadog fazem isso com SDK proprietário; nós faríamos
com o padrão aberto, sem lock-in para quem integra.

O que **não** era verdade quando este documento foi escrito: o Ndovu não
tinha receptor OTLP (existe desde a Fase 1, §8). O schema já tem `trace_id/span_id/parent_span_id` em `trace_events`
(comentário "OpenTelemetry ingest", `schema.sql:57`), mas isso é a correlação
W3C dos eventos *do nosso contrato*, não ingestão de spans.

## 2. Escopo por sinal

| Sinal | Valor | Custo | Decisão |
|---|---|---|---|
| **Traces** | alto — é o que liga front↔back | médio (tabela nova + endpoint + waterfall) | **Fase 1** |
| **Logs** | alto — substitui "log em arquivo na VPS" | médio (tabela + busca) | **Fase 2** |
| **Métricas** | médio — RED por rota sai dos próprios spans | alto (gauge/sum/histogram/exp. histogram, cardinalidade) | **adiar**; derivar RED de spans via materialized view |

## 3. Desenho proposto

### 3.1 Transporte

- **OTLP/HTTP** (protobuf e JSON, `Content-Encoding: gzip`). É o default dos
  SDKs JS e o que passa sem atrito por Cloudflare Tunnel → Caddy.
- **gRPC fica fora** da Fase 1: exige HTTP/2 de ponta a ponta no túnel e no
  Caddy, e os exporters HTTP cobrem todas as linguagens.
- **Host próprio** `otlp.<domínio>` com os paths padrão (`/v1/traces`,
  `/v1/logs`). Motivo: o SDK OTel monta `OTEL_EXPORTER_OTLP_ENDPOINT +
  /v1/traces`; no host da API, `POST /v1/traces` conviveria com o nosso
  `GET /v1/traces/{traceId}` (auth diferente, semântica diferente). Host
  separado = configuração zero para o cliente e nenhuma ambiguidade de rota.
  Atrás, é o mesmo processo `api` (outro listener ou roteamento por host no
  Caddy).

Configuração no cliente, sem código nosso:

```
OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp.ndovy.solutions
OTEL_EXPORTER_OTLP_HEADERS=x-api-key=ndk_...
OTEL_SERVICE_NAME=felpma-api
```

### 3.2 Autenticação e app

- Mesma `X-Api-Key` de hoje, validada pelo `APIKeyService` (cache + rate
  limit por chave). O **app vem da chave**, não do `service.name` — o
  `service.name` vira atributo (um app pode ter vários serviços).
- Reforça a recomendação de **chave separada para servidor**: chave de browser
  é pública e não deve aceitar OTLP. Proposta: flag por chave
  `allow_otlp` (default false), ligada só em chaves de servidor.

### 3.3 Pipeline

Reusa o que existe — nada de caminho paralelo:

```
exporter OTel ──HTTP──▶ api (/v1/traces) ──▶ NATS (subject otlp.traces) ──▶ writer ──▶ ClickHouse otel_spans
```

- Decodificação com `go.opentelemetry.io/collector/pdata` (`ptraceotlp`,
  `plogotlp`): já faz protobuf+JSON e evita manter os `.proto` à mão.
- Resposta segue a spec OTLP: `200` com `partial_success` quando parte foi
  rejeitada; `429`/`503` com `Retry-After` (os exporters respeitam e
  reenviam); `400` para payload inválido (exporter **não** reenvia).
- **Limite pós-descompressão** (gzip bomb): teto do corpo descomprimido, não só
  do comprimido. O `maxBody` atual mede o corpo cru.

**Alternativa descartada:** subir o OTel Collector com `clickhouseexporter`.
Rápido de ligar, mas grava direto no ClickHouse e pula autenticação por chave,
rate limit, sampling, stream (perde o buffer contra ClickHouse fora do ar) e
o vínculo com app/empresa do control plane. Seria um segundo produto ao lado
do nosso.

### 3.4 Armazenamento

Tabela nova `otel_spans` (não misturar com `trace_events`: span tem
início/fim, kind, status, events, links e resource — forçar no contrato de
evento perde informação):

- colunas: `app`, `trace_id`, `span_id`, `parent_span_id`, `name`, `kind`,
  `service_name`, `start_time` (DateTime64(9)), `duration_ns`, `status_code`,
  `status_message`, `resource_attrs` / `span_attrs` (Map(LowCardinality(String), String)),
  `events` e `links` (Nested), `received_at`.
- `MergeTree`, `PARTITION BY toDate(start_time)`, `ORDER BY (app, service_name, start_time)`,
  bloom filter em `trace_id` (igual a `trace_events`), mesma policy `tiered`
  de retenção.
- Fase 2: `otel_logs` no mesmo molde, com `trace_id/span_id` para pular do log
  ao trace.

### 3.5 Produto (o que justifica o esforço)

- **Waterfall unificado** em `GET /v1/traces/{traceId}`: união de
  `trace_events` (clique, fetch, erro JS) com `otel_spans` (handler, SQL,
  chamada externa), ordenada por tempo e montada pela árvore de
  `parent_span_id`. O SDK browser já gera o span raiz; o SDK Node 1.4.0 já
  liga o request a ele.
- Da sessão/issue no dashboard → "ver trace do backend" sem trocar de
  ferramenta.
- Span com `status=ERROR` ou evento `exception` entra no agrupamento de
  issues (mesmo fingerprint por stack) — fase 1.5.

## 4. Riscos

### 4.1 Volume (o principal)

Backend gera ordens de grandeza mais dados que frontend. Conta de bolso com
um backend modesto: 10 req/s × 10 spans/request = **8,6 M spans/dia**. O
`CAPACITY-VPS.md` diz que a VPS atual aguenta **≤ ~1 M eventos/dia** sem
mudança e **> 5 M/dia não serve**. Ou seja: **um único cliente com OTLP
ligado sem sampling estoura a VPS.**

Mitigações obrigatórias, não opcionais:

1. **Sampling por trace, não por evento.** O sampling atual sorteia evento a
   evento (`sampling.go:93`, `rand.Float64()`); aplicado a spans, picota os
   traces (sobra o filho sem o pai). Para spans, decidir por
   `hash(trace_id)` — o trace inteiro fica ou sai — e manter
   `keepErrors=true` (trace com erro sempre fica).
2. **Orientar head sampling no cliente** (`OTEL_TRACES_SAMPLER=parentbased_traceidratio`),
   documentado no guia de integração.
3. **Cota por app** (spans/dia) além do rate limit por segundo, com métrica e
   alerta quando um app se aproximar.
4. Antes de abrir para clientes: rodar o próprio Ndovu (ou o felpma) contra
   ele e medir bytes/span reais no ClickHouse, atualizando `CAPACITY-VPS.md`.

### 4.2 PII

Spans carregam `db.statement`, `url.full` com query string, headers e
`enduser.id`. O SDK browser redige; aqui o dado chega pronto do cliente.

- Redação server-side no writer: mesma regex de chaves sensíveis aplicada a
  atributos, e query string removida de `url.full`/`http.url` por padrão.
- **LGPD/forget:** `DELETE /v1/admin/gdpr/user/{userId}` passa a apagar
  também `otel_spans`/`otel_logs` onde `enduser.id = userId`.

### 4.3 Escopo

"Observabilidade completa" puxa métricas, alertas sobre métricas, profiles…
Este documento propõe traces → logs e para aí até medir uso e custo.

## 5. Fases e estimativa grosseira

| Fase | Entrega | Tamanho |
|---|---|---|
| 0 ✅ | Dogfooding: Ndovu se instrumenta com OTel (§18.4 da ARQUITETURA) exportando para um Jaeger local — gera o tráfego de teste da Fase 1 | P |
| 1 ✅ | `POST /v1/traces` (HTTP, proto+JSON, gzip) · `otel_spans` · sampling por trace · flag `allow_otlp` (no app, ver §8) · waterfall unificado no dashboard | G |
| 1.5 | Spans com erro viram issues · RED por rota via materialized view | M |
| 2 | `POST /v1/logs` · `otel_logs` · busca e salto log→trace | M/G |
| — | Métricas OTLP | adiado |

## 6. Decisões (2026-10-21)

1. **Host separado** `otlp.<domínio>` com os paths padrão OTLP (`/v1/traces`,
   `/v1/logs`). Prefixo no host da API descartado.
2. **Cota diária por app estourada → sampling forçado temporário**, não 429.
   Ao passar da cota, o app entra em sampling por trace a uma taxa reduzida
   (erros continuam passando, `keepErrors`) até a virada do dia, quando volta
   à regra normal. Entrar e sair do modo forçado gera métrica e entrada no
   audit log, e dispara alerta para o admin. Valores iniciais, a calibrar com
   a medição da Fase 0: cota **500 k spans/dia por app**, taxa forçada
   **10%**, virada à meia-noite de `America/Sao_Paulo`.
   Motivo: exporter OTel que recebe 429 por horas descarta dados
   indiscriminadamente (inclusive erros); sampling no servidor preserva os
   traces com erro e um retrato estatístico do resto.
3. **Retenção de spans e logs OTLP: 30 dias** (limite superior da faixa
   recomendada), configurável por env, independente dos 90 dias de
   `trace_events`.
4. **felpma é o piloto**, mas só entra **depois do waterfall unificado**
   (fim da Fase 1). Até lá o felpma segue com o SDK Node 1.4.0, que já
   correlaciona o request do servidor com o trace do browser.

## 7. Resultado da Fase 0 (2026-10-22)

- API e writer exportam traces OTLP/HTTP; detalhes em ARQUITETURA §18.4.
- Verificado de ponta a ponta contra Jaeger 2.22: ingestão → stream → writer
  → ClickHouse aparece como dois traces ligados por link; consulta autenticada
  continua o trace recebido.
- Volume do próprio Ndovu: 2–3 spans por request de ingestão, 2 por lote do
  writer. Dá a primeira referência para a cota da §6: o Ndovu sozinho, a 1 M
  eventos/dia, gera ~150 k spans/dia — bem abaixo dos 500 k de cota inicial.
- Para a Fase 1: trocar o destino de Jaeger para o próprio Ndovu é mudar
  `OTEL_EXPORTER_OTLP_ENDPOINT`. O Ndovu passa a ser o primeiro cliente do
  receptor OTLP.
- Pendente: medir bytes/span no ClickHouse (só possível com `otel_spans`, Fase 1);
  propagação nos backends Kafka e RabbitMQ do stream.

## 8. Resultado da Fase 1 (2026-10-22)

Entregue: receptor `POST /v1/traces` em listener próprio (`:4318`, host
`otlp.<domínio>` no Caddy), transporte `ndovu.spans.<app>` no NATS, tabela
`otel_spans`, política de sampling por trace + cota diária no writer,
`GET /v1/traces/{traceId}` unificado e com scope de tenant, waterfall em
`/traces/{traceId}` no dashboard, checkbox "aceita OTLP" no cadastro de app e
tipo `span` nas regras de sampling. Detalhes em ARQUITETURA §18.5; guia do
cliente em INTEGRATION §14.

Verificado de ponta a ponta com o próprio Ndovu exportando para si: evento do
SDK (span B1) → `GET /v1/sessions/{sessionId}` (pai = B1) → dois
`SELECT trace_events`, no mesmo waterfall. Chave de app de browser recebe
403; JSON e gzip aceitos; cota estourada liga o modo forçado e grava
`otlp.quota.forced_sampling_on` no audit log.

**Desvios do desenho original:**

1. **`allow_otlp` é do app, não da chave.** `CreateAPIKey` revoga a chave
   anterior do app (uma ativa por app); um flag na chave se perderia a cada
   rotação. Na prática é o mesmo controle: o app de servidor (`felpma-api`) é
   separado do app de browser, como já recomendado.
2. **Redação na API, não no writer** (§4.2 dizia writer). Assim o dado
   sensível não chega nem ao disco do stream.
3. **Regras de sampling só valem para spans com `event_type = span`.** A regra
   global de eventos (tipo vazio) não corta spans — volume e granularidade são
   outros.
4. **"Trace com erro sempre fica" vale dentro do lote.** É head sampling no
   servidor: um erro que chega num lote posterior não resgata spans já
   descartados do mesmo trace. Tail sampling de verdade exigiria segurar spans
   em memória por trace — fora do escopo.
5. **O estouro da cota passa do limite em até um lote do writer** (a conta é
   feita depois do insert, para reentrega não inflar o contador).
6. **Corrigido de passagem:** o `GET /v1/traces/{traceId}` não filtrava por
   tenant — qualquer usuário logado lia o trace de qualquer app pelo id.

**Pendente para ligar em produção** (fora do código, manual):

- Public hostname `otlp.ndovy.solutions` no Cloudflare Tunnel apontando para
  o `caddy`, e `NDOVU_OTLP_DOMAIN=otlp.ndovy.solutions` no `.env` da VPS (sem
  isso o bloco do Caddy fica inerte em `otlp.invalid`).
- `NDOVU_SPAN_QUOTA_ALERT_TO` (default: destinatários do digest) para o
  e-mail de cota ter para quem ir.

**Para depois:** exportação LGPD (portabilidade) ainda devolve só eventos, não
spans; Kafka e RabbitMQ não transportam spans; bytes/span reais em produção
para recalibrar a cota (`CAPACITY-VPS.md`).
