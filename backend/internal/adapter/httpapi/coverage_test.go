package httpapi

// Testes complementares para elevar cobertura de handlers.go: cada
// handler tem 1-2 asserts básicos (autenticação + status esperado).
// Não são "testes de comportamento profundo" — mas garantem que a
// rota existe, o middleware casa e o handler não panica.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

func TestHealth_OK(t *testing.T) {
	f := newFixture(t)
	rr := f.do(f.req(http.MethodGet, "/health", nil))
	requireStatus(t, rr, http.StatusOK)
}

// -- GETs read-only exigindo bearer ---------------------------------

func TestReadOnlyEndpoints_ExigemBearer(t *testing.T) {
	f := newFixture(t)
	from := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	to := time.Now().UTC().Format(time.RFC3339)

	paths := []string{
		"/v1/events/some-id",
		"/v1/events/some-id/resolved-stack",
		"/v1/traces/some-trace",
		"/v1/snapshots/event/some-id",
		"/v1/snapshots/event/some-id/html",
		"/v1/stats/vitals?from=" + from + "&to=" + to,
		"/v1/stats/compare?app=x&releaseA=1&releaseB=2",
		"/v1/stats/retention?from=" + from + "&to=" + to,
		"/v1/issues?from=" + from + "&to=" + to,
		"/v1/issues/fp/comments",
		"/v1/releases",
		"/v1/funnels",
		"/v1/funnels/some-id/results",
		"/v1/saved-views",
	}
	for _, p := range paths {
		rr := f.do(f.req(http.MethodGet, p, nil))
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s: esperava 401, veio %d", p, rr.Code)
		}
	}
}

func TestReadOnlyEndpoints_HappyPathAuth(t *testing.T) {
	// Cobertura larga: cada endpoint retorna 200/404 (mas nunca panic).
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	f.seedApp("app-A", "c-default")
	from := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	to := time.Now().UTC().Format(time.RFC3339)

	cases := []struct {
		path     string
		wantCode int
	}{
		{"/v1/events/some-id", http.StatusNotFound}, // reader devolve NotFound
		{"/v1/traces/some-trace", http.StatusOK},
		{"/v1/snapshots/event/some-id", http.StatusNotFound},
		{"/v1/stats/vitals?from=" + from + "&to=" + to + "&app=app-A", http.StatusOK},
		{"/v1/stats/compare?app=app-A&releaseA=1&releaseB=2", http.StatusOK},
		{"/v1/stats/retention?from=" + from + "&to=" + to, http.StatusOK},
		{"/v1/issues?from=" + from + "&to=" + to, http.StatusOK},
		{"/v1/issues/fp/comments", http.StatusOK},
		{"/v1/releases?app=app-A", http.StatusOK},
		{"/v1/funnels", http.StatusOK},
		{"/v1/saved-views", http.StatusOK},
	}
	for _, c := range cases {
		rr := f.do(f.authRequest(http.MethodGet, c.path, nil, uid))
		if rr.Code != c.wantCode {
			t.Errorf("%s: esperava %d, veio %d: %s",
				c.path, c.wantCode, rr.Code, rr.Body.String())
		}
	}
}

// -- Saved views CRUD do próprio dono -------------------------------

func TestSavedView_UpdateEDeleteDoDono(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("v@x", domain.RoleViewer, "c-default", false)

	// Cria via API pra id vir da fixture.
	rr := f.do(f.authRequest(http.MethodPost, "/v1/saved-views", map[string]any{
		"viewType": "traces", "name": "minha",
		"filters": map[string]any{}, "isShared": false,
	}, uid))
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var created domain.SavedView
	decodeJSON(t, rr, &created)

	// Update.
	rr = f.do(f.authRequest(http.MethodPatch, "/v1/saved-views/"+created.ID, map[string]any{
		"name": "renomeada", "filters": map[string]any{}, "isShared": false,
	}, uid))
	requireStatus(t, rr, http.StatusOK)

	// Delete.
	rr = f.do(f.authRequest(http.MethodDelete, "/v1/saved-views/"+created.ID, nil, uid))
	requireStatus(t, rr, http.StatusOK)
}

// -- Funnels CRUD por editor ----------------------------------------

func TestFunnels_UpdateEDeleteEditor(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("e@x", domain.RoleEditor, "c-default", false)

	rr := f.do(f.authRequest(http.MethodPost, "/v1/funnels", map[string]any{
		"app": "app-A", "name": "f1", "windowSeconds": 900,
		"steps": []map[string]any{
			{"name": "s1", "match": map[string]any{"type": "page_view"}},
			{"name": "s2", "match": map[string]any{"type": "action"}},
		},
	}, uid))
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var created domain.Funnel
	decodeJSON(t, rr, &created)

	rr = f.do(f.authRequest(http.MethodPatch, "/v1/funnels/"+created.ID, map[string]any{
		"name": "f-novo", "windowSeconds": 1800,
		"steps": []map[string]any{
			{"name": "s1", "match": map[string]any{"type": "action"}},
			{"name": "s2", "match": map[string]any{"type": "custom"}},
		},
	}, uid))
	requireStatus(t, rr, http.StatusOK)

	rr = f.do(f.authRequest(http.MethodGet, "/v1/funnels/"+created.ID+"/results?from=2026-01-01T00:00:00Z&to=2026-12-31T00:00:00Z", nil, uid))
	requireStatus(t, rr, http.StatusOK)

	rr = f.do(f.authRequest(http.MethodDelete, "/v1/funnels/"+created.ID, nil, uid))
	requireStatus(t, rr, http.StatusOK)
}

// -- Anomaly + sampling CRUD do admin -------------------------------

func TestAnomaly_CRUDAdmin(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	f.seedApp("app-A", "c-default")

	rr := f.do(f.authRequest(http.MethodPost, "/v1/admin/anomaly-rules", map[string]any{
		"name": "rA", "app": "app-A", "metric": "error_count",
		"windowMinutes": 15, "baselineWeeks": 4, "sensitivity": 3,
		"direction": "above", "silenceSeconds": 60,
		"channel": "webhook", "targetUrl": "http://x", "active": true,
	}, uid))
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var created domain.AnomalyRule
	decodeJSON(t, rr, &created)

	rr = f.do(f.authRequest(http.MethodPatch, "/v1/admin/anomaly-rules/"+created.ID, map[string]any{
		"name": "rA-novo", "app": "app-A", "metric": "error_count",
		"windowMinutes": 15, "baselineWeeks": 4, "sensitivity": 3,
		"direction": "above", "silenceSeconds": 60,
		"channel": "webhook", "targetUrl": "http://x", "active": false,
	}, uid))
	requireStatus(t, rr, http.StatusOK)

	rr = f.do(f.authRequest(http.MethodDelete, "/v1/admin/anomaly-rules/"+created.ID, nil, uid))
	requireStatus(t, rr, http.StatusOK)
}

func TestSampling_CRUDAdmin(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	f.seedApp("app-A", "c-default")

	rr := f.do(f.authRequest(http.MethodPost, "/v1/admin/sampling-rules", map[string]any{
		"app": "app-A", "eventType": "page_view",
		"sampleRate": 0.5, "keepErrors": true, "active": true,
	}, uid))
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var created domain.SamplingRule
	decodeJSON(t, rr, &created)

	rr = f.do(f.authRequest(http.MethodPatch, "/v1/admin/sampling-rules/"+created.ID, map[string]any{
		"app": "app-A", "eventType": "page_view",
		"sampleRate": 0.1, "keepErrors": true, "active": true,
	}, uid))
	requireStatus(t, rr, http.StatusOK)

	rr = f.do(f.authRequest(http.MethodDelete, "/v1/admin/sampling-rules/"+created.ID, nil, uid))
	requireStatus(t, rr, http.StatusOK)
}

// -- Source maps delete admin ---------------------------------------

func TestSourceMaps_DeleteAdmin(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	f.seedApp("app-A", "c-default")
	sm, _ := f.sourceMaps.UpsertSourceMap(context.Background(), domain.SourceMap{
		App: "app-A", Release: "1.0", Filename: "main.js",
	}, "content")

	rr := f.do(f.authRequest(http.MethodDelete, "/v1/admin/source-maps/"+sm.ID, nil, uid))
	requireStatus(t, rr, http.StatusOK)
}

// -- Feedback delete admin ------------------------------------------

func TestFeedback_DeleteAdmin(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	f.seedApp("app-A", "c-default")
	fb, _ := f.feedback.CreateFeedback(context.Background(), domain.UserFeedback{
		App: "app-A", SessionID: "s1", Type: domain.FeedbackBug, Message: "m",
	})

	rr := f.do(f.authRequest(http.MethodDelete, "/v1/admin/feedbacks/"+fb.ID, nil, uid))
	requireStatus(t, rr, http.StatusOK)
}

// -- Companies GET/PATCH da própria (não-super) ---------------------

func TestCompany_GetPatchDaPropriaCompany(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/companies/c-default", nil, uid))
	requireStatus(t, rr, http.StatusOK)

	newName := "Padrão-Renomeada"
	rr = f.do(f.authRequest(http.MethodPatch, "/v1/admin/companies/c-default",
		map[string]any{"name": newName}, uid))
	requireStatus(t, rr, http.StatusOK)
}

// -- User permissions granulares (RBAC) ------------------------------

func TestUserPermissions_GetPutDelete(t *testing.T) {
	f := newFixture(t)
	admin := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	target := f.seedUser("t@x", domain.RoleViewer, "c-default", false)
	appID := f.seedApp("app-A", "c-default")

	// GET vazio.
	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/users/"+target+"/permissions", nil, admin))
	requireStatus(t, rr, http.StatusOK)

	// PUT concede.
	rr = f.do(f.authRequest(http.MethodPut, "/v1/admin/users/"+target+"/permissions/"+appID,
		map[string]any{"role": "viewer"}, admin))
	requireStatus(t, rr, http.StatusOK)

	// DELETE revoga.
	rr = f.do(f.authRequest(http.MethodDelete, "/v1/admin/users/"+target+"/permissions/"+appID, nil, admin))
	requireStatus(t, rr, http.StatusOK)
}

// -- Comment delete pelo autor --------------------------------------

func TestIssueComment_DeletePeloAutor(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("e@x", domain.RoleEditor, "c-default", false)

	// Cria comentário via API.
	rr := f.do(f.authRequest(http.MethodPost, "/v1/issues/fp-1/comments",
		map[string]any{"body": "meu texto"}, uid))
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("create comment: %d %s", rr.Code, rr.Body.String())
	}
	var created domain.IssueComment
	decodeJSON(t, rr, &created)

	rr = f.do(f.authRequest(http.MethodDelete, "/v1/issues/comments/"+created.ID, nil, uid))
	requireStatus(t, rr, http.StatusOK)
}
