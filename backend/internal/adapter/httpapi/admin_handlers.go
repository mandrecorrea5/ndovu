package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/usecase"
)

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

// GetUsers lista as contas.
func (h *Handlers) GetUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.auth.ListUsers(r.Context())
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
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
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

// GetAPIKeys lista as chaves (sem o valor em claro).
func (h *Handlers) GetAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.keys.ListKeys(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// PostAPIKeys cria uma chave — a resposta é a ÚNICA vez que ela aparece em claro.
func (h *Handlers) PostAPIKeys(w http.ResponseWriter, r *http.Request) {
	var req createKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
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
func (h *Handlers) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
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

// GetApps lista os apps cadastrados.
func (h *Handlers) GetApps(w http.ResponseWriter, r *http.Request) {
	apps, err := h.apps.List(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": apps})
}

// GetApp retorna um app por id.
func (h *Handlers) GetApp(w http.ResponseWriter, r *http.Request) {
	app, err := h.apps.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, app)
}

// PostApps cadastra um app e gera a chave de API — a resposta é a ÚNICA vez
// que a chave aparece em claro (para copiar e enviar ao responsável).
func (h *Handlers) PostApps(w http.ResponseWriter, r *http.Request) {
	var req createAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	identity, _ := IdentityFrom(r.Context())
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

// PatchApp altera os metadados de um app.
func (h *Handlers) PatchApp(w http.ResponseWriter, r *http.Request) {
	var req updateAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	id := chi.URLParam(r, "id")
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

// DeleteApp remove um app e revoga todas as suas chaves.
func (h *Handlers) DeleteApp(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
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

// GetCompanies lista as empresas.
func (h *Handlers) GetCompanies(w http.ResponseWriter, r *http.Request) {
	list, err := h.companies.List(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"companies": list})
}

// GetCompany retorna uma empresa por id.
func (h *Handlers) GetCompany(w http.ResponseWriter, r *http.Request) {
	c, err := h.companies.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// PostCompanies cria uma empresa.
func (h *Handlers) PostCompanies(w http.ResponseWriter, r *http.Request) {
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

// PatchCompany altera nome/documento/ativação.
func (h *Handlers) PatchCompany(w http.ResponseWriter, r *http.Request) {
	var req updateCompanyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "JSON inválido"})
		return
	}
	id := chi.URLParam(r, "id")
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

// DeleteCompany remove uma empresa (falha se ainda houver usuários vinculados).
func (h *Handlers) DeleteCompany(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.companies.Delete(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.recordAudit(r, "company.delete", "company", id, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
