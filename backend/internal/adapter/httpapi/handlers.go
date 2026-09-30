package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/clickhouse"
	"github.com/marcoscorrea/ndovu/backend/internal/config"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/usecase"
)

// Handlers concentra as dependências dos endpoints.
type Handlers struct {
	ingest     *usecase.IngestService
	query      *usecase.QueryService
	auth       *usecase.AuthService
	keys       *usecase.APIKeyService
	apps       *usecase.AppService
	companies  *usecase.CompanyService
	issues     *usecase.IssueService
	alerts     *usecase.AlertService
	releases   *usecase.ReleaseService
	sourceMaps *usecase.SourceMapService
	savedViews *usecase.SavedViewService
	funnels    *usecase.FunnelService
	retention  *usecase.RetentionService
	digest      *usecase.DigestService
	audit       *usecase.AuditService
	gdpr        *usecase.GDPRService
	permissions *usecase.PermissionService
	sampling    *usecase.SamplingService
	snapshots   *usecase.SnapshotService
	anomalies   *usecase.AnomalyService
	feedback    *usecase.FeedbackService
	logger      *slog.Logger
	cfg         config.Config
}

// NewHandlers cria o conjunto de handlers da API.
func NewHandlers(
	ingest *usecase.IngestService,
	query *usecase.QueryService,
	auth *usecase.AuthService,
	keys *usecase.APIKeyService,
	apps *usecase.AppService,
	companies *usecase.CompanyService,
	issues *usecase.IssueService,
	alerts *usecase.AlertService,
	releases *usecase.ReleaseService,
	sourceMaps *usecase.SourceMapService,
	savedViews *usecase.SavedViewService,
	funnels *usecase.FunnelService,
	retention *usecase.RetentionService,
	digest *usecase.DigestService,
	audit *usecase.AuditService,
	gdpr *usecase.GDPRService,
	permissions *usecase.PermissionService,
	sampling *usecase.SamplingService,
	snapshots *usecase.SnapshotService,
	anomalies *usecase.AnomalyService,
	feedback *usecase.FeedbackService,
	logger *slog.Logger,
	cfg config.Config,
) *Handlers {
	return &Handlers{
		ingest: ingest, query: query, auth: auth, keys: keys, apps: apps,
		companies: companies, issues: issues, alerts: alerts, releases: releases,
		sourceMaps: sourceMaps, savedViews: savedViews,
		funnels: funnels, retention: retention, digest: digest, audit: audit,
		gdpr: gdpr, permissions: permissions, sampling: sampling,
		snapshots: snapshots, anomalies: anomalies, feedback: feedback,
		logger: logger, cfg: cfg,
	}
}

// recordAudit é o helper curto para gravar auditoria a partir de um handler.
// Consome a identidade do JWT + IP/UA da request atuais.
func (h *Handlers) recordAudit(r *http.Request, action, resourceType, resourceID string, details any) {
	if h.audit == nil {
		return
	}
	identity, _ := IdentityFrom(r.Context())
	h.audit.Record(r.Context(), usecase.RecordInput{
		Actor:        identity,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      details,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
	})
}

// scopeFrom devolve a whitelist de apps que o requester pode consultar.
// nil = sem restrição (super-admin ou nenhum middleware aplicado).
func scopeFrom(r *http.Request) []string {
	if scope, ok := AppScopeFrom(r.Context()); ok {
		return scope
	}
	return nil
}

// clientIP resolve o IP real considerando proxies comuns (X-Forwarded-For).
// Só o primeiro valor da lista — os seguintes são proxies intermediários.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xr := r.Header.Get("X-Real-Ip"); xr != "" {
		return xr
	}
	// r.RemoteAddr = "ip:port" — o "port" varia por request, só o IP interessa.
	if i := strings.LastIndex(r.RemoteAddr, ":"); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

// ---------------------------------------------------------------------------
// Ingestão
// ---------------------------------------------------------------------------

// PostEvents recebe um lote de traces de qualquer frontend (contrato v1).
func (h *Handlers) PostEvents(w http.ResponseWriter, r *http.Request) {
	var req ingestRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge,
				errorResponse{Error: "payload acima do limite"})
			return
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido: " + err.Error()})
		return
	}

	// Vincula o envelope ao frontend emissor: cada chave pertence a um app e
	// só pode rotular eventos com esse app. Impede que uma chave vazada seja
	// usada para injetar traces em nome de outro frontend.
	if key, ok := APIKeyFrom(r.Context()); ok && req.App != key.App {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "app do envelope não corresponde ao app da chave",
			Details: []string{
				fmt.Sprintf("a chave pertence ao app %q, mas o envelope enviou %q", key.App, req.App),
			},
		})
		return
	}

	batch := req.toDomain()
	// Se o cliente propagou W3C traceparent no header, usa como fallback
	// para eventos que não trouxeram trace própria (padrão OTel para
	// correlacionar toda a requisição HTTP com um trace específico).
	if parent := parseTraceparent(r.Header.Get("traceparent")); parent != nil {
		for i := range batch.Events {
			if batch.Events[i].Trace == nil {
				batch.Events[i].Trace = parent
			}
		}
	}
	result, err := h.ingest.Ingest(r.Context(), batch)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

// ---------------------------------------------------------------------------
// Consulta (read-only)
// ---------------------------------------------------------------------------

// GetEvents lista eventos com o filtro combinado vindo da querystring.
func (h *Handlers) GetEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := domain.EventFilter{
		App:       q.Get("app"),
		AppScope:  scopeFrom(r),
		Release:   q.Get("release"),
		UserID:    q.Get("userId"),
		SessionID: q.Get("sessionId"),
		Type:      domain.EventType(q.Get("type")),
		Feature:   q.Get("feature"),
		Name:      q.Get("name"),
		Screen:    q.Get("screen"),
		HTTPURL:   q.Get("route"),
		Search:    q.Get("search"),
	}
	var err error
	if f.From, err = parseTime(q.Get("from")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'from' deve ser RFC3339"))
		return
	}
	if f.To, err = parseTime(q.Get("to")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'to' deve ser RFC3339"))
		return
	}
	f.StatusMin = parseIntPtr(q.Get("statusMin"))
	f.StatusMax = parseIntPtr(q.Get("statusMax"))
	f.OnlyErrors = q.Get("onlyErrors") == "true"
	f.Limit = parseIntDefault(q.Get("limit"), 0)
	if f.Cursor, err = clickhouse.DecodeCursor(q.Get("cursor")); err != nil {
		h.writeError(w, r, err)
		return
	}

	page, err := h.query.Events(r.Context(), f)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// GetEvent retorna um evento por id, com payloads completos.
func (h *Handlers) GetEvent(w http.ResponseWriter, r *http.Request) {
	event, err := h.query.Event(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, event)
}

// GetSessions lista sessões.
func (h *Handlers) GetSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := domain.SessionFilter{
		App:        q.Get("app"),
		AppScope:   scopeFrom(r),
		UserID:     q.Get("userId"),
		OnlyErrors: q.Get("onlyErrors") == "true",
		Limit:      parseIntDefault(q.Get("limit"), 0),
		Offset:     parseIntDefault(q.Get("offset"), 0),
	}
	var err error
	if f.From, err = parseTime(q.Get("from")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'from' deve ser RFC3339"))
		return
	}
	if f.To, err = parseTime(q.Get("to")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'to' deve ser RFC3339"))
		return
	}

	page, err := h.query.Sessions(r.Context(), f)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// GetSession retorna a sessão + rastro cronológico completo.
func (h *Handlers) GetSession(w http.ResponseWriter, r *http.Request) {
	detail, err := h.query.Session(r.Context(), chi.URLParam(r, "sessionId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// GetTrace devolve o rastro cronológico de todos os eventos com o mesmo
// W3C trace_id — frontend + backend (se ambos instrumentados) em uma
// única timeline correlacionada.
func (h *Handlers) GetTrace(w http.ResponseWriter, r *http.Request) {
	events, err := h.query.TraceTimeline(r.Context(), chi.URLParam(r, "traceId"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"traceId": chi.URLParam(r, "traceId"),
		"events":  events,
	})
}

// GetOverview agrega métricas da janela de tempo.
func (h *Handlers) GetOverview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := parseTime(q.Get("from"))
	if err != nil {
		h.writeError(w, r, domain.NewValidationError("'from' deve ser RFC3339"))
		return
	}
	to, err := parseTime(q.Get("to"))
	if err != nil {
		h.writeError(w, r, domain.NewValidationError("'to' deve ser RFC3339"))
		return
	}
	overview, err := h.query.Overview(r.Context(), derefTime(from), derefTime(to), q.Get("app"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

// GetFilterOptions devolve valores distintos para os dropdowns.
func (h *Handlers) GetFilterOptions(w http.ResponseWriter, r *http.Request) {
	opts, err := h.query.FilterOptions(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

// Health é o probe de liveness/readiness.
func (h *Handlers) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "ndovu-api"})
}

// ---------------------------------------------------------------------------
// Issues (erros agrupados por fingerprint) e Web Vitals
// ---------------------------------------------------------------------------

// GetIssues lista erros agrupados por fingerprint em uma janela.
// Filtros extras: onlyOpen=true, assignee=me (usa a identidade do token).
func (h *Handlers) GetIssues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := domain.IssueFilter{
		App:      q.Get("app"),
		AppScope: scopeFrom(r),
		Release:  q.Get("release"),
		Limit:    parseIntDefault(q.Get("limit"), 50),
	}
	var err error
	if f.From, err = parseTime(q.Get("from")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'from' deve ser RFC3339"))
		return
	}
	if f.To, err = parseTime(q.Get("to")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'to' deve ser RFC3339"))
		return
	}

	in := usecase.ListInput{Filter: f, OnlyOpen: q.Get("onlyOpen") == "true"}
	if q.Get("assignee") == "me" {
		if id, ok := IdentityFrom(r.Context()); ok {
			in.AssigneeID = id.UserID
		}
	}
	issues, err := h.issues.List(r.Context(), in)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": issues})
}

type patchIssueRequest struct {
	Status         string `json:"status"`
	Assignee       string `json:"assignee"`       // texto legado (mantido)
	AssigneeUserID string `json:"assigneeUserId"` // FK preferido; "" desatribui
	App            string `json:"app"`
}

// PatchIssue altera o status e o assignee de uma issue.
func (h *Handlers) PatchIssue(w http.ResponseWriter, r *http.Request) {
	fingerprint := chi.URLParam(r, "fingerprint")
	var req patchIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	err := h.issues.SetStatus(r.Context(), domain.IssueStatusInput{
		Fingerprint:    fingerprint,
		App:            req.App,
		Status:         domain.IssueStatus(req.Status),
		Assignee:       req.Assignee,
		AssigneeUserID: req.AssigneeUserID,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "issue.update", "issue", fingerprint, map[string]any{
		"status": req.Status, "assigneeUserId": req.AssigneeUserID,
	})
	writeJSON(w, http.StatusOK, map[string]any{"fingerprint": fingerprint, "status": req.Status})
}

type postCommentRequest struct {
	Body string `json:"body"`
}

// GetIssueComments lista os comentários de um issue.
func (h *Handlers) GetIssueComments(w http.ResponseWriter, r *http.Request) {
	fingerprint := chi.URLParam(r, "fingerprint")
	list, err := h.issues.ListComments(r.Context(), fingerprint)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": list})
}

// PostIssueComment adiciona um comentário à issue com o autor = identidade JWT.
func (h *Handlers) PostIssueComment(w http.ResponseWriter, r *http.Request) {
	fingerprint := chi.URLParam(r, "fingerprint")
	var req postCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	identity, _ := IdentityFrom(r.Context())
	created, err := h.issues.AddComment(r.Context(), fingerprint, identity.UserID, req.Body)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// DeleteIssueComment remove um comentário — só o próprio autor pode.
func (h *Handlers) DeleteIssueComment(w http.ResponseWriter, r *http.Request) {
	identity, _ := IdentityFrom(r.Context())
	if err := h.issues.DeleteComment(r.Context(), chi.URLParam(r, "id"), identity.UserID); err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// GetReleases lista as versões ativas do app com contadores.
func (h *Handlers) GetReleases(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := domain.ReleaseFilter{
		App:      q.Get("app"),
		AppScope: scopeFrom(r),
		Limit:    parseIntDefault(q.Get("limit"), 50),
	}
	var err error
	if f.From, err = parseTime(q.Get("from")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'from' deve ser RFC3339"))
		return
	}
	if f.To, err = parseTime(q.Get("to")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'to' deve ser RFC3339"))
		return
	}
	list, err := h.releases.List(r.Context(), f)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": list})
}

// GetReleaseCompare devolve o delta entre duas releases da mesma app na janela.
func (h *Handlers) GetReleaseCompare(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := parseTime(q.Get("from"))
	if err != nil {
		h.writeError(w, r, domain.NewValidationError("'from' deve ser RFC3339"))
		return
	}
	to, err := parseTime(q.Get("to"))
	if err != nil {
		h.writeError(w, r, domain.NewValidationError("'to' deve ser RFC3339"))
		return
	}
	cmp, err := h.releases.Compare(r.Context(), q.Get("app"), q.Get("releaseA"), q.Get("releaseB"),
		derefTime(from), derefTime(to))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cmp)
}

// GetWebVitals agrega LCP/CLS/INP/... por rota com p75 e p95.
func (h *Handlers) GetWebVitals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := domain.WebVitalFilter{
		App:      q.Get("app"),
		AppScope: scopeFrom(r),
		Vital:    q.Get("vital"),
		Limit:    parseIntDefault(q.Get("limit"), 100),
	}
	var err error
	if f.From, err = parseTime(q.Get("from")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'from' deve ser RFC3339"))
		return
	}
	if f.To, err = parseTime(q.Get("to")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'to' deve ser RFC3339"))
		return
	}
	stats, err := h.query.WebVitals(r.Context(), f)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vitals": stats})
}

// ---------------------------------------------------------------------------
// Funis de conversão (Sprint C.2)
// ---------------------------------------------------------------------------

type funnelRequest struct {
	App           string              `json:"app"`
	Name          string              `json:"name"`
	WindowSeconds int                 `json:"windowSeconds"`
	Steps         []domain.FunnelStep `json:"steps"`
}

// GetFunnels lista os funis, filtro opcional por ?app=.
func (h *Handlers) GetFunnels(w http.ResponseWriter, r *http.Request) {
	list, err := h.funnels.List(r.Context(), r.URL.Query().Get("app"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"funnels": list})
}

// PostFunnel cria um funil.
func (h *Handlers) PostFunnel(w http.ResponseWriter, r *http.Request) {
	var req funnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	identity, _ := IdentityFrom(r.Context())
	created, err := h.funnels.Create(r.Context(), usecase.CreateFunnelInput{
		App: req.App, Name: req.Name, WindowSeconds: req.WindowSeconds,
		Steps: req.Steps, CreatedBy: identity.UserID,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// PatchFunnel edita um funil.
func (h *Handlers) PatchFunnel(w http.ResponseWriter, r *http.Request) {
	var req funnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	f, err := h.funnels.Update(r.Context(), chi.URLParam(r, "id"), usecase.UpdateFunnelInput{
		Name: req.Name, WindowSeconds: req.WindowSeconds, Steps: req.Steps,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// DeleteFunnel remove por id.
func (h *Handlers) DeleteFunnel(w http.ResponseWriter, r *http.Request) {
	if err := h.funnels.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// GetFunnelResults executa o funil na janela pedida e devolve os steps
// com contagem, taxa acumulada e conversão step-a-step.
func (h *Handlers) GetFunnelResults(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := parseTime(q.Get("from"))
	if err != nil {
		h.writeError(w, r, domain.NewValidationError("'from' deve ser RFC3339"))
		return
	}
	to, err := parseTime(q.Get("to"))
	if err != nil {
		h.writeError(w, r, domain.NewValidationError("'to' deve ser RFC3339"))
		return
	}
	res, err := h.funnels.Run(r.Context(), chi.URLParam(r, "id"), from, to)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GetRetention monta a matriz cohort × Dn.
func (h *Handlers) GetRetention(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := domain.RetentionFilter{App: q.Get("app"), CohortBy: q.Get("cohortBy")}
	var err error
	if f.From, err = parseTime(q.Get("from")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'from' deve ser RFC3339"))
		return
	}
	if f.To, err = parseTime(q.Get("to")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'to' deve ser RFC3339"))
		return
	}
	res, err := h.retention.Query(r.Context(), f)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ---------------------------------------------------------------------------
// Saved views (Sprint C.1)
// ---------------------------------------------------------------------------

type savedViewRequest struct {
	Name     string          `json:"name"`
	ViewType string          `json:"viewType"`
	Filters  json.RawMessage `json:"filters"`
	IsShared bool            `json:"isShared"`
}

// GetSavedViews lista as views visíveis para o usuário atual (minhas + compartilhadas).
// Filtro opcional por ?viewType=traces.
func (h *Handlers) GetSavedViews(w http.ResponseWriter, r *http.Request) {
	identity, _ := IdentityFrom(r.Context())
	list, err := h.savedViews.List(r.Context(), identity.UserID, r.URL.Query().Get("viewType"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"views": list})
}

// PostSavedView cria uma view associada ao usuário do token.
func (h *Handlers) PostSavedView(w http.ResponseWriter, r *http.Request) {
	var req savedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	identity, _ := IdentityFrom(r.Context())
	created, err := h.savedViews.Create(r.Context(), usecase.CreateSavedViewInput{
		OwnerUserID: identity.UserID,
		OwnerRole:   identity.Role,
		ViewType:    req.ViewType,
		Name:        req.Name,
		Filters:     req.Filters,
		IsShared:    req.IsShared,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// PatchSavedView edita nome/filters/isShared. Só o dono pode.
func (h *Handlers) PatchSavedView(w http.ResponseWriter, r *http.Request) {
	var req savedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	identity, _ := IdentityFrom(r.Context())
	v, err := h.savedViews.Update(r.Context(), chi.URLParam(r, "id"), identity.UserID,
		usecase.UpdateSavedViewInput{Name: req.Name, Filters: req.Filters, IsShared: req.IsShared, OwnerRole: identity.Role})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// DeleteSavedView só o dono pode.
func (h *Handlers) DeleteSavedView(w http.ResponseWriter, r *http.Request) {
	identity, _ := IdentityFrom(r.Context())
	if err := h.savedViews.Delete(r.Context(), chi.URLParam(r, "id"), identity.UserID); err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ---------------------------------------------------------------------------
// User feedback widget (Fase 4 sprint J)
// ---------------------------------------------------------------------------

type feedbackIngestRequest struct {
	App       string `json:"app"`
	SessionID string `json:"sessionId"`
	EventID   string `json:"eventId"`
	UserID    string `json:"userId"`
	Type      string `json:"type"`
	Message   string `json:"message"`
	Email     string `json:"email"`
	URL       string `json:"url"`
	ViewportW int    `json:"viewportW"`
	ViewportH int    `json:"viewportH"`
}

// PostFeedback recebe do widget SDK (auth = X-Api-Key, mesma do ingest).
// Valida app contra o app dono da key (cross-tenant guard).
func (h *Handlers) PostFeedback(w http.ResponseWriter, r *http.Request) {
	var req feedbackIngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido: " + err.Error()})
		return
	}
	if key, ok := APIKeyFrom(r.Context()); ok && req.App != key.App {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "app do feedback não corresponde ao app da chave",
		})
		return
	}
	fb, err := h.feedback.Ingest(r.Context(), usecase.FeedbackInput{
		App: req.App, SessionID: req.SessionID, EventID: req.EventID,
		UserID: req.UserID, Type: domain.FeedbackType(req.Type),
		Message: req.Message, Email: req.Email, URL: req.URL,
		ViewportW: req.ViewportW, ViewportH: req.ViewportH,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, fb)
}

// GetFeedbacks lista os feedbacks paginados (admin) — filtrado por scope.
func (h *Handlers) GetFeedbacks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, total, err := h.feedback.List(r.Context(), domain.FeedbackFilter{
		App:    q.Get("app"),
		Status: q.Get("status"),
		Limit:  parseIntDefault(q.Get("limit"), 50),
		Offset: parseIntDefault(q.Get("offset"), 0),
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if scope, _ := AppScopeFrom(r.Context()); scope != nil {
		list = filterByAppScope(list, scope, func(f domain.UserFeedback) string { return f.App })
		total = len(list)
	}
	writeJSON(w, http.StatusOK, map[string]any{"feedbacks": list, "total": total})
}

type patchFeedbackRequest struct {
	Status string `json:"status"`
}

// PatchFeedback altera o status — bloqueia se app está fora do scope.
func (h *Handlers) PatchFeedback(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cur, err := h.feedback.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceAppInScope(w, r, cur.App) {
		return
	}
	var req patchFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	identity, _ := IdentityFrom(r.Context())
	fb, err := h.feedback.SetStatus(r.Context(), id, domain.FeedbackStatus(req.Status), identity.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "feedback.update", "feedback", id, map[string]any{"status": req.Status})
	writeJSON(w, http.StatusOK, fb)
}

// DeleteFeedback remove um feedback (útil para spam) — bloqueia cross-app.
func (h *Handlers) DeleteFeedback(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cur, err := h.feedback.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceAppInScope(w, r, cur.App) {
		return
	}
	if err := h.feedback.Delete(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "feedback.delete", "feedback", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ---------------------------------------------------------------------------
// Detecção de anomalia (Fase 4 sprint I)
// ---------------------------------------------------------------------------

type anomalyRuleRequest struct {
	Name           string  `json:"name"`
	App            string  `json:"app"`
	Metric         string  `json:"metric"`
	WindowMinutes  int     `json:"windowMinutes"`
	BaselineWeeks  int     `json:"baselineWeeks"`
	Sensitivity    float64 `json:"sensitivity"`
	Direction      string  `json:"direction"`
	SilenceSeconds int     `json:"silenceSeconds"`
	Channel        string  `json:"channel"`
	TargetURL      string  `json:"targetUrl"`
	Active         bool    `json:"active"`
}

func (req anomalyRuleRequest) toInput() usecase.AnomalyInput {
	return usecase.AnomalyInput{
		Name: req.Name, App: req.App,
		Metric: domain.AnomalyMetric(req.Metric),
		WindowMinutes: req.WindowMinutes, BaselineWeeks: req.BaselineWeeks,
		Sensitivity: req.Sensitivity,
		Direction:   domain.AnomalyDirection(req.Direction),
		SilenceSeconds: req.SilenceSeconds,
		Channel:   domain.AlertChannel(req.Channel),
		TargetURL: req.TargetURL, Active: req.Active,
	}
}

func (h *Handlers) GetAnomalyRules(w http.ResponseWriter, r *http.Request) {
	list, err := h.anomalies.List(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	scope, _ := AppScopeFrom(r.Context())
	list = filterByAppScope(list, scope, func(a domain.AnomalyRule) string { return a.App })
	writeJSON(w, http.StatusOK, map[string]any{"rules": list})
}

func (h *Handlers) PostAnomalyRule(w http.ResponseWriter, r *http.Request) {
	var req anomalyRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	if h.enforceAppInScope(w, r, req.App) {
		return
	}
	created, err := h.anomalies.Create(r.Context(), req.toInput())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "anomaly.create", "anomaly_rule", created.ID, req)
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handlers) PatchAnomalyRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cur, err := h.anomalies.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceAppInScope(w, r, cur.App) {
		return
	}
	var req anomalyRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	// Não pode mover a regra pra outro app fora do scope.
	if h.enforceAppInScope(w, r, req.App) {
		return
	}
	updated, err := h.anomalies.Update(r.Context(), id, req.toInput())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "anomaly.update", "anomaly_rule", id, req)
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handlers) DeleteAnomalyRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cur, err := h.anomalies.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceAppInScope(w, r, cur.App) {
		return
	}
	if err := h.anomalies.Delete(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "anomaly.delete", "anomaly_rule", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// GetAnomalyDetections lista histórico — pra filtrar por company, carrega
// todas as regras uma vez e joga fora detections cujo rule_id pertence a
// app fora do scope. Não é lookup por request (regras ficam em memória).
func (h *Handlers) GetAnomalyDetections(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, total, err := h.anomalies.Detections(r.Context(),
		parseIntDefault(q.Get("limit"), 50),
		parseIntDefault(q.Get("offset"), 0))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if scope, _ := AppScopeFrom(r.Context()); scope != nil {
		rules, _ := h.anomalies.List(r.Context())
		ruleApp := make(map[string]string, len(rules))
		for _, ru := range rules {
			ruleApp[ru.ID] = ru.App
		}
		list = filterByAppScope(list, scope, func(d domain.AnomalyDetection) string { return ruleApp[d.RuleID] })
		total = len(list)
	}
	writeJSON(w, http.StatusOK, map[string]any{"detections": list, "total": total})
}

// ---------------------------------------------------------------------------
// Session snapshots (Sprint H — session replay MVP)
// ---------------------------------------------------------------------------

type snapshotRequest struct {
	EventID   string    `json:"eventId"`
	SessionID string    `json:"sessionId"`
	App       string    `json:"app"`
	HTML      string    `json:"html"`
	URL       string    `json:"url"`
	ViewportW int       `json:"viewportW"`
	ViewportH int       `json:"viewportH"`
	TakenAt   time.Time `json:"takenAt"`
}

// PostSnapshot recebe o payload do SDK (mesma X-Api-Key da ingestão de eventos).
// Valida app contra o app dono da chave — evita cross-tenant.
func (h *Handlers) PostSnapshot(w http.ResponseWriter, r *http.Request) {
	if h.snapshots == nil || !h.snapshots.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable,
			errorResponse{Error: "snapshots desligados (blob storage indisponível)"})
		return
	}
	var req snapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido: " + err.Error()})
		return
	}
	if key, ok := APIKeyFrom(r.Context()); ok && req.App != key.App {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "app do snapshot não corresponde ao app da chave",
		})
		return
	}
	meta, err := h.snapshots.Save(r.Context(), usecase.SnapshotInput{
		EventID:   req.EventID,
		SessionID: req.SessionID,
		App:       req.App,
		HTML:      req.HTML,
		URL:       req.URL,
		ViewportW: req.ViewportW,
		ViewportH: req.ViewportH,
		TakenAt:   req.TakenAt,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, meta)
}

// GetSnapshot devolve o HTML do snapshot (descomprimido) para o browser
// renderizar dentro de um iframe sandboxed. Content-Type text/html.
func (h *Handlers) GetSnapshot(w http.ResponseWriter, r *http.Request) {
	if h.snapshots == nil || !h.snapshots.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable,
			errorResponse{Error: "snapshots desligados"})
		return
	}
	eventID := chi.URLParam(r, "eventId")
	_, html, err := h.snapshots.Get(r.Context(), eventID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	// Content-Security-Policy defensivo: iframe do dashboard vai renderizar
	// isso em sandbox; ainda assim proibimos script inline como camada extra.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src * data:; style-src 'unsafe-inline' *; font-src *")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(html)
}

// GetSnapshotMeta devolve só a metadata (o drill do dashboard usa antes de
// decidir se mostra o iframe). 404 se não existe snapshot pro evento.
func (h *Handlers) GetSnapshotMeta(w http.ResponseWriter, r *http.Request) {
	if h.snapshots == nil || !h.snapshots.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable,
			errorResponse{Error: "snapshots desligados"})
		return
	}
	eventID := chi.URLParam(r, "eventId")
	meta, _, err := h.snapshots.Get(r.Context(), eventID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// ---------------------------------------------------------------------------
// Sampling adaptativo (Sprint G)
// ---------------------------------------------------------------------------

type samplingRuleRequest struct {
	App        string  `json:"app"`
	EventType  string  `json:"eventType"`
	SampleRate float64 `json:"sampleRate"`
	KeepErrors bool    `json:"keepErrors"`
	Active     bool    `json:"active"`
	Note       string  `json:"note"`
}

func (h *Handlers) GetSamplingRules(w http.ResponseWriter, r *http.Request) {
	list, err := h.sampling.List(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	scope, _ := AppScopeFrom(r.Context())
	list = filterByAppScope(list, scope, func(s domain.SamplingRule) string { return s.App })
	writeJSON(w, http.StatusOK, map[string]any{"rules": list})
}

func (h *Handlers) PostSamplingRule(w http.ResponseWriter, r *http.Request) {
	var req samplingRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	if h.enforceAppInScope(w, r, req.App) {
		return
	}
	created, err := h.sampling.Create(r.Context(), usecase.SamplingInput{
		App: req.App, EventType: req.EventType, SampleRate: req.SampleRate,
		KeepErrors: req.KeepErrors, Active: req.Active, Note: req.Note,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "sampling.create", "sampling_rule", created.ID, req)
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handlers) PatchSamplingRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cur, err := h.sampling.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceAppInScope(w, r, cur.App) {
		return
	}
	var req samplingRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	if h.enforceAppInScope(w, r, req.App) {
		return
	}
	updated, err := h.sampling.Update(r.Context(), id, usecase.SamplingInput{
		App: req.App, EventType: req.EventType, SampleRate: req.SampleRate,
		KeepErrors: req.KeepErrors, Active: req.Active, Note: req.Note,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "sampling.update", "sampling_rule", id, req)
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handlers) DeleteSamplingRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cur, err := h.sampling.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceAppInScope(w, r, cur.App) {
		return
	}
	if err := h.sampling.Delete(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "sampling.delete", "sampling_rule", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ---------------------------------------------------------------------------
// RBAC granular por app (Sprint E.4)
// ---------------------------------------------------------------------------

type grantPermissionRequest struct {
	Role string `json:"role"` // "viewer" (default) | "editor"
}

// GetUserPermissions lista as permissões explícitas de um user.
// Nota: admin de company não precisa de permissão explícita — vê tudo da
// própria company. Esta rota é útil principalmente para viewers.
func (h *Handlers) GetUserPermissions(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	// Bloqueia se o user alvo não é da company do actor.
	target, err := h.auth.GetUserByID(r.Context(), userID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceOwnership(w, r, target.CompanyID) {
		return
	}
	list, err := h.permissions.ListForUser(r.Context(), userID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": list})
}

// PutUserPermission concede acesso do user ao app (idempotente).
// Ownership: user alvo E app alvo devem ser da company do actor.
func (h *Handlers) PutUserPermission(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	appID := chi.URLParam(r, "appId")
	target, err := h.auth.GetUserByID(r.Context(), userID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceOwnership(w, r, target.CompanyID) {
		return
	}
	app, err := h.apps.Get(r.Context(), appID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceOwnership(w, r, app.CompanyID) {
		return
	}
	var req grantPermissionRequest
	// body é opcional — se vazio, cai em "viewer" no service.
	_ = json.NewDecoder(r.Body).Decode(&req)
	identity, _ := IdentityFrom(r.Context())
	p, err := h.permissions.Grant(r.Context(), userID, appID, req.Role, identity.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "permission.grant", "user_app", userID+"/"+appID,
		map[string]any{"role": p.Role})
	writeJSON(w, http.StatusOK, p)
}

// DeleteUserPermission revoga o acesso — mesmo ownership check do PUT.
func (h *Handlers) DeleteUserPermission(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	appID := chi.URLParam(r, "appId")
	target, err := h.auth.GetUserByID(r.Context(), userID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceOwnership(w, r, target.CompanyID) {
		return
	}
	if err := h.permissions.Revoke(r.Context(), userID, appID); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "permission.revoke", "user_app", userID+"/"+appID, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

// ---------------------------------------------------------------------------
// LGPD/GDPR (admin) — portabilidade e direito ao esquecimento
// ---------------------------------------------------------------------------

// GetGDPRExport devolve todos os eventos de um user como download JSON.
// Content-Disposition attachment para o browser salvar como arquivo.
// LIMITAÇÃO ATUAL: só super-admin pode operar. O userId pode ter eventos em
// apps de múltiplas companies e o método atual não filtra por scope — para
// evitar vazamento cross-tenant, admin não-super recebe 403. Evolução:
// passar AppScope para o GDPRService e filtrar WHERE user_id AND app IN (scope).
func (h *Handlers) GetGDPRExport(w http.ResponseWriter, r *http.Request) {
	identity, _ := IdentityFrom(r.Context())
	if !identity.IsSuper {
		writeJSON(w, http.StatusForbidden, errorResponse{
			Error: "operação LGPD requer super-admin — admin de company não pode operar em user_id que pode existir em múltiplas empresas",
		})
		return
	}
	userID := chi.URLParam(r, "userId")
	events, err := h.gdpr.Export(r.Context(), userID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "gdpr.export", "user_data", userID, map[string]any{"eventCount": len(events)})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="ndovu-export-%s.json"`, userID))
	_ = json.NewEncoder(w).Encode(map[string]any{
		"userId":     userID,
		"eventCount": len(events),
		"exportedAt": time.Now().UTC(),
		"events":     events,
	})
}

// DeleteGDPRUser dispara o "esquecer usuário" (async no ClickHouse).
// Retorna 202 Accepted — o registro do pedido fica no audit log.
// Ver LIMITAÇÃO em GetGDPRExport: só super-admin por enquanto.
func (h *Handlers) DeleteGDPRUser(w http.ResponseWriter, r *http.Request) {
	identity, _ := IdentityFrom(r.Context())
	if !identity.IsSuper {
		writeJSON(w, http.StatusForbidden, errorResponse{
			Error: "operação LGPD requer super-admin — admin de company não pode operar em user_id que pode existir em múltiplas empresas",
		})
		return
	}
	userID := chi.URLParam(r, "userId")
	if err := h.gdpr.Forget(r.Context(), userID); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "gdpr.forget", "user_data", userID, nil)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"userId":  userID,
		"status":  "delete dispatched",
		"note":    "ClickHouse aplica async; consultas podem ver os dados por até alguns minutos",
	})
}

// ---------------------------------------------------------------------------
// Audit log (admin)
// ---------------------------------------------------------------------------

// GetAuditLog lista as entradas de audit paginadas — filtra por company do
// actor quando não-super (evita vazamento cross-tenant de auditoria).
func (h *Handlers) GetAuditLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	identity, _ := IdentityFrom(r.Context())
	companyFilter := ""
	if !identity.IsSuper {
		companyFilter = identity.CompanyID
	}
	f := domain.AuditFilter{
		Actor:        q.Get("actor"),
		Action:       q.Get("action"),
		ResourceType: q.Get("resourceType"),
		CompanyID:    companyFilter,
		Limit:        parseIntDefault(q.Get("limit"), 50),
		Offset:       parseIntDefault(q.Get("offset"), 0),
	}
	var err error
	if f.From, err = parseTime(q.Get("from")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'from' deve ser RFC3339"))
		return
	}
	if f.To, err = parseTime(q.Get("to")); err != nil {
		h.writeError(w, r, domain.NewValidationError("'to' deve ser RFC3339"))
		return
	}
	entries, total, err := h.audit.List(r.Context(), f)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "total": total})
}

// ---------------------------------------------------------------------------
// Digest semanal (admin)
// ---------------------------------------------------------------------------

// PostDigestSendNow dispara o digest imediatamente (bypass do agendamento).
// Útil para preview durante desenvolvimento. Só admin, e o serviço pode
// não estar configurado (retorna 400 nesse caso).
func (h *Handlers) PostDigestSendNow(w http.ResponseWriter, r *http.Request) {
	if h.digest == nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "digest não configurado (SMTP/recipients)"})
		return
	}
	if err := h.digest.SendOnce(r.Context()); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "digest.send_now", "digest", "", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}

// ---------------------------------------------------------------------------
// Source maps (admin) + resolução de stack
// ---------------------------------------------------------------------------

type uploadSourceMapRequest struct {
	App      string `json:"app"`
	Release  string `json:"release"`
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// GetSourceMaps lista os source maps (metadata) — filtrado por scope.
func (h *Handlers) GetSourceMaps(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := h.sourceMaps.List(r.Context(), q.Get("app"), q.Get("release"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	scope, _ := AppScopeFrom(r.Context())
	list = filterByAppScope(list, scope, func(m domain.SourceMap) string { return m.App })
	writeJSON(w, http.StatusOK, map[string]any{"sourceMaps": list})
}

// PostSourceMaps recebe upload como JSON simples ({"app","release","filename","content"}).
// Content é o texto do .map (JSON); reenvio sobrescreve por (app,release,filename).
// Bloqueia upload em app fora do scope do actor.
func (h *Handlers) PostSourceMaps(w http.ResponseWriter, r *http.Request) {
	var req uploadSourceMapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	if h.enforceAppInScope(w, r, req.App) {
		return
	}
	identity, _ := IdentityFrom(r.Context())
	m, err := h.sourceMaps.Upload(r.Context(), usecase.UploadInput{
		App:        req.App,
		Release:    req.Release,
		Filename:   req.Filename,
		Content:    req.Content,
		UploadedBy: identity.UserID,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "sourcemap.upload", "sourcemap", m.ID, map[string]any{
		"app": m.App, "release": m.Release, "filename": m.Filename, "sizeBytes": m.SizeBytes,
	})
	writeJSON(w, http.StatusCreated, m)
}

// DeleteSourceMap remove um source map por id — bloqueia se app não é do actor.
func (h *Handlers) DeleteSourceMap(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cur, err := h.sourceMaps.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceAppInScope(w, r, cur.App) {
		return
	}
	if err := h.sourceMaps.Delete(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "sourcemap.delete", "sourcemap", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// GetResolvedStack pega um evento pelo id, extrai o stack (error.body.stack)
// e devolve os frames resolvidos via source maps daquela release.
func (h *Handlers) GetResolvedStack(w http.ResponseWriter, r *http.Request) {
	event, err := h.query.Event(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	stack := extractStack(event)
	if stack == "" {
		writeJSON(w, http.StatusOK, map[string]any{"frames": []any{}, "raw": ""})
		return
	}
	frames := h.sourceMaps.Resolve(r.Context(), event.App, event.Release, stack)
	writeJSON(w, http.StatusOK, map[string]any{
		"frames":  frames,
		"raw":     stack,
		"release": event.Release,
	})
}

// extractStack tenta pegar a string do stack em error.body.stack (formato
// que o SDK browser envia). Retorna "" se não achar.
func extractStack(e domain.TraceEvent) string {
	if e.Error == nil || len(e.Error.Body) == 0 {
		return ""
	}
	var body map[string]any
	if err := json.Unmarshal(e.Error.Body, &body); err != nil {
		return ""
	}
	if s, ok := body["stack"].(string); ok {
		return s
	}
	return ""
}

// ---------------------------------------------------------------------------
// Alertas (admin)
// ---------------------------------------------------------------------------

type createAlertRequest struct {
	Name           string `json:"name"`
	App            string `json:"app"`
	ErrorCode      string `json:"errorCode"`
	Threshold      int    `json:"threshold"`
	WindowSeconds  int    `json:"windowSeconds"`
	Channel        string `json:"channel"`
	TargetURL      string `json:"targetUrl"`
	SilenceSeconds int    `json:"silenceSeconds"`
	Active         bool   `json:"active"`
}

// GetAlerts lista as regras de alerta — filtrado por scope de apps do actor.
func (h *Handlers) GetAlerts(w http.ResponseWriter, r *http.Request) {
	rules, err := h.alerts.List(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	scope, _ := AppScopeFrom(r.Context())
	rules = filterByAppScope(rules, scope, func(a domain.AlertRule) string { return a.App })
	writeJSON(w, http.StatusOK, map[string]any{"alerts": rules})
}

// PostAlerts cria uma regra de alerta — valida que o app é do actor.
func (h *Handlers) PostAlerts(w http.ResponseWriter, r *http.Request) {
	var req createAlertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	if h.enforceAppInScope(w, r, req.App) {
		return
	}
	rule := domain.AlertRule{
		Name:           req.Name,
		App:            req.App,
		ErrorCode:      req.ErrorCode,
		Threshold:      req.Threshold,
		WindowSeconds:  req.WindowSeconds,
		Channel:        domain.AlertChannel(req.Channel),
		TargetURL:      req.TargetURL,
		SilenceSeconds: req.SilenceSeconds,
		Active:         req.Active,
	}
	created, err := h.alerts.Create(r.Context(), rule)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "alert.create", "alert", created.ID, map[string]any{
		"name": created.Name, "channel": created.Channel,
	})
	writeJSON(w, http.StatusCreated, created)
}

// DeleteAlert remove uma regra — bloqueia se o app é de outra company.
func (h *Handlers) DeleteAlert(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cur, err := h.alerts.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceAppInScope(w, r, cur.App) {
		return
	}
	if err := h.alerts.Delete(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "alert.delete", "alert", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeError traduz erros de domínio para HTTP em um único lugar.
func (h *Handlers) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var vErr *domain.ValidationError
	switch {
	case errors.As(err, &vErr):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "violação de contrato", Details: vErr.Issues})
	case errors.Is(err, domain.ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "credenciais inválidas"})
	case errors.Is(err, domain.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "permissão insuficiente"})
	case errors.Is(err, domain.ErrConflict):
		writeJSON(w, http.StatusConflict, errorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "recurso não encontrado"})
	default:
		h.logger.ErrorContext(r.Context(), "erro interno", "err", err, "path", r.URL.Path)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "erro interno"})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func parseTime(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	t = t.UTC()
	return &t, nil
}

func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func derefTime(t *time.Time) time.Time { return deref(t) }

func parseIntPtr(s string) *int {
	if s == "" {
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return &n
	}
	return nil
}

func parseIntDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// parseTraceparent decompõe o header W3C:
//
//	traceparent: 00-<32hex traceId>-<16hex spanId>-<2hex flags>
//
// Retorna nil se malformado — não é erro fatal, só ignoramos.
func parseTraceparent(h string) *domain.TraceContext {
	if h == "" {
		return nil
	}
	parts := strings.Split(h, "-")
	if len(parts) != 4 {
		return nil
	}
	if len(parts[0]) != 2 || len(parts[1]) != 32 || len(parts[2]) != 16 {
		return nil
	}
	return &domain.TraceContext{TraceID: parts[1], SpanID: parts[2]}
}
