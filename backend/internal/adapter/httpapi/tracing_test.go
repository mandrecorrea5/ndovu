package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func tracedTestRouter(t *testing.T) (http.Handler, *tracetest.SpanRecorder) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	r := chi.NewRouter()
	r.Use(traceRoute)
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	r.Get("/health", ok)
	r.Route("/v1", func(r chi.Router) {
		r.Get("/sessions/{sessionId}", ok)
		r.Post("/events", ok)
	})
	return withTracing(r), rec
}

const browserTraceparent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

func TestSpanLevaPadraoDaRota(t *testing.T) {
	h, rec := tracedTestRouter(t)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/sessions/abc-123", nil))

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("esperava 1 span, veio %d", len(spans))
	}
	if got := spans[0].Name(); got != "GET /v1/sessions/{sessionId}" {
		t.Fatalf("nome do span = %q — path cru explode a cardinalidade", got)
	}
}

func TestConsultaContinuaTraceRecebido(t *testing.T) {
	h, rec := tracedTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/abc", nil)
	req.Header.Set("traceparent", browserTraceparent)
	h.ServeHTTP(httptest.NewRecorder(), req)

	span := rec.Ended()[0]
	if span.Parent().TraceID().String() != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("rota interna deveria continuar o trace recebido")
	}
}

// Ingestão é pública: o traceparent é do browser de um cliente. O span do
// Ndovu vira raiz própria e guarda o do cliente só como link.
func TestIngestaoNaoHerdaTraceDoCliente(t *testing.T) {
	h, rec := tracedTestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/events", nil)
	req.Header.Set("traceparent", browserTraceparent)
	h.ServeHTTP(httptest.NewRecorder(), req)

	span := rec.Ended()[0]
	if span.Parent().IsValid() {
		t.Fatalf("ingestão não deveria ter pai externo")
	}
	if span.SpanContext().TraceID().String() == "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("ingestão não deveria reusar o traceId do cliente")
	}
	if l := span.Links(); len(l) != 1 || l[0].SpanContext.TraceID().String() != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("trace do cliente deveria ficar como link; links = %+v", l)
	}
}

func TestHealthNaoGeraSpan(t *testing.T) {
	h, rec := tracedTestRouter(t)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	if n := len(rec.Ended()); n != 0 {
		t.Fatalf("/health não deveria gerar span; gerou %d", n)
	}
}
