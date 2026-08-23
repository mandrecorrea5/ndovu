package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/usecase"
)

// enforceOwnership rejeita a request se o actor não é super-admin e a
// company do recurso alvo é diferente da company do actor. Usado nos
// handlers admin para bloquear cross-tenant access. Retorna true se
// respondeu (a request deve parar); false se pode continuar.
func (h *Handlers) enforceOwnership(w http.ResponseWriter, r *http.Request, resourceCompanyID string) bool {
	identity, _ := IdentityFrom(r.Context())
	if identity.IsSuper {
		return false
	}
	if identity.CompanyID == "" || resourceCompanyID == "" || identity.CompanyID != resourceCompanyID {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "recurso pertence a outra empresa"})
		return true
	}
	return false
}

// filterByAppScope devolve apenas os itens cujo App está no scope injetado
// pelo tenantScope (nil = super-admin, passa tudo). Vazio = string do app
// não confere; app "" (regra global) é aceita apenas por super.
// Usado nas listagens admin do bloco estendido (alerts, sampling, anomaly,
// source-maps, feedbacks) — as tabelas cabem em memória.
func filterByAppScope[T any](items []T, scope []string, getApp func(T) string) []T {
	if scope == nil {
		return items
	}
	allowed := make(map[string]bool, len(scope))
	for _, a := range scope {
		allowed[a] = true
	}
	out := make([]T, 0, len(items))
	for _, it := range items {
		if allowed[getApp(it)] {
			out = append(out, it)
		}
	}
	return out
}

// enforceAppInScope rejeita a request se o `app` do recurso não está na
// whitelist do actor (super passa livre). Usado em POST/PATCH/DELETE do
// bloco admin estendido (alerts, sampling, anomaly, source-maps).
func (h *Handlers) enforceAppInScope(w http.ResponseWriter, r *http.Request, app string) bool {
	identity, _ := IdentityFrom(r.Context())
	if identity.IsSuper {
		return false
	}
	scope, _ := AppScopeFrom(r.Context())
	// scope nil aqui não deveria acontecer para não-super, mas por segurança
	// tratamos como "nenhum app permitido".
	if scope == nil {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "sem escopo de apps"})
		return true
	}
	if app == "" {
		// Regra "global" (app vazio) só faz sentido pra super-admin.
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "regra global exige super-admin"})
		return true
	}
	for _, s := range scope {
		if s == app {
			return false
		}
	}
	writeJSON(w, http.StatusForbidden, errorResponse{Error: "app não pertence à sua empresa"})
	return true
}

// ---------------------------------------------------------------------------
// Autenticação
// ---------------------------------------------------------------------------

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// PostLogin autentica e devolve o token + usuário.
func (h *Handlers) PostLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	result, err := h.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// GetMe devolve a identidade do token apresentado.
func (h *Handlers) GetMe(w http.ResponseWriter, r *http.Request) {
	identity, ok := IdentityFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "não autenticado"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        identity.UserID,
		"email":     identity.Email,
		"name":      identity.Name,
		"role":      string(identity.Role),
		"companyId": identity.CompanyID,
		"isSuper":   identity.IsSuper,
	})
}

// ---------------------------------------------------------------------------
// Admin: usuários (a ferramenta é auto gerenciável)
// ---------------------------------------------------------------------------

type createUserRequest struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	Password  string `json:"password"`
	Role      string `json:"role"`
	CompanyID string `json:"companyId"`
}

// GetUsers lista as contas — filtrado por company do actor (super vê tudo).
func (h *Handlers) GetUsers(w http.ResponseWriter, r *http.Request) {
	identity, _ := IdentityFrom(r.Context())
	var (
		users []domain.User
		err   error
	)
	if identity.IsSuper {
		users, err = h.auth.ListUsers(r.Context())
	} else {
		users, err = h.auth.ListUsersByCompany(r.Context(), identity.CompanyID)
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// PostUsers cria uma conta.
func (h *Handlers) PostUsers(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	// Admin não-super só pode criar user na própria company — força o
	// campo mesmo que o body tenha vindo com outra company.
	identity, _ := IdentityFrom(r.Context())
	if !identity.IsSuper {
		req.CompanyID = identity.CompanyID
	}
	user, err := h.auth.CreateUser(r.Context(), usecase.CreateUserInput{
		Email:     req.Email,
		Name:      req.Name,
		Password:  req.Password,
		Role:      domain.Role(req.Role),
		CompanyID: req.CompanyID,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "user.create", "user", user.ID, map[string]any{
		"email": user.Email, "role": user.Role, "companyId": user.CompanyID,
	})
	writeJSON(w, http.StatusCreated, user)
}

type updateUserRequest struct {
	Role      *string `json:"role"`
	Active    *bool   `json:"active"`
	Name      *string `json:"name"`
	Password  *string `json:"password"`
	CompanyID *string `json:"companyId"`
}

// PatchUser altera papel/ativação/nome e, opcionalmente, redefine a senha.
func (h *Handlers) PatchUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Ownership: pega o user alvo pra checar company antes de mutar.
	target, err := h.auth.GetUserByID(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceOwnership(w, r, target.CompanyID) {
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	// Admin não-super não pode mudar user pra outra company.
	identity, _ := IdentityFrom(r.Context())
	if !identity.IsSuper && req.CompanyID != nil && *req.CompanyID != identity.CompanyID {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "não pode mover usuário para outra empresa"})
		return
	}

	in := usecase.UpdateUserInput{Active: req.Active, Name: req.Name, CompanyID: req.CompanyID}
	if req.Role != nil {
		role := domain.Role(*req.Role)
		in.Role = &role
	}
	user, err := h.auth.UpdateUser(r.Context(), id, in)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	passwordChanged := false
	if req.Password != nil {
		if err := h.auth.ResetPassword(r.Context(), id, *req.Password); err != nil {
			h.writeError(w, r, err)
			return
		}
		passwordChanged = true
	}
	h.recordAudit(r, "user.update", "user", id, map[string]any{
		"changes":         req,
		"passwordChanged": passwordChanged,
	})
	writeJSON(w, http.StatusOK, user)
}

// ---------------------------------------------------------------------------
// Admin: chaves de API (apps emissores)
// ---------------------------------------------------------------------------

type createKeyRequest struct {
	App   string `json:"app"`
	Label string `json:"label"`
}

// GetAPIKeys lista as chaves — filtrado por company do actor.
func (h *Handlers) GetAPIKeys(w http.ResponseWriter, r *http.Request) {
	identity, _ := IdentityFrom(r.Context())
	var (
		keys []domain.APIKey
		err  error
	)
	if identity.IsSuper {
		keys, err = h.keys.ListKeys(r.Context())
	} else {
		keys, err = h.keys.ListKeysByCompany(r.Context(), identity.CompanyID)
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// PostAPIKeys cria uma chave — a resposta é a ÚNICA vez que ela aparece em claro.
// Valida que o app da chave pertence à company do actor (não-super).
func (h *Handlers) PostAPIKeys(w http.ResponseWriter, r *http.Request) {
	var req createKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	// Resolve o app pelo nome pra validar ownership.
	app, err := h.apps.GetByName(r.Context(), req.App)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceOwnership(w, r, app.CompanyID) {
		return
	}
	identity, _ := IdentityFrom(r.Context())
	created, err := h.keys.CreateKey(r.Context(), "", req.App, req.Label, identity.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "apikey.create", "apikey", created.ID, map[string]any{
		"app": created.App, "prefix": created.Prefix,
	})
	writeJSON(w, http.StatusCreated, created)
}

// DeleteAPIKey revoga uma chave (efeito imediato na ingestão).
// Valida que o app da chave pertence à company do actor (não-super).
func (h *Handlers) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	key, err := h.keys.GetKeyByID(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if key.App != "" {
		app, err := h.apps.GetByName(r.Context(), key.App)
		if err == nil {
			if h.enforceOwnership(w, r, app.CompanyID) {
				return
			}
		}
	}
	if err := h.keys.RevokeKey(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "apikey.revoke", "apikey", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

// ---------------------------------------------------------------------------
// Admin: apps emissores (frontends) — CRUD + geração de chave no cadastro
// ---------------------------------------------------------------------------

type createAppRequest struct {
	Name        string `json:"name"`
	Technology  string `json:"technology"`
	Company     string `json:"company"`
	CompanyID   string `json:"companyId"`
	Responsible string `json:"responsible"`
}

type updateAppRequest struct {
	Name        *string `json:"name"`
	Technology  *string `json:"technology"`
	Company     *string `json:"company"`
	CompanyID   *string `json:"companyId"`
	Responsible *string `json:"responsible"`
}

// GetApps lista os apps — filtrado por company do actor.
func (h *Handlers) GetApps(w http.ResponseWriter, r *http.Request) {
	identity, _ := IdentityFrom(r.Context())
	var (
		apps []domain.App
		err  error
	)
	if identity.IsSuper {
		apps, err = h.apps.List(r.Context())
	} else {
		apps, err = h.apps.ListByCompany(r.Context(), identity.CompanyID)
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": apps})
}

// GetApp retorna um app por id — bloqueia se não pertence à company do actor.
func (h *Handlers) GetApp(w http.ResponseWriter, r *http.Request) {
	app, err := h.apps.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceOwnership(w, r, app.CompanyID) {
		return
	}
	writeJSON(w, http.StatusOK, app)
}

// PostApps cadastra um app e gera a chave de API — a resposta é a ÚNICA vez
// que a chave aparece em claro. Admin não-super força CompanyID = sua.
func (h *Handlers) PostApps(w http.ResponseWriter, r *http.Request) {
	var req createAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	identity, _ := IdentityFrom(r.Context())
	if !identity.IsSuper {
		req.CompanyID = identity.CompanyID
	}
	created, err := h.apps.Create(r.Context(), usecase.CreateAppInput{
		Name:        req.Name,
		Technology:  req.Technology,
		Company:     req.Company,
		CompanyID:   req.CompanyID,
		Responsible: req.Responsible,
	}, identity.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "app.create", "app", created.ID, map[string]any{
		"name": created.Name, "companyId": created.CompanyID,
	})
	writeJSON(w, http.StatusCreated, created)
}

// PatchApp altera os metadados de um app. Bloqueia cross-company.
func (h *Handlers) PatchApp(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cur, err := h.apps.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceOwnership(w, r, cur.CompanyID) {
		return
	}

	var req updateAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	// Admin não-super não move app entre companies.
	identity, _ := IdentityFrom(r.Context())
	if !identity.IsSuper && req.CompanyID != nil && *req.CompanyID != identity.CompanyID {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "não pode mover app para outra empresa"})
		return
	}
	app, err := h.apps.Update(r.Context(), id, usecase.UpdateAppInput{
		Name:        req.Name,
		Technology:  req.Technology,
		Company:     req.Company,
		CompanyID:   req.CompanyID,
		Responsible: req.Responsible,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "app.update", "app", id, req)
	writeJSON(w, http.StatusOK, app)
}

// DeleteApp remove um app e revoga todas as suas chaves. Bloqueia cross-company.
func (h *Handlers) DeleteApp(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cur, err := h.apps.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.enforceOwnership(w, r, cur.CompanyID) {
		return
	}
	if err := h.apps.Delete(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "app.delete", "app", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ---------------------------------------------------------------------------
// Admin: empresas (usuários pertencem a uma; apps podem opcionalmente)
// ---------------------------------------------------------------------------

type createCompanyRequest struct {
	Name     string `json:"name"`
	Document string `json:"document"`
	Active   bool   `json:"active"`
}

type updateCompanyRequest struct {
	Name     *string `json:"name"`
	Document *string `json:"document"`
	Active   *bool   `json:"active"`
}

// GetCompanies lista as empresas. Admin não-super só vê a própria.
func (h *Handlers) GetCompanies(w http.ResponseWriter, r *http.Request) {
	identity, _ := IdentityFrom(r.Context())
	if identity.IsSuper {
		list, err := h.companies.List(r.Context())
		if err != nil {
			h.writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"companies": list})
		return
	}
	c, err := h.companies.Get(r.Context(), identity.CompanyID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"companies": []domain.Company{c}})
}

// GetCompany retorna uma empresa por id — bloqueia se não é a do actor.
func (h *Handlers) GetCompany(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.enforceOwnership(w, r, id) {
		return
	}
	c, err := h.companies.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// PostCompanies cria uma empresa. Apenas super-admin (multi-tenant onboarding).
func (h *Handlers) PostCompanies(w http.ResponseWriter, r *http.Request) {
	identity, _ := IdentityFrom(r.Context())
	if !identity.IsSuper {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "apenas super-admin cria empresas"})
		return
	}
	var req createCompanyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	created, err := h.companies.Create(r.Context(), usecase.CreateCompanyInput{
		Name:     req.Name,
		Document: req.Document,
		Active:   req.Active,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "company.create", "company", created.ID, map[string]any{"name": created.Name})
	writeJSON(w, http.StatusCreated, created)
}

// PatchCompany altera nome/documento/ativação. Bloqueia cross-company.
func (h *Handlers) PatchCompany(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.enforceOwnership(w, r, id) {
		return
	}
	var req updateCompanyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	c, err := h.companies.Update(r.Context(), id, usecase.UpdateCompanyInput{
		Name:     req.Name,
		Document: req.Document,
		Active:   req.Active,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "company.update", "company", id, req)
	writeJSON(w, http.StatusOK, c)
}

// DeleteCompany remove uma empresa. Apenas super-admin — admin regular não
// pode apagar sua própria empresa (evita lock-out acidental).
func (h *Handlers) DeleteCompany(w http.ResponseWriter, r *http.Request) {
	identity, _ := IdentityFrom(r.Context())
	if !identity.IsSuper {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "apenas super-admin remove empresas"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.companies.Delete(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "company.delete", "company", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
