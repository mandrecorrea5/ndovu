package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// -----------------------------------------------------------------------
// /v1/admin/alerts
// -----------------------------------------------------------------------

func TestAdminAlerts_ListagemFiltraPorScope(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	f.seedApp("app-A", "c-default")
	f.seedApp("app-B", companyB)

	// Plant 2 regras: uma pra cada app.
	_, _ = f.alerts.CreateAlertRule(context.Background(), domain.AlertRule{
		Name: "r-A", App: "app-A", Threshold: 1, WindowSeconds: 60,
		Channel: domain.AlertChannelWebhook, TargetURL: "http://x",
	})
	_, _ = f.alerts.CreateAlertRule(context.Background(), domain.AlertRule{
		Name: "r-B", App: "app-B", Threshold: 1, WindowSeconds: 60,
		Channel: domain.AlertChannelWebhook, TargetURL: "http://x",
	})

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/alerts", nil, adminA))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Alerts []domain.AlertRule }
	decodeJSON(t, rr, &body)
	if len(body.Alerts) != 1 || body.Alerts[0].App != "app-A" {
		t.Errorf("admin A deveria ver só r-A, veio %+v", body.Alerts)
	}
}

func TestAdminAlerts_PostBloqueiaAppForaDoScope(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	f.seedApp("app-B", companyB)

	rr := f.do(f.authRequest(http.MethodPost, "/v1/admin/alerts", map[string]any{
		"name":          "hack",
		"app":           "app-B", // fora do scope de admin A
		"threshold":     10,
		"windowSeconds": 300,
		"channel":       "webhook",
		"targetUrl":     "http://x",
	}, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminAlerts_PostAppSemNomeBloqueado(t *testing.T) {
	// Regra "global" (app vazio) só faz sentido pra super.
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)

	rr := f.do(f.authRequest(http.MethodPost, "/v1/admin/alerts", map[string]any{
		"name": "global", "app": "",
		"threshold": 10, "windowSeconds": 300,
		"channel": "webhook", "targetUrl": "http://x",
	}, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminAlerts_DeleteBloqueiaAppForaDoScope(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	f.seedApp("app-B", companyB)
	created, _ := f.alerts.CreateAlertRule(context.Background(), domain.AlertRule{
		Name: "r", App: "app-B", Threshold: 1, WindowSeconds: 60,
		Channel: domain.AlertChannelSlack, TargetURL: "http://x",
	})

	rr := f.do(f.authRequest(http.MethodDelete, "/v1/admin/alerts/"+created.ID, nil, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

// -----------------------------------------------------------------------
// /v1/admin/anomaly-rules
// -----------------------------------------------------------------------

func TestAdminAnomaly_ListagemFiltraPorScope(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	f.seedApp("app-A", "c-default")
	f.seedApp("app-B", companyB)
	_, _ = f.anomaly.CreateAnomalyRule(context.Background(), domain.AnomalyRule{Name: "rA", App: "app-A"})
	_, _ = f.anomaly.CreateAnomalyRule(context.Background(), domain.AnomalyRule{Name: "rB", App: "app-B"})

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/anomaly-rules", nil, adminA))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Rules []domain.AnomalyRule }
	decodeJSON(t, rr, &body)
	if len(body.Rules) != 1 || body.Rules[0].App != "app-A" {
		t.Errorf("admin A deveria ver só rA, veio %+v", body.Rules)
	}
}

func TestAdminAnomaly_DetectionsFiltraPorScope(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	f.seedApp("app-A", "c-default")
	f.seedApp("app-B", companyB)
	ruleA, _ := f.anomaly.CreateAnomalyRule(context.Background(), domain.AnomalyRule{Name: "rA", App: "app-A"})
	ruleB, _ := f.anomaly.CreateAnomalyRule(context.Background(), domain.AnomalyRule{Name: "rB", App: "app-B"})
	_ = f.anomaly.RecordDetection(context.Background(), domain.AnomalyDetection{RuleID: ruleA.ID})
	_ = f.anomaly.RecordDetection(context.Background(), domain.AnomalyDetection{RuleID: ruleB.ID})

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/anomaly-detections", nil, adminA))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Detections []domain.AnomalyDetection }
	decodeJSON(t, rr, &body)
	if len(body.Detections) != 1 || body.Detections[0].RuleID != ruleA.ID {
		t.Errorf("admin A deveria ver só detection de rA, veio %+v", body.Detections)
	}
}

// -----------------------------------------------------------------------
// /v1/admin/sampling-rules
// -----------------------------------------------------------------------

func TestAdminSampling_ListagemFiltraPorScope(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	f.seedApp("app-A", "c-default")
	f.seedApp("app-B", companyB)
	_, _ = f.sampling.CreateSamplingRule(context.Background(), domain.SamplingRule{App: "app-A"})
	_, _ = f.sampling.CreateSamplingRule(context.Background(), domain.SamplingRule{App: "app-B"})

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/sampling-rules", nil, adminA))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Rules []domain.SamplingRule }
	decodeJSON(t, rr, &body)
	if len(body.Rules) != 1 || body.Rules[0].App != "app-A" {
		t.Errorf("admin A deveria ver só rule de app-A, veio %+v", body.Rules)
	}
}

// -----------------------------------------------------------------------
// /v1/admin/source-maps
// -----------------------------------------------------------------------

func TestAdminSourceMaps_ListagemFiltraPorScope(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	f.seedApp("app-A", "c-default")
	f.seedApp("app-B", companyB)
	_, _ = f.sourceMaps.UpsertSourceMap(context.Background(), domain.SourceMap{App: "app-A", Release: "1.0", Filename: "x.js"}, "")
	_, _ = f.sourceMaps.UpsertSourceMap(context.Background(), domain.SourceMap{App: "app-B", Release: "1.0", Filename: "y.js"}, "")

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/source-maps", nil, adminA))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ SourceMaps []domain.SourceMap }
	decodeJSON(t, rr, &body)
	if len(body.SourceMaps) != 1 || body.SourceMaps[0].App != "app-A" {
		t.Errorf("admin A deveria ver só x.js, veio %+v", body.SourceMaps)
	}
}

func TestAdminSourceMaps_PostBloqueiaAppForaDoScope(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	f.seedApp("app-B", companyB)

	rr := f.do(f.authRequest(http.MethodPost, "/v1/admin/source-maps", map[string]any{
		"app": "app-B", "release": "1.0", "filename": "x.js", "content": "{}",
	}, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

// -----------------------------------------------------------------------
// /v1/admin/feedbacks
// -----------------------------------------------------------------------

func TestAdminFeedbacks_ListagemFiltraPorScope(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	f.seedApp("app-A", "c-default")
	f.seedApp("app-B", companyB)
	_, _ = f.feedback.CreateFeedback(context.Background(), domain.UserFeedback{
		App: "app-A", SessionID: "s1", Message: "m", Type: domain.FeedbackBug,
	})
	_, _ = f.feedback.CreateFeedback(context.Background(), domain.UserFeedback{
		App: "app-B", SessionID: "s2", Message: "m", Type: domain.FeedbackBug,
	})

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/feedbacks", nil, adminA))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Feedbacks []domain.UserFeedback }
	decodeJSON(t, rr, &body)
	if len(body.Feedbacks) != 1 || body.Feedbacks[0].App != "app-A" {
		t.Errorf("admin A deveria ver só feedback de app-A, veio %+v", body.Feedbacks)
	}
}

func TestAdminFeedbacks_PatchStatusBloqueiaCrossTenant(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	f.seedApp("app-B", companyB)
	fb, _ := f.feedback.CreateFeedback(context.Background(), domain.UserFeedback{
		App: "app-B", SessionID: "s", Message: "m", Type: domain.FeedbackBug,
	})

	rr := f.do(f.authRequest(http.MethodPatch, "/v1/admin/feedbacks/"+fb.ID,
		map[string]any{"status": "resolved"}, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

// -----------------------------------------------------------------------
// /v1/admin/gdpr — só super
// -----------------------------------------------------------------------

func TestAdminGDPR_ExportBloqueadoParaNaoSuper(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/gdpr/user/user-x/export", nil, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminGDPR_ForgetBloqueadoParaNaoSuper(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)

	rr := f.do(f.authRequest(http.MethodDelete, "/v1/admin/gdpr/user/user-x", nil, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminGDPR_ExportSuperOK(t *testing.T) {
	f := newFixture(t)
	super := f.seedUser("s@x", domain.RoleAdmin, "c-default", true)

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/gdpr/user/user-x/export", nil, super))
	requireStatus(t, rr, http.StatusOK)
}

// -----------------------------------------------------------------------
// /v1/admin/audit-log
// -----------------------------------------------------------------------

func TestAdminAudit_AdminNaoAcessaSemBearer(t *testing.T) {
	f := newFixture(t)
	rr := f.do(f.req(http.MethodGet, "/v1/admin/audit-log", nil))
	requireStatus(t, rr, http.StatusUnauthorized)
}

func TestAdminAudit_ListagemOK(t *testing.T) {
	f := newFixture(t)
	admin := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/audit-log", nil, admin))
	requireStatus(t, rr, http.StatusOK)
}
