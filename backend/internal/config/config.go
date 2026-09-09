// Package config carrega e valida a configuração via variáveis de ambiente.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config é a configuração completa da API e do writer.
type Config struct {
	Port            int
	CORSOrigins     string // "*" ou lista separada por vírgula
	MaxBodyBytes    int64
	ShutdownTimeout time.Duration
	LogLevel        string

	// Control plane (Postgres): usuários do backoffice e chaves de API
	PostgresURL        string
	AuthSecret         string        // segredo HS256 do JWT local
	AuthTokenTTL       time.Duration // validade do token de sessão
	AdminEmail         string        // bootstrap do primeiro admin
	AdminPassword      string
	BootstrapIngestKey string        // chave de ingestão criada quando não existe nenhuma
	KeyCacheTTL        time.Duration // cache de validação de X-Api-Key

	// ClickHouse (armazém analítico de traces)
	ClickHouseAddr     string // host:porta do protocolo nativo
	ClickHouseDB       string
	ClickHouseUser     string
	ClickHousePassword string

	// Buffer de ingestão (NATS JetStream ou Apache Kafka)
	StreamBackend string // "nats" (default) | "kafka"

	// NATS JetStream (usado quando StreamBackend == "nats")
	NatsURL       string
	StreamMaxAge  time.Duration // janela de replay do stream
	WriterBatch   int           // mensagens por fetch do writer
	WriterMaxWait time.Duration // espera máxima para completar um fetch

	// Kafka (usado quando StreamBackend == "kafka")
	KafkaBrokers       string        // lista separada por vírgula
	KafkaEventsTopic   string        // tópico principal de ingestão
	KafkaDLQTopic      string        // tópico de dead-letter
	KafkaConsumerGroup string        // grupo do writer
	KafkaPartitions    int           // partições do tópico de eventos
	KafkaReplication   int           // fator de replicação dos tópicos
	KafkaRetention     time.Duration // janela de replay no tópico de eventos
	KafkaMaxPoll       int           // máx. mensagens por ciclo do consumer
	KafkaFetchMaxWait  time.Duration // espera máxima por batch no consumer

	// RabbitMQ (usado quando StreamBackend == "rabbitmq")
	RabbitMQURL          string // amqp://user:pass@host:5672/vhost
	RabbitMQEventsQueue  string // fila principal de ingestão
	RabbitMQDLQQueue     string // fila de dead-letter
	RabbitMQConsumerName string // consumer tag do writer

	// Rate limit por API key (requests/segundo). 0 desliga.
	IngestRateRPS float64
	// Intervalo de avaliação das regras de alerta (roda no writer).
	AlertsInterval time.Duration
	// Intervalo de refresh do cache de sampling rules no writer.
	SamplingRefresh time.Duration
	// Intervalo de avaliação das regras de anomalia no writer.
	AnomalyInterval time.Duration

	// Session snapshots (replay MVP). S3-compatible: MinIO local ou AWS S3.
	// Se S3Endpoint vazio, snapshots ficam desligados (POST /v1/snapshots
	// responde 503).
	S3Endpoint     string
	S3AccessKey    string
	S3SecretKey    string
	S3UseSSL       bool
	SnapshotBucket string

	// Mailer (digest semanal). Provider "" (auto) escolhe SendGrid se a chave
	// estiver setada, senão SMTP, senão noop (loga sem enviar).
	MailerProvider string
	SMTPHost       string
	SMTPPort       int
	SMTPUser       string
	SMTPPassword   string
	SMTPFrom       string
	SendGridAPIKey string

	// Digest: destinatários (comma-separated) + agendamento (weekday + hora UTC).
	// URL do dashboard entra nos links do e-mail.
	DigestRecipients []string
	DigestDashboard  string
	DigestWeekday    time.Weekday // 0=domingo
	DigestHourUTC    int
	DigestMinuteUTC  int
	DigestTickEvery  time.Duration
}

// Load lê o ambiente e aplica defaults sãos para a POC.
func Load() (Config, error) {
	cfg := Config{
		Port:            envInt("NDOVU_PORT", 8080),
		CORSOrigins:     envStr("NDOVU_CORS_ORIGINS", "*"),
		MaxBodyBytes:    int64(envInt("NDOVU_MAX_BODY_BYTES", 1<<20)), // 1 MB
		ShutdownTimeout: 15 * time.Second,
		LogLevel:        envStr("NDOVU_LOG_LEVEL", "info"),

		ClickHouseAddr:     envStr("NDOVU_CLICKHOUSE_ADDR", "localhost:9000"),
		ClickHouseDB:       envStr("NDOVU_CLICKHOUSE_DB", "ndovu"),
		ClickHouseUser:     envStr("NDOVU_CLICKHOUSE_USER", "ndovu"),
		ClickHousePassword: envStr("NDOVU_CLICKHOUSE_PASSWORD", "ndovu"),

		StreamBackend: envStr("NDOVU_STREAM_BACKEND", "nats"),

		NatsURL:       envStr("NDOVU_NATS_URL", "nats://localhost:4222"),
		StreamMaxAge:  time.Duration(envInt("NDOVU_STREAM_MAX_AGE_HOURS", 48)) * time.Hour,
		WriterBatch:   envInt("NDOVU_WRITER_BATCH", 64),
		WriterMaxWait: time.Duration(envInt("NDOVU_WRITER_MAX_WAIT_MS", 1000)) * time.Millisecond,

		KafkaBrokers:       envStr("NDOVU_KAFKA_BROKERS", "localhost:9092"),
		KafkaEventsTopic:   envStr("NDOVU_KAFKA_EVENTS_TOPIC", "ndovu.events"),
		KafkaDLQTopic:      envStr("NDOVU_KAFKA_DLQ_TOPIC", "ndovu.dlq"),
		KafkaConsumerGroup: envStr("NDOVU_KAFKA_CONSUMER_GROUP", "ndovu-writer"),
		KafkaPartitions:    envInt("NDOVU_KAFKA_PARTITIONS", 32),
		KafkaReplication:   envInt("NDOVU_KAFKA_REPLICATION_FACTOR", 3),
		KafkaRetention:     time.Duration(envInt("NDOVU_KAFKA_RETENTION_HOURS", 48)) * time.Hour,
		KafkaMaxPoll:       envInt("NDOVU_KAFKA_MAX_POLL_RECORDS", 64),
		KafkaFetchMaxWait:  time.Duration(envInt("NDOVU_KAFKA_FETCH_MAX_WAIT_MS", 1000)) * time.Millisecond,

		RabbitMQURL:          envStr("NDOVU_RABBITMQ_URL", "amqp://ndovu:ndovu@localhost:5672/"),
		RabbitMQEventsQueue:  envStr("NDOVU_RABBITMQ_EVENTS_QUEUE", "ndovu.events"),
		RabbitMQDLQQueue:     envStr("NDOVU_RABBITMQ_DLQ_QUEUE", "ndovu.dlq"),
		RabbitMQConsumerName: envStr("NDOVU_RABBITMQ_CONSUMER_NAME", "ndovu-writer"),

		PostgresURL:        envStr("NDOVU_POSTGRES_URL", "postgres://ndovu:ndovu@localhost:5432/ndovu?sslmode=disable"),
		AuthSecret:         envStr("NDOVU_AUTH_SECRET", "dev-secret-troque-em-producao"),
		AuthTokenTTL:       time.Duration(envInt("NDOVU_AUTH_TOKEN_TTL_HOURS", 8)) * time.Hour,
		AdminEmail:         envStr("NDOVU_ADMIN_EMAIL", "admin@ndovu.local"),
		AdminPassword:      envStr("NDOVU_ADMIN_PASSWORD", "admin12345"),
		BootstrapIngestKey: envStr("NDOVU_BOOTSTRAP_INGEST_KEY", "dev-ingest-key"),
		KeyCacheTTL:        time.Duration(envInt("NDOVU_KEY_CACHE_TTL_SECONDS", 30)) * time.Second,

		IngestRateRPS:   float64(envInt("NDOVU_INGEST_RATE_RPS", 50)),
		AlertsInterval:  time.Duration(envInt("NDOVU_ALERTS_INTERVAL_SECONDS", 60)) * time.Second,
		SamplingRefresh: time.Duration(envInt("NDOVU_SAMPLING_REFRESH_SECONDS", 30)) * time.Second,
		AnomalyInterval: time.Duration(envInt("NDOVU_ANOMALY_INTERVAL_SECONDS", 300)) * time.Second,

		S3Endpoint:     envStr("NDOVU_S3_ENDPOINT", ""),
		S3AccessKey:    envStr("NDOVU_S3_ACCESS_KEY", ""),
		S3SecretKey:    envStr("NDOVU_S3_SECRET_KEY", ""),
		S3UseSSL:       envStr("NDOVU_S3_USE_SSL", "false") == "true",
		SnapshotBucket: envStr("NDOVU_SNAPSHOT_BUCKET", "ndovu-snapshots"),

		MailerProvider: envStr("NDOVU_MAILER_PROVIDER", ""),
		SMTPHost:       envStr("NDOVU_SMTP_HOST", ""),
		SMTPPort:       envInt("NDOVU_SMTP_PORT", 1025),
		SMTPUser:       envStr("NDOVU_SMTP_USER", ""),
		SMTPPassword:   envStr("NDOVU_SMTP_PASSWORD", ""),
		SMTPFrom:       envStr("NDOVU_SMTP_FROM", ""),
		SendGridAPIKey: envStr("NDOVU_SENDGRID_API_KEY", ""),

		DigestRecipients: splitCSV(envStr("NDOVU_DIGEST_RECIPIENTS", "")),
		DigestDashboard:  envStr("NDOVU_DIGEST_DASHBOARD_URL", "http://localhost:13000"),
		DigestWeekday:    time.Weekday(envInt("NDOVU_DIGEST_WEEKDAY", int(time.Sunday))),
		DigestHourUTC:    envInt("NDOVU_DIGEST_HOUR_UTC", 20),
		DigestMinuteUTC:  envInt("NDOVU_DIGEST_MINUTE_UTC", 0),
		DigestTickEvery:  time.Duration(envInt("NDOVU_DIGEST_TICK_SECONDS", 300)) * time.Second,
	}
	return cfg, nil
}

// splitCSV divide "a@x.com, b@y.com" em []string sem vazios/espaços.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
