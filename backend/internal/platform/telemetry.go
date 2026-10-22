package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// TracerName é o escopo de instrumentação dos spans criados à mão no Ndovu
// (stream, ClickHouse, Postgres). Os do HTTP vêm do otelhttp.
const TracerName = "github.com/marcoscorrea/ndovu/backend"

// Tracer devolve o tracer global do Ndovu. Sem SetupTracing (ou com tracing
// desligado) é o no-op do OTel: custo de uma chamada de interface por span.
func Tracer() trace.Tracer { return otel.Tracer(TracerName) }

// TracingEnabled diz se há destino OTLP configurado. A configuração é toda
// pelas variáveis padrão do OpenTelemetry — nada de NDOVU_* para isso, para o
// Ndovu se instrumentar exatamente como orientamos os clientes a fazer.
func TracingEnabled() bool {
	if strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true") {
		return false
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
}

// SetupTracing instala o TracerProvider global (exporter OTLP/HTTP em lote) e
// o propagador W3C (tracecontext + baggage). Devolve o shutdown que drena os
// spans pendentes — chamar no encerramento do processo.
//
// Desligado (sem endpoint), instala só o propagador: o traceparent recebido
// continua sendo repassado ao stream mesmo sem exportar nada.
//
// Variáveis lidas pelo SDK: OTEL_EXPORTER_OTLP_ENDPOINT, *_HEADERS,
// OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES, OTEL_TRACES_SAMPLER(_ARG).
func SetupTracing(ctx context.Context, service, version string, logger *slog.Logger) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	if !TracingEnabled() {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("criando exporter OTLP: %w", err)
	}
	// service.name do código é o default; OTEL_SERVICE_NAME (WithFromEnv,
	// aplicado depois) vence quando definido.
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(service), semconv.ServiceVersion(version)),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
	)
	if err != nil && !errors.Is(err, resource.ErrPartialResource) {
		return nil, fmt.Errorf("montando resource OTel: %w", err)
	}
	// O sampler sem opção explícita lê OTEL_TRACES_SAMPLER; default do SDK é
	// parentbased_always_on.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warn("opentelemetry", "err", err)
	}))
	logger.Info("tracing OTLP ativo", "service", service)

	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctx)
	}, nil
}

// traceHandler acrescenta trace_id/span_id aos logs emitidos com contexto
// (logger.InfoContext(ctx, ...)) — o log aponta para o trace no Jaeger e, na
// Fase 2 do OTLP, para o trace no próprio Ndovu.
type traceHandler struct{ slog.Handler }

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	// Só amostrado: trace descartado (ou WithoutTracing) não existe no
	// destino, e um trace_id sem trace só confunde quem lê o log.
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() && sc.IsSampled() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return h.Handler.Handle(ctx, r)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{h.Handler.WithGroup(name)}
}

// BuildVersion devolve o commit embutido pelo `go build` (vcs.revision), ou
// "dev" quando o build não tem metadados de VCS (ex.: contexto Docker sem .git).
func BuildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return s.Value[:7]
			}
		}
	}
	return "dev"
}

// InSpan roda fn dentro de um span raiz nomeado. Usado nos loops de fundo do
// writer (alertas, anomalia, digest): sem ele, cada query de um ciclo vira um
// trace órfão de um span só.
func InSpan(ctx context.Context, name string, fn func(context.Context)) {
	ctx, span := Tracer().Start(ctx, name, trace.WithNewRoot())
	defer span.End()
	fn(ctx)
}

// WithoutTracing marca o contexto como não amostrado: com o sampler
// parentbased (default), nada abaixo dele é gravado. Para tarefas periódicas
// de uma query só, que gerariam milhares de traces por dia sem informação.
func WithoutTracing(ctx context.Context) context.Context {
	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{0x01}, // válido e não amostrado: o parentbased descarta os filhos
		SpanID:  trace.SpanID{0x01},
	}))
}
