package httpapi

// Fixture central para testes de handlers HTTP. Monta um *Handlers com
// todos os services instanciados sobre stores nop (ver nop_stores_test.go)
// — cada teste pode plantar dados nos stores expostos via campo público.
//
// Idéia: os tests exercitam o pipeline REAL (router + middlewares +
// handlers + services + validações), fake só o storage. Isso pega bugs
// de encanamento (rotas erradas, middleware faltando, JSON malformado)
// que unit test de service não pegaria.

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/config"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/usecase"
)

// emptyFS é um embed.FS zerado — o parâmetro openapi de NewRouter só é
// consultado no handler GET /openapi.yaml, que não exercitamos em testes.
var emptyFS embed.FS

// testFixture reúne handlers + stores + router pra os testes.
// Stores são expostos para o teste plantar dados/checar side effects.
type testFixture struct {
	t          *testing.T
	handlers   *Handlers
	router     http.Handler
	verifier   *usecase.AuthService
	authSecret string

	// Stores acessíveis pra plantar dados.
	users      *nopUserStore
	companies  *nopCompanyStore
	apps       *nopAppStore
	keys       *nopKeyStore
	issues     *nopIssueStore
	feedback   *nopFeedbackStore
	alerts     *nopAlertStore
	anomaly    *nopAnomalyStore
	sampling   *nopSamplingStore
	sourceMaps *nopSourceMapStore
	perms      *nopPermStore
	savedViews *nopSavedViewStore
	funnels    *nopFunnelStore
	audit      *nopAuditStore
	stream     *nopStream
	reader     nopReader
	snapMeta   *nopSnapshotMetaStore
	snapBlob   *nopSnapshotBlobStore
}

// newFixture cria um fixture zerado. Testes seedam via helpers.
func newFixture(t *testing.T) *testFixture {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	f := &testFixture{
		t:          t,
		authSecret: "test-secret",
		users:      newNopUserStore(),
		companies:  newNopCompanyStore(),
		apps:       newNopAppStore(),
		keys:       newNopKeyStore(),
		issues:     newNopIssueStore(),
		feedback:   newNopFeedbackStore(),
		alerts:     newNopAlertStore(),
		anomaly:    newNopAnomalyStore(),
		sampling:   newNopSamplingStore(),
		sourceMaps: newNopSourceMapStore(),
		perms:      &nopPermStore{},
		savedViews: newNopSavedViewStore(),
		funnels:    newNopFunnelStore(),
		audit:      &nopAuditStore{},
		stream:     &nopStream{},
		snapMeta:   &nopSnapshotMetaStore{},
		snapBlob:   &nopSnapshotBlobStore{},
	}

	authSvc := usecase.NewAuthService(f.users, f.companies, f.authSecret, time.Hour, logger)
	keySvc := usecase.NewAPIKeyService(f.keys, 30*time.Second, 0, logger) // rateRPS=0 = desligado
	appSvc := usecase.NewAppService(f.apps, keySvc, logger)
	companySvc := usecase.NewCompanyService(f.companies, logger)
	issueSvc := usecase.NewIssueService(f.reader, f.issues)
	alertSvc := usecase.NewAlertService(f.alerts, f.reader, logger)
	releaseSvc := usecase.NewReleaseService(f.reader)
	sourceMapSvc := usecase.NewSourceMapService(f.sourceMaps, logger)
	savedViewSvc := usecase.NewSavedViewService(f.savedViews)
	funnelSvc := usecase.NewFunnelService(f.funnels, f.reader)
	retentionSvc := usecase.NewRetentionService(f.reader)
	auditSvc := usecase.NewAuditService(f.audit, logger)
	gdprSvc := usecase.NewGDPRService(f.reader, logger)
	permSvc := usecase.NewPermissionService(f.perms, f.users, f.apps)
	samplingSvc := usecase.NewSamplingService(f.sampling, nil, logger)
	snapshotSvc := usecase.NewSnapshotService(f.snapMeta, f.snapBlob, logger)
	anomalySvc := usecase.NewAnomalyService(f.anomaly, f.reader, usecase.NewAlertDispatcher(), logger)
	feedbackSvc := usecase.NewFeedbackService(f.feedback, logger)
	ingestSvc := usecase.NewIngestService(f.stream, logger)
	querySvc := usecase.NewQueryService(f.reader).WithFeedbackFallback(f.feedback)

	f.verifier = authSvc
	f.handlers = NewHandlers(
		ingestSvc, querySvc, authSvc, keySvc, appSvc, companySvc,
		issueSvc, alertSvc, releaseSvc, sourceMapSvc, savedViewSvc,
		funnelSvc, retentionSvc, nil /* digest */, auditSvc, gdprSvc,
		permSvc, samplingSvc, snapshotSvc, anomalySvc, feedbackSvc, logger,
	)

	// Router real (mesmo do main), com openapi fs vazio (não exercitamos /docs).
	f.router = NewRouter(f.handlers, config.Config{
		MaxBodyBytes: 1 << 20,
	}, authSvc, keySvc, f.apps, f.perms, nil, logger, emptyFS)

	return f
}

// -----------------------------------------------------------------------
// Helpers de request
// -----------------------------------------------------------------------

// do executa uma request no router e devolve o recorder.
func (f *testFixture) do(req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	return rr
}

// req monta uma request com body JSON opcional.
func (f *testFixture) req(method, path string, body any) *http.Request {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			f.t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(b))
	}
	r := httptest.NewRequest(method, path, reader)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

// authRequest monta request com Authorization: Bearer <token> pro user informado.
// O token é gerado via AuthService (JWT válido) — o middleware bearerAuth
// vai reconhecer normalmente.
func (f *testFixture) authRequest(method, path string, body any, userID string) *http.Request {
	r := f.req(method, path, body)
	token := f.tokenFor(userID)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

// keyRequest monta request com X-Api-Key <key> — usado para ingest.
func (f *testFixture) keyRequest(method, path string, body any, plaintextKey string) *http.Request {
	r := f.req(method, path, body)
	r.Header.Set("X-Api-Key", plaintextKey)
	return r
}

// tokenFor devolve um JWT válido para o user informado. Se o user não existe,
// falha o teste. Cache local pra evitar refazer login.
func (f *testFixture) tokenFor(userID string) string {
	f.t.Helper()
	u, err := f.users.GetUserByID(context.Background(), userID)
	if err != nil {
		f.t.Fatalf("user %q não seedado: %v", userID, err)
	}
	// Login sintético: força um SetPassword temporário e faz Login normal
	// pra obter um JWT completo com Identity correta. É mais fiel do que
	// forjar o token manualmente.
	tempPass := "test-pass-1234"
	if err := f.verifier.ResetPassword(context.Background(), userID, tempPass); err != nil {
		f.t.Fatalf("reset password: %v", err)
	}
	result, err := f.verifier.Login(context.Background(), u.Email, tempPass)
	if err != nil {
		f.t.Fatalf("login sintético: %v", err)
	}
	return result.Token
}

// -----------------------------------------------------------------------
// Helpers de seed
// -----------------------------------------------------------------------

// seedCompany cria uma company. Devolve o ID.
func (f *testFixture) seedCompany(name string) string {
	f.t.Helper()
	c, err := f.companies.CreateCompany(context.Background(), domain.Company{Name: name, Active: true})
	if err != nil {
		f.t.Fatalf("seed company: %v", err)
	}
	return c.ID
}

// seedUser cria um user (super pode ser true pra bypass tenantScope).
// Sempre retorna id e email — o password default é "test-pass-1234" pra
// permitir Login sintético via tokenFor().
func (f *testFixture) seedUser(email string, role domain.Role, companyID string, super bool) string {
	f.t.Helper()
	u, err := f.users.CreateUser(context.Background(), domain.User{
		Email: email, Name: email, Role: role, Active: true,
		CompanyID: companyID, IsSuper: super,
	}, "$2a$10$placeholder") // hash é reescrito no tokenFor via ResetPassword
	if err != nil {
		f.t.Fatalf("seed user: %v", err)
	}
	return u.ID
}

// seedApp cria um app associado a uma company.
func (f *testFixture) seedApp(name, companyID string) string {
	f.t.Helper()
	a, err := f.apps.CreateApp(context.Background(), domain.App{Name: name, CompanyID: companyID})
	if err != nil {
		f.t.Fatalf("seed app: %v", err)
	}
	return a.ID
}

// seedKey cria uma chave de ingestão pra um app e devolve o plaintext.
// Usa CreateKey do service (que gera prefix + hash bcrypt corretos)
// para o Validate() reconhecer.
func (f *testFixture) seedKey(appName string) string {
	f.t.Helper()
	// Usa logger silencioso pra não poluir o output dos testes.
	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	keySvc := usecase.NewAPIKeyService(f.keys, 30*time.Second, 0, silent)
	created, err := keySvc.CreateKey(context.Background(), "", appName, "test-key", "")
	if err != nil {
		f.t.Fatalf("seed key: %v", err)
	}
	return created.Key
}

// -----------------------------------------------------------------------
// Helpers de assertion
// -----------------------------------------------------------------------

// requireStatus falha o teste se rr.Code != want. Inclui o body na msg.
func requireStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rr.Code != want {
		t.Fatalf("status = %d, esperado %d; body=%s", rr.Code, want, rr.Body.String())
	}
}

// decodeJSON deserializa a resposta em `out`; falha se não for JSON válido.
func decodeJSON(t *testing.T, rr *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), out); err != nil {
		t.Fatalf("body não é JSON válido: %v; body=%s", err, rr.Body.String())
	}
}
