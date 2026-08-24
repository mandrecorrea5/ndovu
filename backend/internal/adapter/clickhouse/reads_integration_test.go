//go:build integration

package clickhouse_test

import (
	"context"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/testenv"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// seedEvents insere N eventos com pattern padrão pra popular os testes
// de agregação.
func seedEvents(t *testing.T, repo interface {
	SaveBatches(context.Context, []domain.IngestBatch) (domain.IngestResult, error)
}, events ...domain.TraceEvent) {
	t.Helper()
	batch := domain.IngestBatch{
		Session: domain.Session{SessionID: events[0].SessionID, App: events[0].App},
		Events:  events,
	}
	if _, err := repo.SaveBatches(context.Background(), []domain.IngestBatch{batch}); err != nil {
		t.Fatalf("save: %v", err)
	}
}

// -- GetOverview -------------------------------------------------------

func TestClickHouse_GetOverview(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()
	now := time.Now().UTC()
	sid := mustSessionID(t)

	// 3 eventos: 2 http_request (uma com erro 500) + 1 action.
	statusOK := 200
	status500 := 500
	dur100 := int32(100)
	dur200 := int32(200)

	seedEvents(t, repo,
		domain.TraceEvent{
			ID: mustUUID(t), SessionID: sid, App: "portal", UserID: "u-1",
			Type: domain.EventHTTPRequest, Name: "GET /faturas",
			HTTP:       &domain.HTTPInfo{Method: "GET", URL: "/faturas", StatusCode: &statusOK},
			DurationMs: (*int)(nil), OccurredAt: now,
		},
		domain.TraceEvent{
			ID: mustUUID(t), SessionID: sid, App: "portal", UserID: "u-1",
			Type: domain.EventHTTPRequest, Name: "POST /pagamento",
			HTTP:       &domain.HTTPInfo{Method: "POST", URL: "/pagamento", StatusCode: &status500},
			Error:      &domain.ErrorInfo{Code: "HTTP_500", Message: "server error"},
			DurationMs: nil,
			OccurredAt: now.Add(1 * time.Second),
		},
		domain.TraceEvent{
			ID: mustUUID(t), SessionID: sid, App: "portal", UserID: "u-1",
			Type: domain.EventAction, Name: "click",
			OccurredAt: now.Add(2 * time.Second),
		},
	)
	_ = dur100
	_ = dur200

	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)
	ov, err := repo.GetOverview(ctx, from, to, "portal")
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if ov.TotalEvents != 3 {
		t.Errorf("total events esperado 3, veio %d", ov.TotalEvents)
	}
	if ov.TotalErrors < 1 {
		t.Errorf("total errors deveria ser >=1, veio %d", ov.TotalErrors)
	}
	if ov.TotalSessions != 1 {
		t.Errorf("sessions esperado 1, veio %d", ov.TotalSessions)
	}
}

// -- FindIssues (agrupamento por fingerprint) --------------------------

func TestClickHouse_FindIssues(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 2 erros do mesmo tipo em sessões diferentes → 1 issue.
	sid1, sid2 := mustSessionID(t), mustSessionID(t)
	seedEvents(t, repo,
		domain.TraceEvent{
			ID: mustUUID(t), SessionID: sid1, App: "portal", UserID: "u-1",
			Type: domain.EventError, Name: "boom",
			Error: &domain.ErrorInfo{Code: "AUTH_401", Message: "unauthorized"},
			OccurredAt: now,
		},
	)
	seedEvents(t, repo,
		domain.TraceEvent{
			ID: mustUUID(t), SessionID: sid2, App: "portal", UserID: "u-2",
			Type: domain.EventError, Name: "boom",
			Error: &domain.ErrorInfo{Code: "AUTH_401", Message: "unauthorized"},
			OccurredAt: now.Add(1 * time.Second),
		},
	)

	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)
	issues, err := repo.FindIssues(ctx, domain.IssueFilter{
		From: &from, To: &to, App: "portal", Limit: 10,
	})
	if err != nil {
		t.Fatalf("find issues: %v", err)
	}
	if len(issues) < 1 {
		t.Fatalf("esperava ≥1 issue, veio %d", len(issues))
	}
	// A primeira issue deve ter 2 ocorrências.
	if issues[0].Count < 2 {
		t.Errorf("issue deveria ter count>=2, veio %d", issues[0].Count)
	}
}

// -- FindReleases ------------------------------------------------------

func TestClickHouse_FindReleases(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()
	now := time.Now().UTC()
	sid := mustSessionID(t)

	// 2 releases distintas.
	seedEvents(t, repo,
		domain.TraceEvent{
			ID: mustUUID(t), SessionID: sid, App: "portal", Release: "1.0.0",
			Type: domain.EventAction, Name: "x", OccurredAt: now,
		},
	)
	seedEvents(t, repo,
		domain.TraceEvent{
			ID: mustUUID(t), SessionID: mustSessionID(t), App: "portal", Release: "1.0.1",
			Type: domain.EventAction, Name: "y", OccurredAt: now.Add(1 * time.Second),
		},
	)

	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)
	rels, err := repo.FindReleases(ctx, domain.ReleaseFilter{
		From: &from, To: &to, App: "portal",
	})
	if err != nil {
		t.Fatalf("find releases: %v", err)
	}
	if len(rels) < 2 {
		t.Errorf("esperava ≥2 releases, veio %d", len(rels))
	}
}

// -- TraceTimeline (W3C traceparent) -----------------------------------

func TestClickHouse_TraceTimeline(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()
	now := time.Now().UTC()
	traceID := "abc123def456"

	sid := mustSessionID(t)
	seedEvents(t, repo,
		domain.TraceEvent{
			ID: mustUUID(t), SessionID: sid, App: "portal",
			Type: domain.EventHTTPRequest, Name: "GET /x",
			Trace:      &domain.TraceContext{TraceID: traceID, SpanID: "s1"},
			OccurredAt: now,
		},
		domain.TraceEvent{
			ID: mustUUID(t), SessionID: sid, App: "portal",
			Type: domain.EventHTTPRequest, Name: "GET /y",
			Trace:      &domain.TraceContext{TraceID: traceID, SpanID: "s2"},
			OccurredAt: now.Add(1 * time.Second),
		},
		// Outro trace_id — não deve aparecer.
		domain.TraceEvent{
			ID: mustUUID(t), SessionID: sid, App: "portal",
			Type: domain.EventHTTPRequest, Name: "outro",
			Trace:      &domain.TraceContext{TraceID: "outro-trace"},
			OccurredAt: now.Add(2 * time.Second),
		},
	)

	events, err := repo.TraceTimeline(ctx, traceID)
	if err != nil {
		t.Fatalf("trace timeline: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("esperava 2 eventos do traceID, veio %d", len(events))
	}
}

// -- GetFilterOptions --------------------------------------------------

func TestClickHouse_GetFilterOptions(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()

	seedEvents(t, repo,
		domain.TraceEvent{
			ID: mustUUID(t), SessionID: mustSessionID(t), App: "app-alfa",
			Type: domain.EventAction, Name: "click", Feature: "checkout",
			OccurredAt: time.Now().UTC(),
		},
	)
	seedEvents(t, repo,
		domain.TraceEvent{
			ID: mustUUID(t), SessionID: mustSessionID(t), App: "app-beta",
			Type: domain.EventPageView, Name: "home", Feature: "landing",
			OccurredAt: time.Now().UTC(),
		},
	)

	opts, err := repo.GetFilterOptions(ctx)
	if err != nil {
		t.Fatalf("filter options: %v", err)
	}
	if len(opts.Apps) < 2 {
		t.Errorf("apps: esperava ≥2, veio %v", opts.Apps)
	}
	if len(opts.Types) == 0 || len(opts.Features) == 0 || len(opts.Names) == 0 {
		t.Errorf("options incompletos: %+v", opts)
	}
}

// -- MetricInWindow (usado pela detecção de anomalia) -----------------

func TestClickHouse_MetricInWindow(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 5 eventos, 2 erros.
	sid := mustSessionID(t)
	for i := 0; i < 5; i++ {
		e := domain.TraceEvent{
			ID: mustUUID(t), SessionID: sid, App: "portal",
			Type: domain.EventAction, Name: "x",
			OccurredAt: now.Add(time.Duration(i) * time.Second),
		}
		if i < 2 {
			e.Type = domain.EventError
			e.Error = &domain.ErrorInfo{Code: "X", Message: "err"}
		}
		seedEvents(t, repo, e)
	}

	from := now.Add(-time.Minute)
	to := now.Add(time.Minute)

	// error_count
	n, err := repo.MetricInWindow(ctx, "error_count", "portal", from, to)
	if err != nil {
		t.Fatalf("metric: %v", err)
	}
	if n != 2 {
		t.Errorf("error_count esperado 2, veio %v", n)
	}

	// event_count
	n, err = repo.MetricInWindow(ctx, "event_count", "portal", from, to)
	if err != nil {
		t.Fatalf("event_count: %v", err)
	}
	if n != 5 {
		t.Errorf("event_count esperado 5, veio %v", n)
	}

	// error_rate (2 / 5 = 0.4)
	n, err = repo.MetricInWindow(ctx, "error_rate", "portal", from, to)
	if err != nil {
		t.Fatalf("error_rate: %v", err)
	}
	if n < 0.35 || n > 0.45 {
		t.Errorf("error_rate esperado ~0.4, veio %v", n)
	}
}

// -- CountErrorsSince (usado por alertas) -----------------------------

func TestClickHouse_CountErrorsSince(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sid := mustSessionID(t)
	for i := 0; i < 3; i++ {
		seedEvents(t, repo, domain.TraceEvent{
			ID: mustUUID(t), SessionID: sid, App: "portal",
			Type: domain.EventError, Name: "boom",
			Error:      &domain.ErrorInfo{Code: "AUTH_401", Message: "x"},
			OccurredAt: now.Add(time.Duration(i) * time.Second),
		})
	}

	// Filtro por código específico.
	n, err := repo.CountErrorsSince(ctx, "portal", "AUTH_401", now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Errorf("esperava 3 erros AUTH_401, veio %d", n)
	}

	// Código que não bateu.
	n, _ = repo.CountErrorsSince(ctx, "portal", "OUTRO_ERRO", now.Add(-time.Minute))
	if n != 0 {
		t.Errorf("erros com código inexistente: esperava 0, veio %d", n)
	}
}
