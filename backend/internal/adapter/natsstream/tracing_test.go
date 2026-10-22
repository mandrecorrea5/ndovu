package natsstream

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// recordSpans troca o TracerProvider global por um que grava em memória.
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	return rec
}

func spanNamed(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

// TestTraceAtravessaOStream: o span de publicação é filho do request da API e
// o span do writer aponta (link) para ele — o lote no ClickHouse é rastreável
// até o POST /v1/events que o originou.
func TestTraceAtravessaOStream(t *testing.T) {
	rec := recordSpans(t)
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

	reqCtx, reqSpan := otel.Tracer("teste").Start(ctx, "POST /v1/events")
	if err := NewPublisher(js).Publish(reqCtx, testBatch("e-trace")); err != nil {
		t.Fatalf("publicando: %v", err)
	}
	reqSpan.End()

	consumer, err := NewConsumer(ctx, js, 10, 500*time.Millisecond, slog.Default())
	if err != nil {
		t.Fatalf("criando consumidor: %v", err)
	}
	runCtx, stop := context.WithCancel(ctx)
	var handlerSpan trace.SpanContext
	go func() {
		_ = consumer.Run(runCtx, func(ctx context.Context, _ []domain.IngestBatch) error {
			handlerSpan = trace.SpanContextFromContext(ctx)
			stop()
			return nil
		})
	}()
	<-runCtx.Done()
	time.Sleep(50 * time.Millisecond) // span do process fecha após o handler

	spans := rec.Ended()
	send := spanNamed(spans, "send ndovu.events.*")
	process := spanNamed(spans, "process ndovu.events.*")
	if send == nil || process == nil {
		t.Fatalf("spans esperados ausentes; gravados: %d", len(spans))
	}
	if send.Parent().SpanID() != reqSpan.SpanContext().SpanID() || send.SpanKind() != trace.SpanKindProducer {
		t.Fatalf("send deveria ser PRODUCER filho do request")
	}
	if process.Parent().IsValid() {
		t.Fatalf("process deveria ser raiz (lote junta traces diferentes)")
	}
	links := process.Links()
	if len(links) != 1 || links[0].SpanContext.SpanID() != send.SpanContext().SpanID() {
		t.Fatalf("process deveria ter link para o send; links = %+v", links)
	}
	if handlerSpan.SpanID() != process.SpanContext().SpanID() {
		t.Fatalf("handler deveria rodar dentro do span de process")
	}
}
