//go:build integration

// Package testenv sobe containers Docker efêmeros pra testes de integração
// (Postgres, ClickHouse, MinIO). Cada helper devolve o cliente configurado
// e registra t.Cleanup para derrubar o container após o teste.
//
// Todos os testes que usam este pacote precisam da build tag `integration`:
//
//	go test -tags=integration ./...
//
// Isso mantém `go test ./...` rápido (só unit tests) e restringe a suite
// pesada ao CI / execução manual quando necessário.
package testenv

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	chgo "github.com/ClickHouse/clickhouse-go/v2"
	miniogo "github.com/minio/minio-go/v7"
	miniocreds "github.com/minio/minio-go/v7/pkg/credentials"
	tcch "github.com/testcontainers/testcontainers-go/modules/clickhouse"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcrabbit "github.com/testcontainers/testcontainers-go/modules/rabbitmq"
	tcredpanda "github.com/testcontainers/testcontainers-go/modules/redpanda"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/blobstore"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/clickhouse"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/ctlpostgres"
)

// PostgresDSN sobe um Postgres 16 alpine em container efêmero e devolve
// o DSN pronto pra uso. Container é derrubado no t.Cleanup automaticamente.
func PostgresDSN(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	container, err := tcpg.Run(ctx,
		"postgres:16-alpine",
		tcpg.WithDatabase("ndovu_test"),
		tcpg.WithUsername("ndovu"),
		tcpg.WithPassword("ndovu"),
		tcpg.BasicWaitStrategies(),
		tcpg.WithSQLDriver("pgx"),
	)
	if err != nil {
		t.Fatalf("subindo Postgres testcontainer: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("obtendo DSN: %v", err)
	}
	return dsn
}

// StartPostgres sobe Postgres + aplica migrações do ndovu (via ctlpostgres.Connect)
// e devolve o *Repository pronto pra uso. Container derruba no cleanup.
func StartPostgres(t *testing.T) *ctlpostgres.Repository {
	t.Helper()
	dsn := PostgresDSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := ctlpostgres.Connect(ctx, dsn, logger)
	if err != nil {
		t.Fatalf("Connect + migrate: %v", err)
	}
	t.Cleanup(repo.Close)
	return repo
}

// StartClickHouse sobe um ClickHouse 24 alpine em container efêmero e
// devolve o *Repository pronto. Aplica um schema simplificado (sem
// storage_policy 'tiered' que exige MinIO + disco warm) — os testes focam
// em comportamento SQL (bulk insert, dedup, alias, filtros), não em
// storage tiers, que dependem de infra externa.
func StartClickHouse(t *testing.T) *clickhouse.Repository {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	container, err := tcch.Run(ctx,
		"clickhouse/clickhouse-server:24.8-alpine",
		tcch.WithUsername("ndovu"),
		tcch.WithPassword("ndovu"),
		tcch.WithDatabase("ndovu_test"),
	)
	if err != nil {
		t.Fatalf("subindo ClickHouse: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	port, err := container.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatalf("port: %v", err)
	}

	chAddr = fmt.Sprintf("%s:%s", host, port.Port())
	chDB, chUser, chPass = "ndovu_test", "ndovu", "ndovu"

	repo, err := clickhouse.Connect(ctx, clickhouse.Options{
		Addr: chAddr, Database: chDB, Username: chUser, Password: chPass,
	})
	if err != nil {
		t.Fatalf("clickhouse.Connect: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	// Schema simplificado: só CREATE TABLE + índices, sem storage_policy
	// que exige MinIO. Cobre 99% dos testes (comportamento SQL).
	if err := applyBaseSchema(ctx, repo); err != nil {
		t.Fatalf("aplicando schema base: %v", err)
	}
	return repo
}

// applyBaseSchema aplica o mesmo DDL do schema.sql mas pulando as ALTERs
// finais de storage_policy = 'tiered' e TTL com moves entre volumes —
// essas dependem de policy definida em config.d/storage.xml (com MinIO
// e disco warm), que não está presente no container standalone.
// Abre conexão paralela (o Repository não expõe ExecRaw pra evitar API
// pública desnecessária).
func applyBaseSchema(ctx context.Context, _ *clickhouse.Repository) error {
	// A repository já validou a conexão em Connect; abrimos uma paralela
	// pro DDL. Reutilizar a interna exigiria expor Exec — evitamos vazar
	// API interna só pra teste.
	return applyBaseSchemaDirect(ctx)
}

var chAddr, chDB, chUser, chPass string

func applyBaseSchemaDirect(ctx context.Context) error {
	conn, err := chgo.Open(&chgo.Options{
		Addr: []string{chAddr},
		Auth: chgo.Auth{Database: chDB, Username: chUser, Password: chPass},
	})
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer conn.Close()

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS trace_events (
			id UUID, session_id String, user_id String DEFAULT '',
			app LowCardinality(String), event_type LowCardinality(String),
			name LowCardinality(String), feature LowCardinality(String) DEFAULT '',
			screen String DEFAULT '',
			http_method LowCardinality(String) DEFAULT '', http_url String DEFAULT '',
			http_status Nullable(UInt16), duration_ms Nullable(Int32),
			request_body String DEFAULT '' CODEC(ZSTD(3)),
			response_body String DEFAULT '' CODEC(ZSTD(3)),
			error_code String DEFAULT '', error_message String DEFAULT '',
			error_body String DEFAULT '' CODEC(ZSTD(3)),
			metadata String DEFAULT '' CODEC(ZSTD(3)),
			user_agent String DEFAULT '' CODEC(ZSTD(1)),
			session_attrs String DEFAULT '' CODEC(ZSTD(1)),
			occurred_at DateTime64(3, 'UTC'), received_at DateTime64(3, 'UTC'),
			INDEX idx_session session_id TYPE bloom_filter(0.01) GRANULARITY 4,
			INDEX idx_user user_id TYPE bloom_filter(0.01) GRANULARITY 4,
			INDEX idx_url http_url TYPE tokenbf_v1(8192, 3, 0) GRANULARITY 4,
			INDEX idx_error error_code TYPE bloom_filter(0.01) GRANULARITY 4,
			PROJECTION by_session (SELECT * ORDER BY (session_id, occurred_at))
		) ENGINE = ReplacingMergeTree PARTITION BY toDate(occurred_at)
		ORDER BY (app, occurred_at, session_id, id)
		TTL toDateTime(occurred_at) + INTERVAL 90 DAY DELETE
		SETTINGS index_granularity = 8192, deduplicate_merge_projection_mode = 'rebuild'`,

		`ALTER TABLE trace_events ADD COLUMN IF NOT EXISTS release LowCardinality(String) DEFAULT ''`,
		`ALTER TABLE trace_events ADD COLUMN IF NOT EXISTS trace_id String DEFAULT ''`,
		`ALTER TABLE trace_events ADD COLUMN IF NOT EXISTS span_id String DEFAULT ''`,
		`ALTER TABLE trace_events ADD COLUMN IF NOT EXISTS parent_span_id String DEFAULT ''`,
		`ALTER TABLE trace_events ADD INDEX IF NOT EXISTS idx_trace_id trace_id TYPE bloom_filter(0.01) GRANULARITY 4`,
	}
	for _, s := range stmts {
		if err := conn.Exec(ctx, s); err != nil {
			return fmt.Errorf("stmt: %w", err)
		}
	}
	return nil
}

// StartMinIO sobe MinIO em container efêmero, cria o bucket e devolve
// o blobstore.MinIO pronto pra uso. Container derruba no cleanup.
func StartMinIO(t *testing.T) *blobstore.MinIO {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		access = "testuser"
		secret = "testsecret1234" // MinIO exige >= 8 chars
		bucket = "test-bucket"
	)

	container, err := tcminio.Run(ctx,
		"minio/minio:latest",
		tcminio.WithUsername(access),
		tcminio.WithPassword(secret),
	)
	if err != nil {
		t.Fatalf("subindo MinIO: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	})

	endpoint, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}

	// Cria o bucket antes — blobstore.New() valida existência.
	client, err := miniogo.New(endpoint, &miniogo.Options{
		Creds:  miniocreds.NewStaticV4(access, secret, ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("minio client: %v", err)
	}
	if err := client.MakeBucket(ctx, bucket, miniogo.MakeBucketOptions{}); err != nil {
		t.Fatalf("make bucket: %v", err)
	}

	store, err := blobstore.New(ctx, blobstore.Config{
		Endpoint: endpoint, AccessKey: access, SecretKey: secret,
		Bucket: bucket, UseSSL: false,
	})
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	return store
}

// KafkaBrokers sobe um Redpanda (API-compatível com Kafka) em container
// efêmero e devolve a lista de brokers. Container derruba no cleanup.
func KafkaBrokers(t *testing.T) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	container, err := tcredpanda.Run(ctx,
		"docker.redpanda.com/redpandadata/redpanda:v24.2.1",
	)
	if err != nil {
		t.Fatalf("subindo Redpanda testcontainer: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	})

	broker, err := container.KafkaSeedBroker(ctx)
	if err != nil {
		t.Fatalf("obtendo seed broker: %v", err)
	}
	return []string{broker}
}

// RabbitMQURL sobe um RabbitMQ em container efêmero e devolve a URL AMQP.
// Container derruba no cleanup.
func RabbitMQURL(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	container, err := tcrabbit.Run(ctx,
		"rabbitmq:3.13-management-alpine",
		tcrabbit.WithAdminUsername("ndovu"),
		tcrabbit.WithAdminPassword("ndovu"),
	)
	if err != nil {
		t.Fatalf("subindo RabbitMQ testcontainer: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	})

	url, err := container.AmqpURL(ctx)
	if err != nil {
		t.Fatalf("obtendo AMQP URL: %v", err)
	}
	return url
}
