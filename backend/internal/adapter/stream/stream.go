// Package stream é a fábrica do buffer durável de ingestão. Ele abstrai a
// escolha entre NATS JetStream e Apache Kafka atrás das mesmas interfaces de
// domínio, permitindo trocar o backend por configuração (`NDOVU_STREAM_BACKEND`)
// sem tocar no composition root ou nas regras de negócio.
//
//   - "nats" (default)  → `internal/adapter/natsstream`
//   - "kafka"           → `internal/adapter/kafka`
//
// O `Runtime` devolvido expõe um `Publisher` (o lado `domain.EventStream` da
// API de ingestão) e um `Consumer` (o lado do writer, plugável no
// `WriteService.HandleBatches`). Ambos os backends são at-least-once: a
// duplicação final é resolvida pelo `ReplacingMergeTree` no ClickHouse.
package stream

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/kafka"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/natsstream"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/rabbitmq"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// Backend nomeia o buffer de ingestão suportado.
type Backend string

const (
	// BackendNATS é o JetStream (default, legacy).
	BackendNATS Backend = "nats"
	// BackendKafka é o Apache Kafka.
	BackendKafka Backend = "kafka"
	// BackendRabbitMQ é o RabbitMQ.
	BackendRabbitMQ Backend = "rabbitmq"
)

// Consumer drena lotes do buffer e os entrega ao handler, com commit/ack só
// após o persistir (at-least-once).
type Consumer interface {
	Run(ctx context.Context, handle func(ctx context.Context, batches []domain.IngestBatch) error) error
}

// Closer libera recursos do backend (conexões, readers, writers).
type Closer interface {
	Close() error
}

// Runtime agrupa as duas pontas do buffer sobre o backend selecionado.
type Runtime struct {
	// Publisher é o lado da API de ingestão (domain.EventStream + DLQ).
	Publisher domain.EventStream
	// DeadLetter reencaminha um lote rejeitado para a fila de reprocesso.
	DeadLetter func(ctx context.Context, batch domain.IngestBatch, reason string) error
	// Consumer é o lado do writer.
	Consumer Consumer
	// Close libera as conexões do backend.
	Close func()
}

// Options parametriza a fábrica.
type Options struct {
	Backend            Backend
	NatsURL            string
	NatsMaxAge         time.Duration
	WriterBatch        int
	WriterMaxWait      time.Duration
	KafkaBrokers       []string
	KafkaEventsTopic   string
	KafkaDLQTopic      string
	KafkaConsumerGroup string
	KafkaPartitions    int
	KafkaReplication   int
	KafkaRetention     time.Duration
	KafkaMaxPoll       int
	KafkaFetchMaxWait  time.Duration

	// RabbitMQ (usado quando Backend == "rabbitmq")
	RabbitMQURL          string
	RabbitMQEventsQueue  string
	RabbitMQDLQQueue     string
	RabbitMQConsumerName string
}

// New monta o Runtime do backend escolhido. Para "kafka", garante a existência
// dos tópicos (events + DLQ) antes de devolver.
func New(ctx context.Context, opts Options, logger *slog.Logger) (*Runtime, error) {
	switch opts.Backend {
	case BackendKafka:
		return newKafka(ctx, opts, logger)
	case BackendNATS:
		return newNATS(opts, logger)
	case BackendRabbitMQ:
		return newRabbitMQ(opts, logger)
	default:
		return nil, fmt.Errorf("NDOVU_STREAM_BACKEND inválido: %q (use nats, kafka ou rabbitmq)", string(opts.Backend))
	}
}

func newNATS(opts Options, logger *slog.Logger) (*Runtime, error) {
	nc, err := natsstream.Connect(opts.NatsURL)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	js, err := natsstream.EnsureStream(ctx, nc, opts.NatsMaxAge)
	if err != nil {
		nc.Close()
		return nil, err
	}
	pub := natsstream.NewPublisher(js)
	consumer, err := natsstream.NewConsumer(ctx, js, opts.WriterBatch, opts.WriterMaxWait, logger)
	if err != nil {
		nc.Close()
		return nil, err
	}
	return &Runtime{
		Publisher: pub,
		DeadLetter: func(ctx context.Context, batch domain.IngestBatch, reason string) error {
			return pub.DeadLetter(ctx, batch, reason)
		},
		Consumer: consumer,
		Close:    func() { nc.Close() },
	}, nil
}

func newKafka(ctx context.Context, opts Options, logger *slog.Logger) (*Runtime, error) {
	cfg := kafka.Config{
		Brokers:           opts.KafkaBrokers,
		EventsTopic:       opts.KafkaEventsTopic,
		DLQTopic:          opts.KafkaDLQTopic,
		ConsumerGroup:     opts.KafkaConsumerGroup,
		Partitions:        opts.KafkaPartitions,
		ReplicationFactor: opts.KafkaReplication,
		RetentionHours:    opts.KafkaRetention,
		MaxPollRecords:    opts.KafkaMaxPoll,
		FetchMaxWait:      opts.KafkaFetchMaxWait,
	}
	if err := kafka.EnsureTopics(ctx, cfg, logger); err != nil {
		return nil, err
	}
	pub, err := kafka.NewPublisher(cfg)
	if err != nil {
		return nil, err
	}
	consumer, err := kafka.NewConsumer(cfg, logger)
	if err != nil {
		pub.Close()
		return nil, err
	}
	return &Runtime{
		Publisher: pub,
		DeadLetter: func(ctx context.Context, batch domain.IngestBatch, reason string) error {
			return pub.DeadLetter(ctx, batch, reason)
		},
		Consumer: consumer,
		Close: func() {
			_ = pub.Close()
			_ = consumer.Close()
		},
	}, nil
}

func newRabbitMQ(opts Options, logger *slog.Logger) (*Runtime, error) {
	cfg := rabbitmq.Config{
		URL:          opts.RabbitMQURL,
		EventsQueue:  opts.RabbitMQEventsQueue,
		DLQQueue:     opts.RabbitMQDLQQueue,
		ConsumerName: opts.RabbitMQConsumerName,
		MaxPoll:      opts.WriterBatch,
		MaxWait:      opts.WriterMaxWait,
	}
	conn, ch, err := rabbitmq.Connect(cfg.URL)
	if err != nil {
		return nil, err
	}
	if err := rabbitmq.EnsureQueues(ch, cfg); err != nil {
		conn.Close()
		return nil, err
	}
	pub := rabbitmq.NewPublisher(ch, cfg)
	consumer, err := rabbitmq.NewConsumer(ch, cfg, logger)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &Runtime{
		Publisher: pub,
		DeadLetter: func(ctx context.Context, batch domain.IngestBatch, reason string) error {
			return pub.DeadLetter(ctx, batch, reason)
		},
		Consumer: consumer,
		Close:    func() { _ = conn.Close() },
	}, nil
}
