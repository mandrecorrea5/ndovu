//go:build integration

package ctlpostgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/ctlpostgres"
	"github.com/marcoscorrea/ndovu/backend/internal/adapter/testenv"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// seedUser é helper reutilizado — cria user na company default e devolve o id.
func seedUser(t *testing.T, repo *ctlpostgres.Repository, email string, role domain.Role) string {
	t.Helper()
	u, err := repo.CreateUser(context.Background(), domain.User{
		Email: email, Name: email, Role: role, Active: true,
		CompanyID: firstCompanyID(t, repo),
	}, "$2a$10$placeholder")
	if err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}
	return u.ID
}

func seedApp(t *testing.T, repo *ctlpostgres.Repository, name string) string {
	t.Helper()
	a, err := repo.CreateApp(context.Background(), domain.App{
		Name: name, CompanyID: firstCompanyID(t, repo),
	})
	if err != nil {
		t.Fatalf("seed app %s: %v", name, err)
	}
	return a.ID
}

// -- Issues ------------------------------------------------------------

func TestIssues_UpsertStatusEGetStates(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	appID := seedApp(t, repo, "app-1")
	_ = appID

	err := repo.UpsertIssueStatus(ctx, domain.IssueStatusInput{
		Fingerprint: "fp-1", App: "app-1", Status: domain.IssueResolved,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	states, err := repo.GetIssueStates(ctx, []string{"fp-1", "fp-nao-existe"})
	if err != nil {
		t.Fatalf("get states: %v", err)
	}
	if states["fp-1"].Status != domain.IssueResolved {
		t.Errorf("state fp-1 errado: %+v", states["fp-1"])
	}
	if _, ok := states["fp-nao-existe"]; ok {
		t.Error("fp inexistente não deveria estar no map")
	}
}

func TestIssues_CommentsCRUD(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	uid := seedUser(t, repo, "commenter@x", domain.RoleEditor)

	c, err := repo.CreateIssueComment(ctx, "fp-x", uid, "primeiro comentário")
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if c.ID == "" || c.Body != "primeiro comentário" {
		t.Errorf("comment mal preenchido: %+v", c)
	}

	list, err := repo.ListIssueComments(ctx, "fp-x")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("esperava 1 comment, veio %d", len(list))
	}

	// Delete pelo requester errado bloqueia.
	other := seedUser(t, repo, "outro@x", domain.RoleEditor)
	if err := repo.DeleteIssueComment(ctx, c.ID, other); !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("outro user apagando comment: esperava Forbidden/NotFound, veio %v", err)
	}
	// Autor deleta OK.
	if err := repo.DeleteIssueComment(ctx, c.ID, uid); err != nil {
		t.Fatalf("delete pelo autor: %v", err)
	}
}

// -- Alerts ------------------------------------------------------------

func TestAlerts_CRUD(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	seedApp(t, repo, "app-1")

	rule, err := repo.CreateAlertRule(ctx, domain.AlertRule{
		Name: "r1", App: "app-1", ErrorCode: "AUTH_401",
		Threshold: 10, WindowSeconds: 300,
		Channel: domain.AlertChannelWebhook, TargetURL: "http://x",
		SilenceSeconds: 60, Active: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetAlertRule(ctx, rule.ID)
	if err != nil || got.Name != "r1" {
		t.Errorf("get: %v / %+v", err, got)
	}

	list, _ := repo.ListAlertRules(ctx)
	if len(list) != 1 {
		t.Errorf("list: esperava 1, veio %d", len(list))
	}

	if err := repo.DeleteAlertRule(ctx, rule.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetAlertRule(ctx, rule.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deveria ser NotFound pós-delete")
	}
}

func TestAlerts_LastDeliveryAndRecord(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	seedApp(t, repo, "app-1")
	rule, _ := repo.CreateAlertRule(ctx, domain.AlertRule{
		Name: "r", App: "app-1", Threshold: 1, WindowSeconds: 60,
		Channel: domain.AlertChannelSlack, TargetURL: "http://x",
	})

	// Sem delivery ainda → zero time.
	last, err := repo.LastDeliveryAt(ctx, rule.ID)
	if err != nil {
		t.Fatalf("last: %v", err)
	}
	if !last.IsZero() {
		t.Errorf("sem delivery, esperava zero time, veio %v", last)
	}

	// Grava delivery.
	if err := repo.RecordDelivery(ctx, rule.ID, 5, true, "ok"); err != nil {
		t.Fatalf("record: %v", err)
	}
	last, _ = repo.LastDeliveryAt(ctx, rule.ID)
	if last.IsZero() {
		t.Error("last delivery deveria ter sido gravado")
	}
}

// -- Anomaly rules + detections ----------------------------------------

func TestAnomaly_CRUDRegrasEDetections(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	seedApp(t, repo, "app-1")

	rule, err := repo.CreateAnomalyRule(ctx, domain.AnomalyRule{
		Name: "spike", App: "app-1", Metric: domain.AnomalyErrorCount,
		WindowMinutes: 15, BaselineWeeks: 4, Sensitivity: 3,
		Direction: domain.AnomalyAbove, SilenceSeconds: 60,
		Channel: domain.AlertChannelWebhook, TargetURL: "http://x", Active: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.GetAnomalyRule(ctx, rule.ID); err != nil {
		t.Errorf("get: %v", err)
	}

	// Update
	rule.Sensitivity = 4
	updated, err := repo.UpdateAnomalyRule(ctx, rule.ID, rule)
	if err != nil || updated.Sensitivity != 4 {
		t.Errorf("update: %v / sens=%v", err, updated.Sensitivity)
	}

	// Detection
	det := domain.AnomalyDetection{
		RuleID: rule.ID, DetectedAt: time.Now().UTC(),
		CurrentValue: 100, BaselineAvg: 10, BaselineStddev: 2, ZScore: 45,
		Direction: domain.AnomalyAbove, NotifyOK: true,
	}
	if err := repo.RecordDetection(ctx, det); err != nil {
		t.Fatalf("record detection: %v", err)
	}
	last, err := repo.LastDetection(ctx, rule.ID)
	if err != nil {
		t.Fatalf("last: %v", err)
	}
	if last.RuleID != rule.ID || last.CurrentValue != 100 {
		t.Errorf("last errado: %+v", last)
	}

	list, total, err := repo.ListDetections(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list detections: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Errorf("esperava 1 detection, veio %d/%d", len(list), total)
	}

	if err := repo.DeleteAnomalyRule(ctx, rule.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// -- Sampling rules ----------------------------------------------------

func TestSampling_CRUD(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	seedApp(t, repo, "app-1")

	rule, err := repo.CreateSamplingRule(ctx, domain.SamplingRule{
		App: "app-1", EventType: "page_view", SampleRate: 0.5,
		KeepErrors: true, Active: true, Note: "teste",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.GetSamplingRule(ctx, rule.ID); err != nil {
		t.Errorf("get: %v", err)
	}

	rule.SampleRate = 0.1
	upd, err := repo.UpdateSamplingRule(ctx, rule.ID, rule)
	if err != nil || upd.SampleRate != 0.1 {
		t.Errorf("update: %v / rate=%v", err, upd.SampleRate)
	}

	// Unique (app, event_type) → conflict em segundo insert idêntico.
	_, err = repo.CreateSamplingRule(ctx, domain.SamplingRule{
		App: "app-1", EventType: "page_view", SampleRate: 1,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicado deveria ser conflict, veio %v", err)
	}

	list, _ := repo.ListSamplingRules(ctx)
	if len(list) != 1 {
		t.Errorf("esperava 1 rule, veio %d", len(list))
	}
	_ = repo.DeleteSamplingRule(ctx, rule.ID)
}

// -- Feedbacks ---------------------------------------------------------

func TestFeedbacks_CRUDEFiltros(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	seedApp(t, repo, "app-1")

	fb1, err := repo.CreateFeedback(ctx, domain.UserFeedback{
		App: "app-1", SessionID: "s-1", Type: domain.FeedbackBug,
		Message: "botão travou", Email: "cliente@x",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, _ = repo.CreateFeedback(ctx, domain.UserFeedback{
		App: "app-1", SessionID: "s-2", Type: domain.FeedbackSuggestion,
		Message: "seria bom X",
	})

	// Get by id.
	got, err := repo.GetFeedback(ctx, fb1.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.FeedbackNew {
		t.Errorf("status inicial deveria ser new, veio %q", got.Status)
	}

	// Update status.
	uid := seedUser(t, repo, "triagem@x", domain.RoleAdmin)
	upd, err := repo.UpdateFeedbackStatus(ctx, fb1.ID, domain.FeedbackResolved, uid)
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if upd.Status != domain.FeedbackResolved || upd.ResolvedBy != uid {
		t.Errorf("update errado: %+v", upd)
	}

	// Filtro por sessão (usado no fallback de session detail).
	bySession, _, err := repo.ListFeedbacks(ctx, domain.FeedbackFilter{SessionID: "s-1"})
	if err != nil {
		t.Fatalf("list by sess: %v", err)
	}
	if len(bySession) != 1 || bySession[0].SessionID != "s-1" {
		t.Errorf("filtro por sessão errado: %+v", bySession)
	}

	// Filtro por app + status resolved.
	byStatus, _, _ := repo.ListFeedbacks(ctx, domain.FeedbackFilter{App: "app-1", Status: "resolved"})
	if len(byStatus) != 1 {
		t.Errorf("filtro status: esperava 1, veio %d", len(byStatus))
	}

	// Delete.
	if err := repo.DeleteFeedback(ctx, fb1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetFeedback(ctx, fb1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deveria ter sumido")
	}
}

// -- Audit -------------------------------------------------------------

func TestAudit_InsertOnly_EFiltroPorCompany(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()

	cA := firstCompanyID(t, repo)
	cB, _ := repo.CreateCompany(ctx, domain.Company{Name: "B"})
	uA, _ := repo.CreateUser(ctx, domain.User{
		Email: "a@x", Name: "a", Role: domain.RoleAdmin, Active: true, CompanyID: cA,
	}, "h")
	uB, _ := repo.CreateUser(ctx, domain.User{
		Email: "b@x", Name: "b", Role: domain.RoleAdmin, Active: true, CompanyID: cB.ID,
	}, "h")

	_ = repo.RecordAudit(ctx, domain.AuditEntry{
		ActorUserID: uA.ID, ActorEmail: uA.Email, Action: "x.create",
	})
	_ = repo.RecordAudit(ctx, domain.AuditEntry{
		ActorUserID: uB.ID, ActorEmail: uB.Email, Action: "y.create",
	})

	// Filtro por company do actor.
	list, total, err := repo.ListAudit(ctx, domain.AuditFilter{CompanyID: cA, Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ActorEmail != "a@x" {
		t.Errorf("filtro por company falhou: total=%d, list=%+v", total, list)
	}

	// Sem filtro (super) vê os 2.
	allList, allTotal, _ := repo.ListAudit(ctx, domain.AuditFilter{Limit: 50})
	if allTotal != 2 || len(allList) != 2 {
		t.Errorf("sem filtro: esperava 2, veio %d/%d", len(allList), allTotal)
	}
}

// -- Permissions -------------------------------------------------------

func TestPermissions_GrantRevokeList(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	uid := seedUser(t, repo, "v@x", domain.RoleViewer)
	appID := seedApp(t, repo, "app-1")

	p, err := repo.GrantAppPermission(ctx, uid, appID, "viewer", "")
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if p.UserID != uid || p.AppID != appID {
		t.Errorf("grant errado: %+v", p)
	}

	// Upsert: mesma chave devolve o mesmo com role atualizada.
	p2, err := repo.GrantAppPermission(ctx, uid, appID, "editor", "")
	if err != nil {
		t.Fatalf("grant upsert: %v", err)
	}
	if p2.Role != "editor" {
		t.Errorf("upsert não atualizou role")
	}

	list, err := repo.ListAppPermissionsForUser(ctx, uid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("esperava 1 grant, veio %d", len(list))
	}

	names, err := repo.ListAppNamesForUser(ctx, uid)
	if err != nil {
		t.Fatalf("list names: %v", err)
	}
	if len(names) != 1 || names[0] != "app-1" {
		t.Errorf("names errado: %v", names)
	}

	if err := repo.RevokeAppPermission(ctx, uid, appID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	list, _ = repo.ListAppPermissionsForUser(ctx, uid)
	if len(list) != 0 {
		t.Errorf("esperava 0 pós-revoke, veio %d", len(list))
	}
}

// -- Saved Views -------------------------------------------------------

func TestSavedViews_CRUDPorOwner(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	uid := seedUser(t, repo, "u@x", domain.RoleEditor)

	v, err := repo.CreateSavedView(ctx, domain.SavedView{
		OwnerUserID: uid, ViewType: "traces", Name: "meus filtros",
		Filters: json.RawMessage(`{"app":"x"}`), IsShared: false,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Update pelo owner OK.
	upd, err := repo.UpdateSavedView(ctx, v.ID, uid, "renomeada", json.RawMessage(`{}`), true)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Name != "renomeada" || !upd.IsShared {
		t.Errorf("update falhou: %+v", upd)
	}

	// Update por outro user → ErrNotFound (o WHERE não bate).
	other := seedUser(t, repo, "o@x", domain.RoleViewer)
	_, err = repo.UpdateSavedView(ctx, v.ID, other, "hack", json.RawMessage(`{}`), true)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("update por não-dono: esperava NotFound, veio %v", err)
	}

	// List: dono vê + compartilhadas.
	list, err := repo.ListSavedViews(ctx, uid, "traces")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("esperava 1 view, veio %d", len(list))
	}

	// Delete por não-dono falha; por dono OK.
	if err := repo.DeleteSavedView(ctx, v.ID, other); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("delete por não-dono: %v", err)
	}
	if err := repo.DeleteSavedView(ctx, v.ID, uid); err != nil {
		t.Fatalf("delete dono: %v", err)
	}
}

// -- Funnels -----------------------------------------------------------

func TestFunnels_CRUD(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	uid := seedUser(t, repo, "e@x", domain.RoleEditor)
	seedApp(t, repo, "portal")

	steps := json.RawMessage(`[
		{"name":"s1","match":{"type":"page_view"}},
		{"name":"s2","match":{"type":"action"}}
	]`)
	fn, err := repo.CreateFunnel(ctx, domain.Funnel{
		App: "portal", Name: "onboarding", WindowSeconds: 1800,
		Steps: steps, CreatedBy: uid,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetFunnel(ctx, fn.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "onboarding" {
		t.Errorf("get errado: %+v", got)
	}

	upd, err := repo.UpdateFunnel(ctx, fn.ID, "novo-nome", 900, steps)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Name != "novo-nome" || upd.WindowSeconds != 900 {
		t.Errorf("update falhou: %+v", upd)
	}

	// List filtra por app.
	list, err := repo.ListFunnels(ctx, "portal")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("esperava 1 funnel, veio %d", len(list))
	}
	other, _ := repo.ListFunnels(ctx, "outro-app")
	if len(other) != 0 {
		t.Errorf("outro-app não deveria ter funnel")
	}

	if err := repo.DeleteFunnel(ctx, fn.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// -- Source Maps -------------------------------------------------------

func TestSourceMaps_UpsertGetContentEDelete(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	seedApp(t, repo, "portal")

	content := `{"version":3,"file":"main.js"}`
	m, err := repo.UpsertSourceMap(ctx, domain.SourceMap{
		App: "portal", Release: "1.0", Filename: "main.js",
	}, content)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if m.SizeBytes != len(content) {
		t.Errorf("size errado: %d vs %d", m.SizeBytes, len(content))
	}

	// Reupsert (rebuild) atualiza sem duplicar.
	newContent := `{"version":3,"file":"main.js","v":2}`
	m2, err := repo.UpsertSourceMap(ctx, domain.SourceMap{
		App: "portal", Release: "1.0", Filename: "main.js",
	}, newContent)
	if err != nil {
		t.Fatalf("reupsert: %v", err)
	}
	if m2.ID != m.ID {
		t.Errorf("upsert deveria manter o mesmo id: %s vs %s", m.ID, m2.ID)
	}

	// GetSourceMap por id.
	got, err := repo.GetSourceMap(ctx, m.ID)
	if err != nil || got.Filename != "main.js" {
		t.Errorf("get: %v / %+v", err, got)
	}

	// GetSourceMapContent (por app+release+filename).
	fetched, err := repo.GetSourceMapContent(ctx, "portal", "1.0", "main.js")
	if err != nil {
		t.Fatalf("get content: %v", err)
	}
	if fetched != newContent {
		t.Errorf("conteúdo divergente")
	}

	// List com filtros.
	list, _ := repo.ListSourceMaps(ctx, "portal", "1.0")
	if len(list) != 1 {
		t.Errorf("list: esperava 1, veio %d", len(list))
	}

	if err := repo.DeleteSourceMap(ctx, m.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetSourceMap(ctx, m.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deveria estar deletado")
	}
}
