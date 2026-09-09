//go:build integration

package rabbitmq_test

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/rabbitmq"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/testenv"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func testCfg(t *testing.T) (rabbitmq.Config, *rabbitmq.Publisher, *rabbitmq.Consumer) {
	t.Helper()
	cfg := rabbitmq.Config{
		URL:          testenv.RabbitMQURL(t),
		EventsQueue:  "ndovu.events.test",
		DLQQueue:     "ndovu.dlq.test",
		ConsumerName: "ndovu-writer-test",
		MaxPoll:      4,
		MaxWait:      500 * time.Millisecond,
	}
	conn, ch, err := rabbitmq.Connect(cfg.URL)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := rabbitmq.EnsureQueues(ch, cfg); err != nil {
		t.Fatalf("EnsureQueues: %v", err)
	}
	pub := rabbitmq.NewPublisher(ch, cfg)
	consumer, err := rabbitmq.NewConsumer(ch, cfg, discardLogger)
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	return cfg, pub, consumer
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
// publisher → fila durável → consumer → handler → ack.
func TestPublishConsumeRoundTrip(t *testing.T) {
	_, pub, consumer := testCfg(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		if err := pub.Publish(ctx, testBatch("e"+string(rune('0'+i)))); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

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

// TestConsumerReentregaAposFalha garante at-least-once: handler falhou →
// Nack(requeue) → a mensagem é reentregue e processada de novo (semântica
// igual ao JetStream nak, no mesmo processo).
func TestConsumerReentregaAposFalha(t *testing.T) {
	_, pub, consumer := testCfg(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := pub.Publish(ctx, testBatch("e-retry")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	var attempts atomic.Int32
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = consumer.Run(runCtx, func(_ context.Context, _ []domain.IngestBatch) error {
			if attempts.Add(1) == 1 {
				return context.DeadlineExceeded // simula ClickHouse fora do ar → requeue
			}
			stop()
			return nil
		})
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timeout esperando a reentrega")
	}
	if attempts.Load() < 2 {
		t.Fatalf("tentativas = %d, esperado >= 2 (reentrega após falha)", attempts.Load())
	}
}
