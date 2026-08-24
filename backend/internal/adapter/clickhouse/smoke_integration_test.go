//go:build integration

package clickhouse_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/testenv"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// Helpers ---------------------------------------------------------------

// mustUUID gera um UUID válido pra usar como eventId nos tests.
func mustUUID(t *testing.T) string {
	t.Helper()
	return uuid.New().String()
}

func mustSessionID(t *testing.T) string {
	t.Helper()
	return uuid.New().String()
}

// -- Smoke: schema aplicado + insert + read básico ----------------------

func TestSmoke_ClickHouseInsertRead(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()

	// Insert simples via SaveBatches.
	now := time.Now().UTC()
	batch := domain.IngestBatch{
		Session: domain.Session{
			SessionID: mustSessionID(t), UserID: "u-1", App: "portal",
		},
		Events: []domain.TraceEvent{
			{
				ID: mustUUID(t), SessionID: "s-1", UserID: "u-1", App: "portal",
				Type: domain.EventAction, Name: "clicou",
				OccurredAt: now,
			},
		},
	}
	res, err := repo.SaveBatches(ctx, []domain.IngestBatch{batch})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if res.Accepted != 1 {
		t.Errorf("esperava 1 accepted, veio %d", res.Accepted)
	}

	// Read via FindEvents.
	page, err := repo.FindEvents(ctx, domain.EventFilter{Limit: 10})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(page.Events) != 1 {
		t.Errorf("esperava 1 evento, veio %d", len(page.Events))
	}
	if page.Events[0].Name != "clicou" || page.Events[0].App != "portal" {
		t.Errorf("evento errado: %+v", page.Events[0])
	}
}

// -- Dedup por eventId --------------------------------------------------

func TestClickHouse_DedupPorEventID(t *testing.T) {
	// ReplacingMergeTree dedup por id. Enviar 3x o mesmo evento deve
	// resultar em 1 linha após FINAL. Bug histórico: aliases colidindo
	// no FindSessions (any(app) AS app) — a regressão fica coberta aqui.
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()
	eid := mustUUID(t)
	sid := mustSessionID(t)
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		batch := domain.IngestBatch{
			Session: domain.Session{SessionID: sid, App: "portal"},
			Events: []domain.TraceEvent{{
				ID: eid, SessionID: sid, App: "portal",
				Type: domain.EventAction, Name: "unique",
				OccurredAt: now,
			}},
		}
		if _, err := repo.SaveBatches(ctx, []domain.IngestBatch{batch}); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	page, err := repo.FindEvents(ctx, domain.EventFilter{Limit: 100})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	// Depois de FINAL (usado por FindEvents), só 1 linha.
	count := 0
	for _, e := range page.Events {
		if e.ID == eid {
			count++
		}
	}
	if count != 1 {
		t.Errorf("dedup falhou: esperava 1 evento com id %s, veio %d", eid, count)
	}
}

// -- FindSessions: regressão do alias any(app) AS app -------------------

func TestClickHouse_FindSessionsComAppScope(t *testing.T) {
	// Bug corrigido no sprint anterior: any(app) AS app colidia com
	// WHERE app IN (...) do tenantScope quando actor não é super.
	// Aqui simulamos: passa AppScope + confirma que não dá 500.
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sid1 := mustSessionID(t)
	sid2 := mustSessionID(t)
	_, _ = repo.SaveBatches(ctx, []domain.IngestBatch{
		{
			Session: domain.Session{SessionID: sid1, App: "app-A"},
			Events: []domain.TraceEvent{{
				ID: mustUUID(t), SessionID: sid1, App: "app-A",
				Type: domain.EventAction, Name: "x", OccurredAt: now,
			}},
		},
		{
			Session: domain.Session{SessionID: sid2, App: "app-B"},
			Events: []domain.TraceEvent{{
				ID: mustUUID(t), SessionID: sid2, App: "app-B",
				Type: domain.EventAction, Name: "y", OccurredAt: now,
			}},
		},
	})

	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)
	page, err := repo.FindSessions(ctx, domain.SessionFilter{
		AppScope: []string{"app-A"},
		From:     &from, To: &to,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("find sessions com scope: %v", err)
	}
	if len(page.Sessions) != 1 || page.Sessions[0].App != "app-A" {
		t.Errorf("scope não filtrou: %+v", page.Sessions)
	}
}

// -- GetSession + SessionTimeline --------------------------------------

func TestClickHouse_GetSessionESessionTimeline(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()
	sid := mustSessionID(t)
	now := time.Now().UTC()

	_, err := repo.SaveBatches(ctx, []domain.IngestBatch{{
		Session: domain.Session{SessionID: sid, App: "portal", UserID: "u"},
		Events: []domain.TraceEvent{
			{ID: mustUUID(t), SessionID: sid, App: "portal", Type: domain.EventPageView, Name: "home", OccurredAt: now},
			{ID: mustUUID(t), SessionID: sid, App: "portal", Type: domain.EventAction, Name: "click", OccurredAt: now.Add(1 * time.Second)},
			{ID: mustUUID(t), SessionID: sid, App: "portal", Type: domain.EventError, Name: "erro", Error: &domain.ErrorInfo{Code: "X", Message: "boom"}, OccurredAt: now.Add(2 * time.Second)},
		},
	}})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	sess, err := repo.GetSession(ctx, sid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if sess.EventCount != 3 || sess.ErrorCount != 1 {
		t.Errorf("session errada: %+v", sess)
	}

	timeline, err := repo.SessionTimeline(ctx, sid)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(timeline) != 3 {
		t.Errorf("timeline esperava 3, veio %d", len(timeline))
	}
	// Ordem cronológica ASC.
	if !timeline[0].OccurredAt.Before(timeline[2].OccurredAt) {
		t.Error("timeline não está em ordem cronológica")
	}
}

func TestClickHouse_GetSessionInexistenteRetornaNotFound(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	_, err := repo.GetSession(context.Background(), "nao-existe")
	if err != domain.ErrNotFound {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

// -- GetEvent -----------------------------------------------------------

func TestClickHouse_GetEventById(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()
	eid := mustUUID(t)
	sid := mustSessionID(t)
	_, _ = repo.SaveBatches(ctx, []domain.IngestBatch{{
		Session: domain.Session{SessionID: sid, App: "portal"},
		Events: []domain.TraceEvent{{
			ID: eid, SessionID: sid, App: "portal",
			Type: domain.EventAction, Name: "click",
			Metadata: json.RawMessage(`{"key":"val"}`),
			OccurredAt: time.Now().UTC(),
		}},
	}})

	got, err := repo.GetEvent(ctx, eid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != eid || got.Name != "click" {
		t.Errorf("evento errado: %+v", got)
	}

	// Inexistente.
	_, err = repo.GetEvent(ctx, mustUUID(t))
	if err != domain.ErrNotFound {
		t.Errorf("esperava NotFound, veio %v", err)
	}
}

// -- Retention: cohorts vazios retornam [] (regressão) -----------------

func TestClickHouse_RetentionVazioRetornaArrayNaoNil(t *testing.T) {
	// Bug corrigido: reader devolvia nil (que serializa como null no JSON).
	repo := testenv.StartClickHouse(t)
	from := time.Now().Add(-30 * 24 * time.Hour).UTC()
	to := time.Now().UTC()

	res, err := repo.Retention(context.Background(), domain.RetentionFilter{
		From: &from, To: &to, CohortBy: "week",
	})
	if err != nil {
		t.Fatalf("retention: %v", err)
	}
	if res.Cohorts == nil {
		t.Error("cohorts NÃO deveria ser nil quando vazio (regressão)")
	}
}

// -- FindEventsByUser + DeleteEventsByUser (LGPD) ----------------------

func TestClickHouse_GDPRExportEForget(t *testing.T) {
	repo := testenv.StartClickHouse(t)
	ctx := context.Background()
	sid := mustSessionID(t)
	uid := "user-forget-me"
	now := time.Now().UTC()

	_, _ = repo.SaveBatches(ctx, []domain.IngestBatch{{
		Session: domain.Session{SessionID: sid, App: "portal", UserID: uid},
		Events: []domain.TraceEvent{
			{ID: mustUUID(t), SessionID: sid, UserID: uid, App: "portal", Type: domain.EventPageView, Name: "home", OccurredAt: now},
			{ID: mustUUID(t), SessionID: sid, UserID: uid, App: "portal", Type: domain.EventAction, Name: "click", OccurredAt: now.Add(1 * time.Second)},
		},
	}})

	events, err := repo.FindEventsByUser(ctx, uid)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("esperava 2 eventos exportados, veio %d", len(events))
	}

	// Forget é async no ClickHouse — só validamos que não dá erro.
	if err := repo.DeleteEventsByUser(ctx, uid); err != nil {
		t.Errorf("forget: %v", err)
	}

	// UserID vazio deve rejeitar.
	if err := repo.DeleteEventsByUser(ctx, ""); err == nil {
		t.Error("userID vazio deveria rejeitar")
	}
}
