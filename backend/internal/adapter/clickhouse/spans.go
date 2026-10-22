package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// DefaultSpanRetentionDays é a retenção de otel_spans quando a env não diz
// nada — decisão 3 do docs/AVALIACAO-OTLP.md.
const DefaultSpanRetentionDays = 30

// hotDays é quanto o span fica no volume quente antes de ir para o warm,
// igual a trace_events. Retenção menor que isso dispensa o move.
const hotDays = 7

// spanTTL monta o MODIFY TTL de otel_spans. Sem volume cold: com 30 dias o
// span sai antes de onde trace_events passa a ir para o S3.
func spanTTL(days int) string {
	if days <= 0 {
		days = DefaultSpanRetentionDays
	}
	if days <= hotDays {
		return fmt.Sprintf("ALTER TABLE otel_spans MODIFY TTL toDateTime(start_time) + INTERVAL %d DAY DELETE", days)
	}
	return fmt.Sprintf(`ALTER TABLE otel_spans MODIFY TTL
		toDateTime(start_time) + INTERVAL %d DAY TO VOLUME 'warm',
		toDateTime(start_time) + INTERVAL %d DAY DELETE`, hotDays, days)
}

// SaveSpans insere todos os spans dos lotes num único INSERT. Reentrega do
// stream é segura: ReplacingMergeTree deduplica pela chave de ordenação.
func (r *Repository) SaveSpans(ctx context.Context, batches []domain.SpanBatch) (int, error) {
	batch, err := r.conn.PrepareBatch(ctx, `INSERT INTO otel_spans
		(app, trace_id, span_id, parent_span_id, trace_state, name, kind,
		 service_name, scope_name, user_id, start_time, duration_ns,
		 status_code, status_message, resource_attrs, span_attrs, events,
		 links, received_at)`)
	if err != nil {
		return 0, fmt.Errorf("preparando insert de spans: %w", err)
	}
	total := 0
	for _, b := range batches {
		for _, s := range b.Spans {
			events, err := marshalOptional(s.Events)
			if err != nil {
				_ = batch.Abort()
				return 0, fmt.Errorf("serializando eventos do span: %w", err)
			}
			links, err := marshalOptional(s.Links)
			if err != nil {
				_ = batch.Abort()
				return 0, fmt.Errorf("serializando links do span: %w", err)
			}
			if err := batch.Append(
				s.App, s.TraceID, s.SpanID, s.ParentSpanID, s.TraceState, s.Name, s.Kind,
				s.ServiceName, s.ScopeName, s.UserID, s.StartTime, s.DurationNs,
				s.StatusCode, s.StatusMessage, nonNilMap(s.ResourceAttrs), nonNilMap(s.Attrs), events,
				links, s.ReceivedAt,
			); err != nil {
				_ = batch.Abort()
				return 0, fmt.Errorf("anexando span: %w", err)
			}
			total++
		}
	}
	if total == 0 {
		_ = batch.Abort()
		return 0, nil
	}
	if err := batch.Send(); err != nil {
		return 0, fmt.Errorf("enviando insert de spans: %w", err)
	}
	return total, nil
}

// TraceSpans devolve os spans de um trace em ordem de início. scope vazio =
// sem restrição de app (super-admin).
func (r *Repository) TraceSpans(ctx context.Context, traceID string, scope []string) ([]domain.Span, error) {
	if traceID == "" {
		return nil, nil
	}
	query := `SELECT app, trace_id, span_id, parent_span_id, trace_state, name, kind,
		service_name, scope_name, user_id, start_time, duration_ns, status_code,
		status_message, resource_attrs, span_attrs, events, links, received_at
		FROM otel_spans FINAL WHERE trace_id = $1`
	args := []any{traceID}
	if len(scope) > 0 {
		query += " AND app IN $2"
		args = append(args, scope)
	}
	// Teto defensivo: um trace patológico (loop instrumentado) não pode
	// derrubar a API nem o navegador de quem abre o waterfall.
	query += " ORDER BY start_time ASC LIMIT 5000"

	rows, err := r.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("consultando spans do trace: %w", err)
	}
	defer rows.Close()

	spans := []domain.Span{}
	for rows.Next() {
		var s domain.Span
		var events, links string
		if err := rows.Scan(
			&s.App, &s.TraceID, &s.SpanID, &s.ParentSpanID, &s.TraceState, &s.Name, &s.Kind,
			&s.ServiceName, &s.ScopeName, &s.UserID, &s.StartTime, &s.DurationNs, &s.StatusCode,
			&s.StatusMessage, &s.ResourceAttrs, &s.Attrs, &events, &links, &s.ReceivedAt,
		); err != nil {
			return nil, fmt.Errorf("lendo span: %w", err)
		}
		if events != "" {
			if err := json.Unmarshal([]byte(events), &s.Events); err != nil {
				return nil, fmt.Errorf("decodificando eventos do span: %w", err)
			}
		}
		if links != "" {
			if err := json.Unmarshal([]byte(links), &s.Links); err != nil {
				return nil, fmt.Errorf("decodificando links do span: %w", err)
			}
		}
		spans = append(spans, s)
	}
	return spans, rows.Err()
}

// CountSpansByApp conta spans por app com received_at >= since. Conta pelo
// recebimento, não pelo start_time: a cota é sobre o que entrou hoje.
func (r *Repository) CountSpansByApp(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := r.conn.Query(ctx,
		"SELECT app, count() FROM otel_spans WHERE received_at >= $1 GROUP BY app", since)
	if err != nil {
		return nil, fmt.Errorf("contando spans por app: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var app string
		var n uint64
		if err := rows.Scan(&app, &n); err != nil {
			return nil, fmt.Errorf("lendo contagem de spans: %w", err)
		}
		out[app] = int(n) //nolint:gosec // contagem diária cabe em int
	}
	return out, rows.Err()
}

func marshalOptional[T any](items []T) (string, error) {
	if len(items) == 0 {
		return "", nil
	}
	b, err := json.Marshal(items)
	return string(b), err
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
