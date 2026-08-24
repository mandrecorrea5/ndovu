package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// -----------------------------------------------------------------------
// GET /v1/events
// -----------------------------------------------------------------------

func TestGetEvents_SemBearerRetorna401(t *testing.T) {
	f := newFixture(t)
	rr := f.do(f.req(http.MethodGet, "/v1/events", nil))
	requireStatus(t, rr, http.StatusUnauthorized)
}

func TestGetEvents_OKParaViewerDaCompany(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	f.seedApp("app-A", "c-default")

	rr := f.do(f.authRequest(http.MethodGet, "/v1/events", nil, uid))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Events []domain.TraceEvent }
	decodeJSON(t, rr, &body)
	// Reader é nop → array vazio, mas nunca nil.
	if body.Events == nil {
		t.Error("events NÃO deveria ser null (regressão)")
	}
}

func TestGetEvents_FromInvalidoRetorna400(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	f.seedApp("app-A", "c-default")

	rr := f.do(f.authRequest(http.MethodGet, "/v1/events?from=nao-eh-data", nil, uid))
	requireStatus(t, rr, http.StatusBadRequest)
}

// -----------------------------------------------------------------------
// GET /v1/sessions
// -----------------------------------------------------------------------

func TestGetSessions_ListagemOK(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	f.seedApp("app-A", "c-default")

	rr := f.do(f.authRequest(http.MethodGet, "/v1/sessions", nil, uid))
	requireStatus(t, rr, http.StatusOK)

	var body struct {
		Sessions []domain.Session `json:"sessions"`
	}
	decodeJSON(t, rr, &body)
	if body.Sessions == nil {
		t.Error("sessions NÃO deveria ser null")
	}
}

func TestGetSessionByID_ComFeedbackFallback(t *testing.T) {
	// ClickHouse não tem a sessão (reader retorna ErrNotFound) mas há
	// feedback com esse sessionId → deve materializar shell (regressão).
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	f.seedApp("app-A", "c-default")

	_, _ = f.feedback.CreateFeedback(context.Background(), domain.UserFeedback{
		App: "app-A", SessionID: "sess-feedback", Message: "só feedback",
		Type: domain.FeedbackBug,
	})

	rr := f.do(f.authRequest(http.MethodGet, "/v1/sessions/sess-feedback", nil, uid))
	requireStatus(t, rr, http.StatusOK)

	var body struct {
		Session  domain.Session      `json:"session"`
		Timeline []domain.TraceEvent `json:"timeline"`
	}
	decodeJSON(t, rr, &body)
	if body.Session.SessionID != "sess-feedback" {
		t.Errorf("session shell não veio: %+v", body.Session)
	}
	if body.Timeline == nil {
		t.Error("timeline deveria ser array vazio, não null")
	}
}

func TestGetSessionByID_InexistenteEmAmbos404(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	rr := f.do(f.authRequest(http.MethodGet, "/v1/sessions/nao-existe", nil, uid))
	requireStatus(t, rr, http.StatusNotFound)
}

// -----------------------------------------------------------------------
// GET /v1/stats/overview + /v1/stats/retention + /v1/meta/filters
// -----------------------------------------------------------------------

func TestGetOverview_ExigeBearer(t *testing.T) {
	f := newFixture(t)
	from := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	to := time.Now().UTC().Format(time.RFC3339)
	rr := f.do(f.req(http.MethodGet, "/v1/stats/overview?from="+from+"&to="+to, nil))
	requireStatus(t, rr, http.StatusUnauthorized)
}

func TestGetOverview_OK(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	f.seedApp("app-A", "c-default")
	from := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	to := time.Now().UTC().Format(time.RFC3339)

	rr := f.do(f.authRequest(http.MethodGet, "/v1/stats/overview?from="+from+"&to="+to, nil, uid))
	requireStatus(t, rr, http.StatusOK)
}

func TestGetFilterOptions_OK(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	f.seedApp("app-A", "c-default")

	rr := f.do(f.authRequest(http.MethodGet, "/v1/meta/filters", nil, uid))
	requireStatus(t, rr, http.StatusOK)

	var body domain.FilterOptions
	decodeJSON(t, rr, &body)
	if body.Apps == nil {
		t.Error("apps NÃO deveria ser null")
	}
}

// -----------------------------------------------------------------------
// PATCH /v1/issues/{fp} — requer editor+admin (não viewer)
// -----------------------------------------------------------------------

func TestPatchIssue_ViewerNaoPode(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	rr := f.do(f.authRequest(http.MethodPatch, "/v1/issues/some-fp",
		map[string]any{"status": "resolved"}, uid))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestPatchIssue_EditorPode(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("e@x", domain.RoleEditor, "c-default", false)
	rr := f.do(f.authRequest(http.MethodPatch, "/v1/issues/some-fp",
		map[string]any{"status": "resolved", "app": "app-A"}, uid))
	// 200 OK, mesmo sem issue real — o service faz upsert.
	requireStatus(t, rr, http.StatusOK)
}

// -----------------------------------------------------------------------
// POST /v1/issues/{fp}/comments — requer editor+admin
// -----------------------------------------------------------------------

func TestPostIssueComment_ViewerNaoPode(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	rr := f.do(f.authRequest(http.MethodPost, "/v1/issues/some-fp/comments",
		map[string]any{"body": "meu comentario"}, uid))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestPostIssueComment_EditorPode(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("e@x", domain.RoleEditor, "c-default", false)
	rr := f.do(f.authRequest(http.MethodPost, "/v1/issues/some-fp/comments",
		map[string]any{"body": "meu comentario"}, uid))
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("editor deveria conseguir comentar, veio %d: %s", rr.Code, rr.Body.String())
	}
}

// -----------------------------------------------------------------------
// Funnels write — requer editor+admin
// -----------------------------------------------------------------------

func TestPostFunnel_ViewerNaoPode(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	rr := f.do(f.authRequest(http.MethodPost, "/v1/funnels", map[string]any{
		"app": "app-A", "name": "onboarding", "windowSeconds": 1800,
		"steps": []map[string]any{
			{"name": "s1", "match": map[string]any{"type": "page_view"}},
			{"name": "s2", "match": map[string]any{"type": "action"}},
		},
	}, uid))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestPostFunnel_EditorPode(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("e@x", domain.RoleEditor, "c-default", false)
	rr := f.do(f.authRequest(http.MethodPost, "/v1/funnels", map[string]any{
		"app": "app-A", "name": "onboarding", "windowSeconds": 1800,
		"steps": []map[string]any{
			{"name": "s1", "match": map[string]any{"type": "page_view"}},
			{"name": "s2", "match": map[string]any{"type": "action"}},
		},
	}, uid))
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("editor deveria conseguir criar funnel, veio %d: %s", rr.Code, rr.Body.String())
	}
}

// -----------------------------------------------------------------------
// Saved views — viewer pode criar privada; compartilhada exige editor+admin
// -----------------------------------------------------------------------

func TestPostSavedView_ViewerCriaPrivada(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	rr := f.do(f.authRequest(http.MethodPost, "/v1/saved-views", map[string]any{
		"viewType": "traces", "name": "minha",
		"filters": map[string]any{}, "isShared": false,
	}, uid))
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("viewer deveria criar view privada, veio %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPostSavedView_ViewerNaoCompartilha(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	rr := f.do(f.authRequest(http.MethodPost, "/v1/saved-views", map[string]any{
		"viewType": "traces", "name": "shared",
		"filters": map[string]any{}, "isShared": true,
	}, uid))
	requireStatus(t, rr, http.StatusBadRequest)
}

func TestPostSavedView_EditorCompartilha(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("e@x", domain.RoleEditor, "c-default", false)
	rr := f.do(f.authRequest(http.MethodPost, "/v1/saved-views", map[string]any{
		"viewType": "traces", "name": "shared",
		"filters": map[string]any{}, "isShared": true,
	}, uid))
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("editor deveria conseguir compartilhar, veio %d: %s", rr.Code, rr.Body.String())
	}
}
