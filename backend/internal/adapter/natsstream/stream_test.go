package natsstream

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// startEmbeddedNATS sobe um nats-server com JetStream em memória para o teste.
func startEmbeddedNATS(t *testing.T) string {
	t.Helper()
	opts := &natsserver.Options{
		Host:      "127.0.0.1",
		Port:      -1, // porta aleatória
		JetStream: true,
		StoreDir:  t.TempDir(),
		NoLog:     true,
		NoSigs:    true,
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("criando nats-server embedded: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats-server embedded não subiu")
	}
	t.Cleanup(srv.Shutdown)
	return srv.ClientURL()
}

func testBatch(id string) domain.IngestBatch {
	return domain.IngestBatch{
		Session: domain.Session{SessionID: "s-1", UserID: "u-1", App: "portal-cliente"},
		Events: []domain.TraceEvent{{
			ID:         id,
			Type:       domain.EventAction,
			Name:       "clicou",
			OccurredAt: time.Now().UTC(),
		}},
	}
}

// TestPublishConsumeRoundTrip cobre o pipeline completo:
// publisher → stream durável → pull consumer → handler → ack.
func TestPublishConsumeRoundTrip(t *testing.T) {
	url := startEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	nc, err := Connect(url)
	if err != nil {
		t.Fatalf("conectando: %v", err)
	}
	defer nc.Close()

	js, err := EnsureStream(ctx, nc, time.Hour)
	if err != nil {
		t.Fatalf("garantindo stream: %v", err)
	}

	pub := NewPublisher(js)
	if err := pub.Publish(ctx, testBatch("e-1")); err != nil {
		t.Fatalf("publicando: %v", err)
	}
	if err := pub.Publish(ctx, testBatch("e-2")); err != nil {
		t.Fatalf("publicando: %v", err)
	}

	consumer, err := NewConsumer(ctx, js, 10, 500*time.Millisecond, slog.Default())
	if err != nil {
		t.Fatalf("criando consumidor: %v", err)
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
			if received.Load() >= 2 {
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
	if received.Load() != 2 {
		t.Fatalf("eventos recebidos = %d, esperado 2", received.Load())
	}
}

// TestPublishDeduplicaRetry garante que a re-publicação do MESMO lote
// (retry da API) é descartada pelo JetStream via Nats-Msg-Id.
func TestPublishDeduplicaRetry(t *testing.T) {
	url := startEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	nc, err := Connect(url)
	if err != nil {
		t.Fatalf("conectando: %v", err)
	}
	defer nc.Close()

	js, err := EnsureStream(ctx, nc, time.Hour)
	if err != nil {
		t.Fatalf("garantindo stream: %v", err)
	}

	pub := NewPublisher(js)
	batch := testBatch("e-dup")
	for i := 0; i < 3; i++ {
		if err := pub.Publish(ctx, batch); err != nil {
			t.Fatalf("publicando (%d): %v", i, err)
		}
	}

	stream, err := js.Stream(ctx, StreamName)
	if err != nil {
		t.Fatalf("consultando stream: %v", err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatalf("info do stream: %v", err)
	}
	if info.State.Msgs != 1 {
		t.Fatalf("mensagens no stream = %d, esperado 1 (dedup por Msg-Id)", info.State.Msgs)
	}
}

// TestConsumerReentregaAposFalha garante at-least-once: handler falhou → nak →
// a mensagem volta e é processada de novo.
func TestConsumerReentregaAposFalha(t *testing.T) {
	url := startEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	nc, err := Connect(url)
	if err != nil {
		t.Fatalf("conectando: %v", err)
	}
	defer nc.Close()

	js, err := EnsureStream(ctx, nc, time.Hour)
	if err != nil {
		t.Fatalf("garantindo stream: %v", err)
	}
	if err := NewPublisher(js).Publish(ctx, testBatch("e-retry")); err != nil {
		t.Fatalf("publicando: %v", err)
	}

	consumer, err := NewConsumer(ctx, js, 10, 500*time.Millisecond, slog.Default())
	if err != nil {
		t.Fatalf("criando consumidor: %v", err)
	}

	var attempts atomic.Int32
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = consumer.Run(runCtx, func(_ context.Context, _ []domain.IngestBatch) error {
			if attempts.Add(1) == 1 {
				return context.DeadlineExceeded // simula ClickHouse fora do ar
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
