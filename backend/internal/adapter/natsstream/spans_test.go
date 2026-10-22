package natsstream

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

func spanBatch(app string, n, attrBytes int) domain.SpanBatch {
	b := domain.SpanBatch{App: app}
	for i := range n {
		b.Spans = append(b.Spans, domain.Span{
			App: app, TraceID: fmt.Sprintf("%032x", i), SpanID: fmt.Sprintf("%016x", i),
			Name: "GET /x", StartTime: time.Now().UTC(),
			Attrs: map[string]string{"blob": strings.Repeat("x", attrBytes)},
		})
	}
	return b
}

func setupSpans(t *testing.T) (context.Context, jetstream.JetStream, *Publisher) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	nc, err := Connect(startEmbeddedNATS(t))
	if err != nil {
		t.Fatalf("conectando: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := EnsureStream(ctx, nc, time.Hour)
	if err != nil {
		t.Fatalf("garantindo stream: %v", err)
	}
	return ctx, js, NewPublisher(js)
}

// consumeSpans roda o consumidor até chegarem `want` spans.
func consumeSpans(t *testing.T, ctx context.Context, js jetstream.JetStream, want int) (msgs int, spans []domain.Span) {
	t.Helper()
	consumer, err := NewSpanConsumer(ctx, js, 50, 300*time.Millisecond, slog.Default())
	if err != nil {
		t.Fatalf("criando consumidor: %v", err)
	}
	var mu sync.Mutex
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = consumer.Run(runCtx, func(_ context.Context, batches []domain.SpanBatch) error {
			mu.Lock()
			defer mu.Unlock()
			msgs += len(batches)
			for _, b := range batches {
				spans = append(spans, b.Spans...)
			}
			if len(spans) >= want {
				stop()
			}
			return nil
		})
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("timeout: chegaram %d de %d spans", len(spans), want)
	}
	return msgs, spans
}

func TestSpansIdaEVolta(t *testing.T) {
	ctx, js, pub := setupSpans(t)
	if err := pub.PublishSpans(ctx, spanBatch("felpma-api", 3, 10)); err != nil {
		t.Fatal(err)
	}
	// Evento no mesmo stream não pode cair no consumidor de spans.
	if err := pub.Publish(ctx, testBatch("e-1")); err != nil {
		t.Fatal(err)
	}
	_, spans := consumeSpans(t, ctx, js, 3)
	if len(spans) != 3 || spans[0].App != "felpma-api" || spans[0].Attrs["blob"] != strings.Repeat("x", 10) {
		t.Fatalf("spans = %+v", spans)
	}
}

func TestSpansGrandesSaoPartidosAbaixoDoLimite(t *testing.T) {
	ctx, js, pub := setupSpans(t)
	// 40 spans de ~64 KB ≈ 2,5 MB: acima do max_payload de 1 MB do NATS.
	if err := pub.PublishSpans(ctx, spanBatch("api", 40, 64<<10)); err != nil {
		t.Fatalf("lote grande deveria ser partido, não recusado: %v", err)
	}
	msgs, spans := consumeSpans(t, ctx, js, 40)
	if len(spans) != 40 {
		t.Fatalf("spans = %d, quero 40", len(spans))
	}
	if msgs < 3 {
		t.Fatalf("chegou em %d mensagens; 2,5 MB precisa de pelo menos 3", msgs)
	}
}

func TestSpansReenvioIdenticoEDeduplicado(t *testing.T) {
	ctx, js, pub := setupSpans(t)
	b := spanBatch("api", 2, 10)
	for range 2 {
		if err := pub.PublishSpans(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	info, err := js.Stream(ctx, StreamName)
	if err != nil {
		t.Fatal(err)
	}
	state, err := info.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.State.Msgs != 1 {
		t.Fatalf("mensagens no stream = %d, quero 1 (reenvio do exporter deduplicado)", state.State.Msgs)
	}
}
