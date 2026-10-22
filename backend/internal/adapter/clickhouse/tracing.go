package clickhouse

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/marcoscorrea/ndovu/backend/internal/platform"
)

const dbSystem = "clickhouse"

// tracedConn decora driver.Conn com um span por operação. Fica na fronteira do
// driver para cobrir todos os métodos do Repository sem tocar em nenhum.
// Ping/Stats/Close passam direto (embutidos).
type tracedConn struct{ driver.Conn }

func (c tracedConn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	ctx, span := platform.StartDBSpan(ctx, dbSystem, query)
	rows, err := c.Conn.Query(ctx, query, args...)
	if err != nil {
		platform.EndSpan(span, err)
		return nil, err
	}
	// O span cobre a leitura das linhas: o ClickHouse faz streaming do
	// resultado, então o tempo real só termina no Close.
	return tracedRows{Rows: rows, span: span}, nil
}

func (c tracedConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	ctx, span := platform.StartDBSpan(ctx, dbSystem, query)
	row := c.Conn.QueryRow(ctx, query, args...)
	platform.EndSpan(span, row.Err())
	return row
}

func (c tracedConn) Select(ctx context.Context, dest any, query string, args ...any) error {
	ctx, span := platform.StartDBSpan(ctx, dbSystem, query)
	err := c.Conn.Select(ctx, dest, query, args...)
	platform.EndSpan(span, err)
	return err
}

func (c tracedConn) Exec(ctx context.Context, query string, args ...any) error {
	ctx, span := platform.StartDBSpan(ctx, dbSystem, query)
	err := c.Conn.Exec(ctx, query, args...)
	platform.EndSpan(span, err)
	return err
}

func (c tracedConn) AsyncInsert(ctx context.Context, query string, wait bool, args ...any) error {
	ctx, span := platform.StartDBSpan(ctx, dbSystem, query)
	err := c.Conn.AsyncInsert(ctx, query, wait, args...)
	platform.EndSpan(span, err)
	return err
}

func (c tracedConn) PrepareBatch(ctx context.Context, query string, opts ...driver.PrepareBatchOption) (driver.Batch, error) {
	ctx, span := platform.StartDBSpan(ctx, dbSystem, query)
	batch, err := c.Conn.PrepareBatch(ctx, query, opts...)
	if err != nil {
		platform.EndSpan(span, err)
		return nil, err
	}
	return &tracedBatch{Batch: batch, span: span}, nil
}

type tracedRows struct {
	driver.Rows
	span trace.Span
}

func (r tracedRows) Close() error {
	err := r.Rows.Close()
	if err == nil {
		err = r.Rows.Err()
	}
	platform.EndSpan(r.span, err)
	return err
}

// tracedBatch fecha o span no Send (o INSERT de fato) ou no Abort. O span
// cobre do PrepareBatch ao Send: inclui montar as linhas, que no writer é
// parte do custo do lote.
type tracedBatch struct {
	driver.Batch
	span  trace.Span
	ended bool
}

func (b *tracedBatch) Send() error {
	err := b.Batch.Send()
	b.end(err)
	return err
}

func (b *tracedBatch) Abort() error {
	err := b.Batch.Abort()
	b.end(err)
	return err
}

func (b *tracedBatch) end(err error) {
	if b.ended {
		return
	}
	b.ended = true
	b.span.SetAttributes(attribute.Int("ndovu.batch.rows", b.Batch.Rows()))
	platform.EndSpan(b.span, err)
}
