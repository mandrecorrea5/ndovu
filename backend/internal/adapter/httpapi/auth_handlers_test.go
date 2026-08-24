package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// -----------------------------------------------------------------------
// POST /v1/auth/login
// -----------------------------------------------------------------------

func TestAuthLogin_OK(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("admin@x", domain.RoleAdmin, "c-default", false)
	// Como tokenFor faz ResetPassword pra "test-pass-1234", usamos essa senha.
	_ = f.tokenFor(uid) // dispara reset

	rr := f.do(f.req(http.MethodPost, "/v1/auth/login", map[string]string{
		"email": "admin@x", "password": "test-pass-1234",
	}))
	requireStatus(t, rr, http.StatusOK)

	var body struct {
		Token string `json:"token"`
		User  struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
	}
	decodeJSON(t, rr, &body)
	if body.Token == "" {
		t.Error("token deveria vir preenchido")
	}
	if body.User.Email != "admin@x" || body.User.Role != "admin" {
		t.Errorf("user errado: %+v", body.User)
	}
}

func TestAuthLogin_SenhaErrada(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("u@x", domain.RoleAdmin, "c-default", false)
	_ = f.tokenFor(uid)

	rr := f.do(f.req(http.MethodPost, "/v1/auth/login", map[string]string{
		"email": "u@x", "password": "senha-errada",
	}))
	requireStatus(t, rr, http.StatusUnauthorized)
}

func TestAuthLogin_UserInativo(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("inativo@x", domain.RoleAdmin, "c-default", false)
	_ = f.tokenFor(uid)
	// Desativa o usuário direto no store.
	u, _ := f.users.GetUserByID(context.Background(), uid)
	inactive := false
	_, _ = f.users.UpdateUser(context.Background(), u.ID, nil, &inactive, nil, nil)

	rr := f.do(f.req(http.MethodPost, "/v1/auth/login", map[string]string{
		"email": "inativo@x", "password": "test-pass-1234",
	}))
	if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusForbidden {
		t.Fatalf("user inativo deveria dar 401/403, veio %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAuthLogin_UserInexistente(t *testing.T) {
	f := newFixture(t)
	rr := f.do(f.req(http.MethodPost, "/v1/auth/login", map[string]string{
		"email": "ninguem@x", "password": "qualquer",
	}))
	requireStatus(t, rr, http.StatusUnauthorized)
}

func TestAuthLogin_JsonInvalido(t *testing.T) {
	// Body malformado deve retornar 400 (não panic).
	f := newFixture(t)
	req := f.req(http.MethodPost, "/v1/auth/login", nil)
	req.Body = http.NoBody
	rr := f.do(req)
	// Sem body válido, esperamos 400.
	if rr.Code != http.StatusBadRequest && rr.Code != http.StatusUnauthorized {
		t.Fatalf("body vazio deveria dar 400 ou 401, veio %d: %s", rr.Code, rr.Body.String())
	}
}

// -----------------------------------------------------------------------
// GET /v1/auth/me
// -----------------------------------------------------------------------

func TestAuthMe_SemTokenRetorna401(t *testing.T) {
	f := newFixture(t)
	rr := f.do(f.req(http.MethodGet, "/v1/auth/me", nil))
	requireStatus(t, rr, http.StatusUnauthorized)
}

func TestAuthMe_TokenInvalidoRetorna401(t *testing.T) {
	f := newFixture(t)
	req := f.req(http.MethodGet, "/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer token-invalido-abc")
	rr := f.do(req)
	requireStatus(t, rr, http.StatusUnauthorized)
}

func TestAuthMe_HappyPath(t *testing.T) {
	f := newFixture(t)
	uid := f.seedUser("me@x", domain.RoleEditor, "c-default", false)

	rr := f.do(f.authRequest(http.MethodGet, "/v1/auth/me", nil, uid))
	requireStatus(t, rr, http.StatusOK)

	var body struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	decodeJSON(t, rr, &body)
	if body.Email != "me@x" || body.Role != "editor" {
		t.Errorf("identity errada: %+v", body)
	}
}
