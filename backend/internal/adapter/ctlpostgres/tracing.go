package ctlpostgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"

	"github.com/marcoscorrea/ndovu/backend/internal/platform"
)

// queryTracer implementa pgx.QueryTracer: um span por query no control plane.
// É o caminho quente da ingestão (validação de chave em cache miss), então
// aparece no trace de POST /v1/events quando o cache expira.
type queryTracer struct{}

type spanKey struct{}

func (queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx, span := platform.StartDBSpan(ctx, "postgresql", data.SQL)
	return context.WithValue(ctx, spanKey{}, span)
}

func (queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if span, ok := ctx.Value(spanKey{}).(trace.Span); ok {
		err := data.Err
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil // "não achou" é resposta, não falha
		}
		platform.EndSpan(span, err)
	}
}
