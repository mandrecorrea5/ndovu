package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// injectIdentity monta um handler que passa uma identidade fake pelo context,
// como se o bearerAuth tivesse validado o token. Usado para testar os
// middlewares de autorização sem tocar em JWT.
func injectIdentity(role domain.Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := domain.Identity{UserID: "u-1", Email: "u@x", Role: role, CompanyID: "c-1"}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey, id)))
	})
}

func requestWith(role domain.Role, h http.Handler) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	injectIdentity(role, h).ServeHTTP(rr, req)
	return rr
}

// TestRequireRole_AceitaSomenteExata garante que requireRole(admin) rejeita
// editor e viewer, e aceita admin.
func TestRequireRole_AceitaSomenteExata(t *testing.T) {
	handler := requireRole(domain.RoleAdmin)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		role   domain.Role
		status int
	}{
		{domain.RoleAdmin, http.StatusOK},
		{domain.RoleEditor, http.StatusForbidden},
		{domain.RoleViewer, http.StatusForbidden},
	}
	for _, c := range cases {
		rr := requestWith(c.role, handler)
		if rr.Code != c.status {
			t.Fatalf("role=%s: esperado %d, veio %d", c.role, c.status, rr.Code)
		}
	}
}

// TestRequireAnyRole_EditorOuAdmin cobre o middleware usado nas rotas
// de escrita colaborativa (funnels, triagem de issues).
func TestRequireAnyRole_EditorOuAdmin(t *testing.T) {
	handler := requireAnyRole(domain.RoleEditor, domain.RoleAdmin)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	cases := []struct {
		role   domain.Role
		status int
	}{
		{domain.RoleAdmin, http.StatusOK},
		{domain.RoleEditor, http.StatusOK},
		{domain.RoleViewer, http.StatusForbidden},
	}
	for _, c := range cases {
		rr := requestWith(c.role, handler)
		if rr.Code != c.status {
			t.Fatalf("role=%s: esperado %d, veio %d", c.role, c.status, rr.Code)
		}
	}
}

// TestRequireAnyRole_SemIdentidadeRejeita garante que uma request sem
// bearerAuth prévio (sem Identity no context) recebe 403 — nunca vaza
// como se fosse admin.
func TestRequireAnyRole_SemIdentidadeRejeita(t *testing.T) {
	handler := requireAnyRole(domain.RoleEditor, domain.RoleAdmin)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Fatal("next não deveria ser chamado sem identidade")
		}),
	)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("esperado 403 sem identity, veio %d", rr.Code)
	}
}

// TestRoleAtLeastEditor cobre o helper de domínio usado nas validações
// de isShared/etc.
func TestRoleAtLeastEditor(t *testing.T) {
	if !domain.RoleAdmin.AtLeastEditor() {
		t.Fatal("admin deve ser AtLeastEditor")
	}
	if !domain.RoleEditor.AtLeastEditor() {
		t.Fatal("editor deve ser AtLeastEditor")
	}
	if domain.RoleViewer.AtLeastEditor() {
		t.Fatal("viewer NÃO deve ser AtLeastEditor")
	}
}
