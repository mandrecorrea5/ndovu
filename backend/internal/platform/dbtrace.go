package platform

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// maxQueryText limita db.query.text: o schema do ClickHouse tem queries de
// vários KB e o span não precisa delas inteiras para ser útil.
const maxQueryText = 2048

var (
	sqlOperation = regexp.MustCompile(`^\s*(?:WITH\b.*?\)\s*)?([A-Za-z]+)`)
	sqlTarget    = regexp.MustCompile(`(?is)\b(?:FROM|INTO|UPDATE|TABLE(?:\s+IF\s+(?:NOT\s+)?EXISTS)?)\s+([A-Za-z_][\w.]*)`)
)

// StartDBSpan abre um span CLIENT de banco no padrão da semconv:
// nome "{operação} {tabela}" (ex.: "SELECT trace_events"), com o SQL em
// db.query.text. As queries do Ndovu são parametrizadas ($1, ?), então o texto
// não carrega valores de usuário.
func StartDBSpan(ctx context.Context, system, query string) (context.Context, trace.Span) {
	op, target := DBSpanName(query)
	name := op
	if target != "" {
		name += " " + target
	}
	if name == "" {
		name = system
	}
	text := strings.TrimSpace(query)
	if len(text) > maxQueryText {
		text = text[:maxQueryText]
	}
	attrs := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.DBSystemNameKey.String(system), semconv.DBQueryText(text)),
	}
	if op != "" {
		attrs = append(attrs, trace.WithAttributes(semconv.DBOperationName(op)))
	}
	if target != "" {
		attrs = append(attrs, trace.WithAttributes(semconv.DBCollectionName(target)))
	}
	return Tracer().Start(ctx, name, attrs...)
}

// DBSpanName extrai operação (SELECT/INSERT/…) e tabela principal do SQL.
// Heurística: primeira palavra e primeiro FROM/INTO/UPDATE/TABLE — basta para
// nomear span, não é parser.
func DBSpanName(query string) (op, target string) {
	if m := sqlOperation.FindStringSubmatch(query); m != nil {
		op = strings.ToUpper(m[1])
	}
	if m := sqlTarget.FindStringSubmatch(query); m != nil {
		target = m[1]
	}
	return op, target
}

// EndSpan fecha o span marcando erro quando houver.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
