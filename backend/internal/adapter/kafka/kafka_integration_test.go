//go:build integration

package kafka_test

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/kafka"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/testenv"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func testCfg(t *testing.T) kafka.Config {
	t.Helper()
	brokers := testenv.KafkaBrokers(t)
	return kafka.Config{
		Brokers:           brokers,
		EventsTopic:       "ndovu.events.test",
		DLQTopic:          "ndovu.dlq.test",
		ConsumerGroup:     "ndovu-writer-test-" + uuid.New().String(),
		Partitions:        4,
		ReplicationFactor: 1,
		RetentionHours:    time.Hour,
		MaxPollRecords:    4,
		FetchMaxWait:      500 * time.Millisecond,
	}
}

func testBatch(id string) domain.IngestBatch {
	return domain.IngestBatch{
		Session: domain.Session{SessionID: "s-" + id, UserID: "u-1", App: "portal-cliente"},
		Events: []domain.TraceEvent{{
			ID:         id,
			Type:       domain.EventAction,
			Name:       "clicou",
			OccurredAt: time.Now().UTC(),
		}},
	}
}

// TestPublishConsumeRoundTrip cobre o pipeline completo:
// publisher → tópico particionado → consumer group → handler → commit.
func TestPublishConsumeRoundTrip(t *testing.T) {
	cfg := testCfg(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	logger := discardLogger

	if err := kafka.EnsureTopics(ctx, cfg, logger); err != nil {
		t.Fatalf("EnsureTopics: %v", err)
	}

	pub, err := kafka.NewPublisher(cfg)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer pub.Close()

	// Mesma sessão -> mesma partição; garante ordem e processamento em 1 consumer.
	for i := 0; i < 3; i++ {
		if err := pub.Publish(ctx, testBatch("e"+string(rune('0'+i)))); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	consumer, err := kafka.NewConsumer(cfg, logger)
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	defer consumer.Close()

	var received atomic.Int32
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = consumer.Run(runCtx, func(_ context.Context, batches []domain.IngestBatch) error {
			for _, b := range batches {
				received.Add(int32(len(b.Events)))
			}
			if received.Load() >= 3 {
				stop()
			}
			return nil
		})
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timeout esperando o consumo")
	}
	if received.Load() != 3 {
		t.Fatalf("eventos recebidos = %d, esperado 3", received.Load())
	}
}

// TestConsumerRetornaErroAoFalhar garante at-least-once: quando o handler
// falha, o Run retorna o erro SEM commitar o offset — o processo reinicia e o
// consumer group re-entrega do último offset commitado. Diferente do
// JetStream (nak + reentrega no mesmo processo), o Kafka delega a reentrega
// ao restart, que é o comportamento idiomático de consumer group.
func TestConsumerRetornaErroAoFalhar(t *testing.T) {
	cfg := testCfg(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	logger := discardLogger

	if err := kafka.EnsureTopics(ctx, cfg, logger); err != nil {
		t.Fatalf("EnsureTopics: %v", err)
	}

	pub, err := kafka.NewPublisher(cfg)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer pub.Close()
	if err := pub.Publish(ctx, testBatch("e-retry")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	consumer, err := kafka.NewConsumer(cfg, logger)
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	defer consumer.Close()

	var attempts atomic.Int32
	err = consumer.Run(ctx, func(_ context.Context, _ []domain.IngestBatch) error {
		attempts.Add(1)
		return context.DeadlineExceeded // simula ClickHouse fora do ar
	})
	if err == nil {
		t.Fatal("esperado erro (offset não commitado → reentrega no restart), mas Run retornou nil")
	}
	if attempts.Load() == 0 {
		t.Fatal("handler nunca foi chamado")
	}
}
