package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestDBSpanName(t *testing.T) {
	cases := []struct{ sql, op, target string }{
		{"SELECT * FROM trace_events FINAL WHERE trace_id = $1", "SELECT", "trace_events"},
		{"INSERT INTO trace_events\n\t(id, session_id)", "INSERT", "trace_events"},
		{"  update api_keys set revoked_at = now() where id = $1", "UPDATE", "api_keys"},
		{"ALTER TABLE trace_events ADD COLUMN IF NOT EXISTS release String", "ALTER", "trace_events"},
		{"CREATE TABLE IF NOT EXISTS ndovu.trace_events (id UUID)", "CREATE", "ndovu.trace_events"},
		{"DELETE FROM sampling_rules WHERE id = $1", "DELETE", "sampling_rules"},
		{"SELECT 1", "SELECT", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		op, target := DBSpanName(c.sql)
		if op != c.op || target != c.target {
			t.Errorf("DBSpanName(%q) = (%q, %q), quer (%q, %q)", c.sql, op, target, c.op, c.target)
		}
	}
}

func TestTracingEnabled(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "")
	if TracingEnabled() {
		t.Fatal("sem endpoint o tracing deveria ficar desligado")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	if !TracingEnabled() {
		t.Fatal("com endpoint o tracing deveria ligar")
	}
	t.Setenv("OTEL_SDK_DISABLED", "true")
	if TracingEnabled() {
		t.Fatal("OTEL_SDK_DISABLED=true deveria vencer o endpoint")
	}
}

func TestSetupTracingDesligadoInstalaPropagador(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	shutdown, err := SetupTracing(context.Background(), "teste", "dev", slog.Default())
	if err != nil {
		t.Fatalf("SetupTracing: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	fields := otel.GetTextMapPropagator().Fields()
	found := false
	for _, f := range fields {
		found = found || f == "traceparent"
	}
	if !found {
		t.Fatalf("propagador W3C não instalado; campos = %v", fields)
	}
}

func TestLogComContextoLevaTraceID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(traceHandler{slog.NewJSONHandler(&buf, nil)}).With("svc", "x")
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("t").Start(context.Background(), "op")
	logger.InfoContext(ctx, "com span")
	span.End()
	logger.Info("sem span")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	var withSpan, without map[string]any
	_ = json.Unmarshal(lines[0], &withSpan)
	_ = json.Unmarshal(lines[1], &without)
	if withSpan["trace_id"] != span.SpanContext().TraceID().String() || withSpan["span_id"] != span.SpanContext().SpanID().String() {
		t.Fatalf("log com contexto sem trace_id/span_id corretos: %v", withSpan)
	}
	if withSpan["svc"] != "x" {
		t.Fatalf("WithAttrs perdido pelo traceHandler: %v", withSpan)
	}
	if _, ok := without["trace_id"]; ok {
		t.Fatalf("log sem span não deveria ter trace_id: %v", without)
	}
}
