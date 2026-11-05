package clickhouse

// Persistência e busca de logs OTLP.

import (
	"context"
	"fmt"
	"strings"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// DefaultLogRetentionDays é o TTL de otel_logs quando a env não diz nada.
const DefaultLogRetentionDays = 30

func logTTL(days int) string {
	if days <= 0 {
		days = DefaultLogRetentionDays
	}
	if days <= hotDays {
		return fmt.Sprintf("ALTER TABLE otel_logs MODIFY TTL toDateTime(log_time) + INTERVAL %d DAY DELETE", days)
	}
	return fmt.Sprintf(`ALTER TABLE otel_logs MODIFY TTL
		toDateTime(log_time) + INTERVAL %d DAY TO VOLUME 'warm',
		toDateTime(log_time) + INTERVAL %d DAY DELETE`, hotDays, days)
}

// SaveLogs insere todos os lotes num único INSERT. Reentrega do stream é
// segura: ReplacingMergeTree deduplica pela chave de ordenação.
func (r *Repository) SaveLogs(ctx context.Context, batches []domain.LogBatch) (int, error) {
	batch, err := r.conn.PrepareBatch(ctx, `INSERT INTO otel_logs
		(app, service_name, scope_name, severity_text, severity_number, body,
		 trace_id, span_id, user_id, log_time, observed_time, attributes,
		 resource_attrs, received_at)`)
	if err != nil {
		return 0, fmt.Errorf("preparando insert de logs: %w", err)
	}
	total := 0
	for _, b := range batches {
		for _, l := range b.Logs {
			if err := batch.Append(
				l.App, l.ServiceName, l.ScopeName, l.SeverityText, l.SeverityNumber, l.Body,
				l.TraceID, l.SpanID, l.UserID, l.Time, l.ObservedTime, nonNilMap(l.Attrs),
				nonNilMap(l.ResourceAttrs), l.ReceivedAt,
			); err != nil {
				_ = batch.Abort()
				return 0, fmt.Errorf("anexando log: %w", err)
			}
			total++
		}
	}
	if total == 0 {
		_ = batch.Abort()
		return 0, nil
	}
	if err := batch.Send(); err != nil {
		return 0, fmt.Errorf("enviando insert de logs: %w", err)
	}
	return total, nil
}

// FindLogs busca logs com os filtros do dashboard. scope vazio = super-admin.
func (r *Repository) FindLogs(ctx context.Context, f domain.LogFilter) ([]domain.LogRecord, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	q := newQueryBuilder(`SELECT app, service_name, scope_name, severity_text,
		severity_number, body, trace_id, span_id, user_id, log_time, observed_time,
		attributes, resource_attrs, received_at FROM otel_logs FINAL`)
	q.whereTime("log_time", f.From, f.To)
	q.whereEq("app", f.App)
	q.whereEq("service_name", f.Service)
	if f.Severity != "" {
		q.whereEq("severity_text", strings.ToLower(f.Severity))
	}
	q.whereInStr("app", f.Scope)
	if f.Search != "" {
		p := q.arg(f.Search)
		q.where(fmt.Sprintf(`(positionCaseInsensitive(body, %[1]s) > 0
			OR positionCaseInsensitive(service_name, %[1]s) > 0
			OR positionCaseInsensitive(trace_id, %[1]s) > 0)`, p))
	}
	q.orderBy("log_time DESC")
	q.limit(f.Limit)

	rows, err := r.conn.Query(ctx, q.sql(), q.args...)
	if err != nil {
		return nil, fmt.Errorf("consultando logs: %w", err)
	}
	defer rows.Close()

	logs := []domain.LogRecord{}
	for rows.Next() {
		var l domain.LogRecord
		if err := rows.Scan(
			&l.App, &l.ServiceName, &l.ScopeName, &l.SeverityText, &l.SeverityNumber,
			&l.Body, &l.TraceID, &l.SpanID, &l.UserID, &l.Time, &l.ObservedTime,
			&l.Attrs, &l.ResourceAttrs, &l.ReceivedAt,
		); err != nil {
			return nil, fmt.Errorf("lendo log: %w", err)
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}
