package clickhouse

import (
	"context"
	"errors"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// fakeConn/fakeRows/fakeBatch implementam só o que o decorator chama; o resto
// da interface fica no valor nil embutido (pânico se alguém usar = teste falha).
type fakeConn struct {
	driver.Conn
	execErr error
}

func (f fakeConn) Exec(context.Context, string, ...any) error { return f.execErr }
func (f fakeConn) Query(context.Context, string, ...any) (driver.Rows, error) {
	return &fakeRows{}, nil
}
func (f fakeConn) PrepareBatch(context.Context, string, ...driver.PrepareBatchOption) (driver.Batch, error) {
	return &fakeBatch{}, nil
}

type fakeRows struct {
	driver.Rows
	closed bool
}

func (r *fakeRows) Close() error { r.closed = true; return nil }
func (r *fakeRows) Err() error   { return nil }

type fakeBatch struct {
	driver.Batch
	rows int
}

func (b *fakeBatch) Append(...any) error { b.rows++; return nil }
func (b *fakeBatch) Send() error         { return nil }
func (b *fakeBatch) Abort() error        { return nil }
func (b *fakeBatch) Rows() int           { return b.rows }

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return rec
}

func TestTracedConnExecMarcaErro(t *testing.T) {
	rec := recordSpans(t)
	conn := tracedConn{fakeConn{execErr: errors.New("boom")}}
	if err := conn.Exec(context.Background(), "ALTER TABLE trace_events DELETE WHERE user_id = ?"); err == nil {
		t.Fatal("erro do driver deveria passar adiante")
	}
	span := rec.Ended()[0]
	if span.Name() != "ALTER trace_events" || span.Status().Code != codes.Error {
		t.Fatalf("span = %q status %v", span.Name(), span.Status().Code)
	}
}

func TestTracedConnQueryFechaSpanNoClose(t *testing.T) {
	rec := recordSpans(t)
	rows, err := tracedConn{fakeConn{}}.Query(context.Background(), "SELECT * FROM trace_events WHERE trace_id = $1")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(rec.Ended()); n != 0 {
		t.Fatalf("span não deveria fechar antes de ler as linhas; fechados = %d", n)
	}
	_ = rows.Close()
	if spans := rec.Ended(); len(spans) != 1 || spans[0].Name() != "SELECT trace_events" {
		t.Fatalf("span do SELECT deveria fechar no Close")
	}
}

func TestTracedBatchFechaUmaVezNoSend(t *testing.T) {
	rec := recordSpans(t)
	batch, err := tracedConn{fakeConn{}}.PrepareBatch(context.Background(), "INSERT INTO trace_events (id)")
	if err != nil {
		t.Fatal(err)
	}
	_ = batch.Append(1)
	_ = batch.Append(2)
	_ = batch.Send()
	_ = batch.Abort() // padrão `defer batch.Abort()` não pode fechar de novo
	spans := rec.Ended()
	if len(spans) != 1 || spans[0].Name() != "INSERT trace_events" {
		t.Fatalf("esperava 1 span INSERT, veio %d", len(spans))
	}
	for _, a := range spans[0].Attributes() {
		if a.Key == "ndovu.batch.rows" && a.Value.AsInt64() == 2 {
			return
		}
	}
	t.Fatalf("ndovu.batch.rows=2 ausente: %v", spans[0].Attributes())
}
