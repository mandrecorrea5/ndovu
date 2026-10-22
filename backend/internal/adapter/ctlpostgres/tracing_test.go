package ctlpostgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestQueryTracer(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	tr := queryTracer{}
	run := func(sql string, err error) sdktrace.ReadOnlySpan {
		ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: sql})
		tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: err})
		spans := rec.Ended()
		return spans[len(spans)-1]
	}

	ok := run("SELECT id, app FROM api_keys WHERE key_hash = $1", nil)
	if ok.Name() != "SELECT api_keys" || ok.Status().Code == codes.Error {
		t.Fatalf("span = %q status %v", ok.Name(), ok.Status().Code)
	}
	if run("SELECT 1 FROM users WHERE id = $1", pgx.ErrNoRows).Status().Code == codes.Error {
		t.Fatal("ErrNoRows não é falha e não deveria marcar o span")
	}
	if run("UPDATE users SET name = $1", errors.New("deadlock")).Status().Code != codes.Error {
		t.Fatal("erro real deveria marcar o span")
	}
}
