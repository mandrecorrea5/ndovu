//go:build integration

package clickhouse_test

import (
	"context"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/testenv"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

func TestSpans_GravaLeContaEApaga(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	traceID := "4bf92f3577b34da6a3ce929d0e0e4736"

	server := domain.Span{
		App: "felpma-api", TraceID: traceID, SpanID: "00f067aa0ba902b7", ParentSpanID: "b7ad6b7169203331",
		Name: "POST /contato", Kind: "server", ServiceName: "felpma-api", UserID: "u-42",
		StartTime: now.Add(-time.Second), DurationNs: int64(120 * time.Millisecond),
		StatusCode: domain.SpanStatusError, StatusMessage: "boom",
		ResourceAttrs: map[string]string{"service.name": "felpma-api"},
		Attrs:         map[string]string{"http.route": "/contato"},
		Events:        []domain.SpanEvent{{Time: now, Name: "exception", Attrs: map[string]string{"exception.type": "TypeError"}}},
		Links:         []domain.SpanLink{{TraceID: "aa", SpanID: "bb"}},
		ReceivedAt:    now,
	}
	client := domain.Span{
		App: "felpma-api", TraceID: traceID, SpanID: "1111111111111111", ParentSpanID: server.SpanID,
		Name: "SELECT contatos", Kind: "client", ServiceName: "felpma-api",
		StartTime: now.Add(-900 * time.Millisecond), DurationNs: 1000, StatusCode: domain.SpanStatusUnset,
		ReceivedAt: now,
	}
	other := client
	other.App, other.SpanID = "outra-empresa", "2222222222222222"

	batch := []domain.SpanBatch{{App: "felpma-api", Spans: []domain.Span{server, client}}, {App: "outra-empresa", Spans: []domain.Span{other}}}
	if n, err := repo.SaveSpans(ctx, batch); err != nil || n != 3 {
		t.Fatalf("SaveSpans = %d, %v", n, err)
	}
	// Reentrega do stream: o mesmo lote de novo não pode duplicar (FINAL).
	if _, err := repo.SaveSpans(ctx, batch); err != nil {
		t.Fatal(err)
	}

	got, err := repo.TraceSpans(ctx, traceID, []string{"felpma-api"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("spans no scope = %d, quero 2 (sem duplicata, sem outra-empresa)", len(got))
	}
	s := got[0]
	if s.SpanID != server.SpanID || s.ParentSpanID != server.ParentSpanID || !s.StartTime.Equal(server.StartTime) ||
		s.Attrs["http.route"] != "/contato" || s.Events[0].Attrs["exception.type"] != "TypeError" ||
		s.Links[0].SpanID != "bb" || s.DurationNs != server.DurationNs || !s.HasError() {
		t.Fatalf("span lido difere do gravado: %+v", s)
	}
	if all, _ := repo.TraceSpans(ctx, traceID, nil); len(all) != 3 {
		t.Fatalf("sem scope = %d spans, quero 3", len(all))
	}

	counts, err := repo.CountSpansByApp(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Sem FINAL de propósito (é barato e a cota tolera a sobra): a contagem
	// vê no máximo as duas cópias antes do merge.
	if counts["felpma-api"] < 2 || counts["outra-empresa"] < 1 {
		t.Fatalf("contagem = %v", counts)
	}

	if err := repo.DeleteEventsByUser(ctx, "u-42"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		left, err := repo.TraceSpans(ctx, traceID, []string{"felpma-api"})
		if err != nil {
			t.Fatal(err)
		}
		if len(left) == 1 && left[0].SpanID == client.SpanID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("esquecimento LGPD não apagou o span do usuário: %d spans", len(left))
		}
		time.Sleep(200 * time.Millisecond)
	}
}
