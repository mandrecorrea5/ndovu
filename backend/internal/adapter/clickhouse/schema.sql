-- Ndovu — schema ClickHouse
-- Tabela única e desnormalizada: colunas "quentes" tipadas para filtro/índice,
-- payloads como JSON texto comprimido com ZSTD (colunar comprime repetição a ~nada).
--
-- ReplacingMergeTree deduplica eventos com o mesmo id (chave de ordenação) na
-- merge — reenvios do SDK/stream são seguros e as consultas usam FINAL.
-- Partição por dia => janelas de tempo tocam só as partições necessárias e a
-- retenção (TTL) descarta partições inteiras.
CREATE TABLE IF NOT EXISTS trace_events
(
    id            UUID,
    session_id    String,
    user_id       String DEFAULT '',
    app           LowCardinality(String),
    event_type    LowCardinality(String),
    name          LowCardinality(String),
    feature       LowCardinality(String) DEFAULT '',
    screen        String DEFAULT '',
    http_method   LowCardinality(String) DEFAULT '',
    http_url      String DEFAULT '',
    http_status   Nullable(UInt16),
    duration_ms   Nullable(Int32),
    request_body  String DEFAULT '' CODEC(ZSTD(3)),
    response_body String DEFAULT '' CODEC(ZSTD(3)),
    error_code    String DEFAULT '',
    error_message String DEFAULT '',
    error_body    String DEFAULT '' CODEC(ZSTD(3)),
    metadata      String DEFAULT '' CODEC(ZSTD(3)),
    user_agent    String DEFAULT '' CODEC(ZSTD(1)),
    session_attrs String DEFAULT '' CODEC(ZSTD(1)),
    occurred_at   DateTime64(3, 'UTC'),
    received_at   DateTime64(3, 'UTC'),

    INDEX idx_session session_id TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_user    user_id    TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_url     http_url   TYPE tokenbf_v1(8192, 3, 0) GRANULARITY 4,
    INDEX idx_error   error_code TYPE bloom_filter(0.01) GRANULARITY 4,

    -- Projeção para o rastro: os dados também ficam ordenados por sessão,
    -- então a timeline de uma sessão não varre a janela de tempo inteira.
    PROJECTION by_session
    (
        SELECT * ORDER BY (session_id, occurred_at)
    )
)
ENGINE = ReplacingMergeTree
PARTITION BY toDate(occurred_at)
ORDER BY (app, occurred_at, session_id, id)
TTL toDateTime(occurred_at) + INTERVAL 90 DAY DELETE
SETTINGS index_granularity = 8192, deduplicate_merge_projection_mode = 'rebuild';

-- Release tracking (Fase 2): coluna extra para filtrar/agrupar por versão do
-- app. Adicionada como ALTER porque a tabela pode já existir. LowCardinality
-- comprime muito bem quando o app deploya poucas vezes por dia.
ALTER TABLE trace_events ADD COLUMN IF NOT EXISTS release LowCardinality(String) DEFAULT '';

-- OpenTelemetry ingest (Fase 2 sprint D): permite correlacionar eventos do
-- frontend com traces do backend (mesmo trace_id W3C). span_id identifica
-- o evento em si e parent_span_id liga a hierarquia (fetch -> HTTP handler).
-- Bloom filter em trace_id para lookup rapido do endpoint /v1/traces/id.
ALTER TABLE trace_events ADD COLUMN IF NOT EXISTS trace_id String DEFAULT '';
ALTER TABLE trace_events ADD COLUMN IF NOT EXISTS span_id String DEFAULT '';
ALTER TABLE trace_events ADD COLUMN IF NOT EXISTS parent_span_id String DEFAULT '';
ALTER TABLE trace_events ADD INDEX IF NOT EXISTS idx_trace_id trace_id TYPE bloom_filter(0.01) GRANULARITY 4;

-- Retencao em camadas (Fase 3 sprint F.1). Aplicado por ALTER porque a
-- tabela ja existe (foi criada em versoes anteriores).
--
-- Policy `tiered` esta definida em /etc/clickhouse-server/config.d/storage.xml:
--   * default (hot) = disk default (SSD, /var/lib/clickhouse)
--   * warm          = /warm (HDD montado como volume separado)
--   * cold          = disk s3 (MinIO local ou AWS S3 real em prod)
-- O nome `default` no primeiro volume e obrigatorio para migrar a policy
-- antiga (que tinha um unico volume tambem chamado `default`) sem exigir
-- reprocessamento das parts existentes.
ALTER TABLE trace_events MODIFY SETTING storage_policy = 'tiered';

-- TTL com moves entre volumes. Cada regra ativa quando o timestamp cruza:
-- o dado migra automaticamente na proxima merge. DELETE final descarta 90d+.
--   0-7d   : hot (SSD)
--   7-30d  : warm (HDD local)
--   30-90d : cold (S3/MinIO)
--   90d+   : delete
ALTER TABLE trace_events MODIFY TTL
    toDateTime(occurred_at) + INTERVAL 7  DAY TO VOLUME 'warm',
    toDateTime(occurred_at) + INTERVAL 30 DAY TO VOLUME 'cold',
    toDateTime(occurred_at) + INTERVAL 90 DAY DELETE;

-- Spans OpenTelemetry recebidos por OTLP (Fase 1 do docs/AVALIACAO-OTLP.md).
-- Tabela própria: span tem início/fim, kind, status, eventos e links, que
-- não cabem no contrato de trace_events. Atributos em Map para filtrar por
-- igualdade (http.route, db.system...); eventos e links em JSON, só exibidos.
--
-- ReplacingMergeTree pela mesma razão de trace_events: exporter OTLP reenvia
-- em timeout, e o reenvio tem start_time/trace_id/span_id idênticos.
-- A retenção (TTL) é aplicada pelo código, porque vem de env
-- (NDOVU_SPAN_RETENTION_DAYS) — ver EnsureSchema.
CREATE TABLE IF NOT EXISTS otel_spans
(
    app            LowCardinality(String),
    trace_id       String,
    span_id        String,
    parent_span_id String DEFAULT '',
    trace_state    String DEFAULT '',
    name           LowCardinality(String),
    kind           LowCardinality(String),
    service_name   LowCardinality(String),
    scope_name     LowCardinality(String) DEFAULT '',
    user_id        String DEFAULT '',
    start_time     DateTime64(9, 'UTC'),
    duration_ns    Int64,
    status_code    LowCardinality(String),
    status_message String DEFAULT '',
    resource_attrs Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    span_attrs     Map(LowCardinality(String), String) CODEC(ZSTD(3)),
    events         String DEFAULT '' CODEC(ZSTD(3)),
    links          String DEFAULT '' CODEC(ZSTD(3)),
    received_at    DateTime64(3, 'UTC'),

    INDEX idx_trace_id trace_id TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_user     user_id  TYPE bloom_filter(0.01) GRANULARITY 4
)
ENGINE = ReplacingMergeTree
PARTITION BY toDate(start_time)
ORDER BY (app, service_name, start_time, trace_id, span_id)
SETTINGS index_granularity = 8192, storage_policy = 'tiered'
