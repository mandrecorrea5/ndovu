package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/usecase"
)

type captureSpanStream struct {
	batches []domain.SpanBatch
	err     error
}

func (c *captureSpanStream) PublishSpans(_ context.Context, b domain.SpanBatch) error {
	if c.err != nil {
		return c.err
	}
	c.batches = append(c.batches, b)
	return nil
}

type otlpFixture struct {
	router http.Handler
	stream *captureSpanStream
	server string // chave de app com allow_otlp
	brows  string // chave de app de browser
}

func newOTLPFixture(t *testing.T, opts OTLPOptions) *otlpFixture {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newNopKeyStore()
	keys := usecase.NewAPIKeyService(store, 30*time.Second, 0, logger, "test-secret")
	server, err := keys.CreateKey(context.Background(), "", "felpma-api", "", "")
	if err != nil {
		t.Fatal(err)
	}
	browser, err := keys.CreateKey(context.Background(), "", "felpma", "", "")
	if err != nil {
		t.Fatal(err)
	}
	k := store.keys[server.ID]
	k.AllowOTLP = true
	store.keys[server.ID] = k

	stream := &captureSpanStream{}
	router := NewOTLPRouter(usecase.NewSpanIngestService(stream, logger), keys, nil, logger, opts)
	return &otlpFixture{router: router, stream: stream, server: server.Key, brows: browser.Key}
}

func exportBody(t *testing.T, json bool, start time.Time) []byte {
	t.Helper()
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "felpma-api")
	spans := rs.ScopeSpans().AppendEmpty().Spans()
	ok := spans.AppendEmpty()
	ok.SetTraceID(pcommon.TraceID{1})
	ok.SetSpanID(pcommon.SpanID{1})
	ok.SetName("GET /contato")
	ok.SetStartTimestamp(pcommon.NewTimestampFromTime(start))
	ok.SetEndTimestamp(pcommon.NewTimestampFromTime(start.Add(time.Millisecond)))
	ok.Attributes().PutStr("http.request.header.authorization", "Bearer x")
	bad := spans.AppendEmpty() // sem ids: vai para o partial_success
	bad.SetName("órfão")

	req := ptraceotlp.NewExportRequestFromTraces(td)
	var body []byte
	var err error
	if json {
		body, err = req.MarshalJSON()
	} else {
		body, err = req.MarshalProto()
	}
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func (f *otlpFixture) post(body []byte, key, contentType, encoding string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	return rr
}

func gz(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestOTLPAceitaProtobufGzipComPartialSuccess(t *testing.T) {
	f := newOTLPFixture(t, OTLPOptions{})
	rr := f.post(gz(t, exportBody(t, false, time.Now())), f.server, "application/x-protobuf", "gzip")
	requireStatus(t, rr, http.StatusOK)
	if ct := rr.Header().Get("Content-Type"); ct != "application/x-protobuf" {
		t.Fatalf("Content-Type = %q", ct)
	}
	resp := ptraceotlp.NewExportResponse()
	if err := resp.UnmarshalProto(rr.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if resp.PartialSuccess().RejectedSpans() != 1 {
		t.Fatalf("rejected = %d, quero 1", resp.PartialSuccess().RejectedSpans())
	}
	if len(f.stream.batches) != 1 || len(f.stream.batches[0].Spans) != 1 {
		t.Fatalf("publicado = %+v", f.stream.batches)
	}
	s := f.stream.batches[0].Spans[0]
	if s.App != "felpma-api" || s.ServiceName != "felpma-api" {
		t.Fatalf("app=%s service=%s", s.App, s.ServiceName)
	}
	if s.Attrs["http.request.header.authorization"] != usecase.RedactedValue {
		t.Fatal("header sensível chegou ao stream sem redação")
	}
}

func TestOTLPAceitaJSON(t *testing.T) {
	f := newOTLPFixture(t, OTLPOptions{})
	rr := f.post(exportBody(t, true, time.Now()), f.server, "application/json", "")
	requireStatus(t, rr, http.StatusOK)
	resp := ptraceotlp.NewExportResponse()
	if err := resp.UnmarshalJSON(rr.Body.Bytes()); err != nil {
		t.Fatalf("resposta não é OTLP/JSON: %v (%s)", err, rr.Body.String())
	}
}

func TestOTLPRecusas(t *testing.T) {
	f := newOTLPFixture(t, OTLPOptions{MaxBodyBytes: 1 << 20, MaxDecodedSize: 1024})
	body := exportBody(t, false, time.Now())
	bomb := gz(t, bytes.Repeat([]byte{0}, 1<<20)) // ~1 KB comprimido, 1 MB aberto

	cases := []struct {
		name              string
		body              []byte
		key, ct, encoding string
		want              int
	}{
		{"sem chave", body, "", "application/x-protobuf", "", http.StatusUnauthorized},
		{"chave inválida", body, "ndk_nao_existe", "application/x-protobuf", "", http.StatusUnauthorized},
		{"app de browser", body, f.brows, "application/x-protobuf", "", http.StatusForbidden},
		{"content-type", body, f.server, "text/plain", "", http.StatusUnsupportedMediaType},
		{"encoding", body, f.server, "application/x-protobuf", "br", http.StatusUnsupportedMediaType},
		{"gzip inválido", body, f.server, "application/x-protobuf", "gzip", http.StatusBadRequest},
		{"gzip bomb", bomb, f.server, "application/x-protobuf", "gzip", http.StatusRequestEntityTooLarge},
		{"protobuf inválido", []byte("lixo"), f.server, "application/x-protobuf", "", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := f.post(c.body, c.key, c.ct, c.encoding)
			requireStatus(t, rr, c.want)
		})
	}
	if len(f.stream.batches) != 0 {
		t.Fatalf("nada recusado deveria chegar ao stream: %+v", f.stream.batches)
	}
}

func TestOTLPStreamForaResponde503ComRetryAfter(t *testing.T) {
	f := newOTLPFixture(t, OTLPOptions{})
	f.stream.err = errors.New("nats fora")
	rr := f.post(exportBody(t, false, time.Now()), f.server, "application/x-protobuf", "")
	requireStatus(t, rr, http.StatusServiceUnavailable)
	if rr.Header().Get("Retry-After") == "" {
		t.Fatal("503 sem Retry-After: o exporter não saberia quando reenviar")
	}
}

func TestOTLPNaoAbreSpanProprio(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	f := newOTLPFixture(t, OTLPOptions{})
	// Mesmo com um traceparent amostrado chegando, nada é gravado.
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(exportBody(t, false, time.Now())))
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Api-Key", f.server)
	req.Header.Set("traceparent", browserTraceparent)
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	requireStatus(t, rr, http.StatusOK)
	if spans := rec.Ended(); len(spans) != 0 {
		t.Fatalf("receptor OTLP gerou %d spans — realimentaria o loop quando o Ndovu exporta para si", len(spans))
	}
}
