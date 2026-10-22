package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// ingestPaths são as rotas públicas de ingestão: quem chama é o browser de um
// cliente, então um traceparent ali não é nosso. Elas abrem trace raiz próprio
// (com link para o do cliente) em vez de pendurar spans do Ndovu no trace de
// terceiros.
var ingestPaths = map[string]bool{
	"/v1/events":    true,
	"/v1/snapshots": true,
	"/v1/feedbacks": true,
}

// withTracing envolve o router com o otelhttp: um span SERVER por request,
// contexto W3C extraído do traceparent. /health e /metrics ficam de fora —
// são sondados a cada poucos segundos e só gerariam ruído.
func withTracing(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http.server",
		otelhttp.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != "/health" && r.URL.Path != "/metrics"
		}),
		otelhttp.WithPublicEndpointFn(func(r *http.Request) bool {
			return ingestPaths[strings.TrimSuffix(r.URL.Path, "/")]
		}),
	)
}

// traceRoute renomeia o span do request com o padrão da rota do chi
// ("GET /v1/sessions/{sessionId}") depois do roteamento — o otelhttp só
// conhece o path cru, que explodiria a cardinalidade de nomes de span.
func traceRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		rctx := chi.RouteContext(r.Context())
		if rctx == nil {
			return
		}
		if pattern := rctx.RoutePattern(); pattern != "" {
			span := trace.SpanFromContext(r.Context())
			span.SetName(r.Method + " " + pattern)
			span.SetAttributes(semconv.HTTPRoute(pattern))
		}
	})
}
