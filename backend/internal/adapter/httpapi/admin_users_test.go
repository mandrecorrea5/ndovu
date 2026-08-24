package httpapi

import (
	"net/http"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// -----------------------------------------------------------------------
// GET /v1/admin/users
// -----------------------------------------------------------------------

func TestAdminUsers_ViewerNaoAcessa(t *testing.T) {
	// requireRole(admin) bloqueia viewer/editor.
	f := newFixture(t)
	viewerID := f.seedUser("v@x", domain.RoleViewer, "c-default", false)
	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/users", nil, viewerID))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminUsers_EditorNaoAcessa(t *testing.T) {
	f := newFixture(t)
	editorID := f.seedUser("e@x", domain.RoleEditor, "c-default", false)
	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/users", nil, editorID))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminUsers_SuperVeTudo(t *testing.T) {
	f := newFixture(t)
	superID := f.seedUser("super@x", domain.RoleAdmin, "c-default", true)
	companyB := f.seedCompany("EmpresaB")
	f.seedUser("outra@x", domain.RoleViewer, companyB, false)

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/users", nil, superID))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Users []domain.User }
	decodeJSON(t, rr, &body)
	if len(body.Users) < 2 {
		t.Errorf("super deveria ver todos users; veio %d", len(body.Users))
	}
}

func TestAdminUsers_AdminNaoSuperVeSoOsDaSuaCompany(t *testing.T) {
	f := newFixture(t)
	// Admin A na company default; outro user na company B.
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("EmpresaB")
	f.seedUser("b@x", domain.RoleViewer, companyB, false)

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/users", nil, adminA))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Users []domain.User }
	decodeJSON(t, rr, &body)
	for _, u := range body.Users {
		if u.CompanyID != "c-default" {
			t.Errorf("admin da default viu user de outra company: %+v", u)
		}
	}
}

// -----------------------------------------------------------------------
// POST /v1/admin/users
// -----------------------------------------------------------------------

func TestAdminUsersCreate_AdminNaoSuperForceCompanyDele(t *testing.T) {
	// Admin A tenta criar user com companyId=B; o handler força ser company-A.
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("EmpresaB")

	rr := f.do(f.authRequest(http.MethodPost, "/v1/admin/users", map[string]any{
		"email":     "novo@x",
		"name":      "novo",
		"password":  "12345678",
		"role":      "viewer",
		"companyId": companyB, // tentativa cross-company
	}, adminA))
	requireStatus(t, rr, http.StatusCreated)

	var created struct{ CompanyID string `json:"companyId"` }
	decodeJSON(t, rr, &created)
	if created.CompanyID != "c-default" {
		t.Errorf("companyId deveria ter sido forçado pra c-default, veio %q", created.CompanyID)
	}
}

func TestAdminUsersCreate_SuperPodeUsarQualquerCompany(t *testing.T) {
	f := newFixture(t)
	super := f.seedUser("s@x", domain.RoleAdmin, "c-default", true)
	companyB := f.seedCompany("EmpresaB")

	rr := f.do(f.authRequest(http.MethodPost, "/v1/admin/users", map[string]any{
		"email":     "novo@x",
		"name":      "novo",
		"password":  "12345678",
		"role":      "editor",
		"companyId": companyB,
	}, super))
	requireStatus(t, rr, http.StatusCreated)

	var created struct{ CompanyID string `json:"companyId"` }
	decodeJSON(t, rr, &created)
	if created.CompanyID != companyB {
		t.Errorf("super deveria conseguir criar em qualquer company, veio %q", created.CompanyID)
	}
}

// -----------------------------------------------------------------------
// PATCH /v1/admin/users/{id}
// -----------------------------------------------------------------------

func TestAdminUsersPatch_BloquiaCrossTenant(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("EmpresaB")
	userB := f.seedUser("bU@x", domain.RoleViewer, companyB, false)

	newName := "hackeado"
	rr := f.do(f.authRequest(http.MethodPatch, "/v1/admin/users/"+userB, map[string]any{
		"name": newName,
	}, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminUsersPatch_AceitaPasswordEChamaResetPassword(t *testing.T) {
	f := newFixture(t)
	admin := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	target := f.seedUser("t@x", domain.RoleViewer, "c-default", false)
	hashAntes := f.users.hashes[target]

	rr := f.do(f.authRequest(http.MethodPatch, "/v1/admin/users/"+target, map[string]any{
		"password": "senha-nova-1234",
	}, admin))
	requireStatus(t, rr, http.StatusOK)

	// Hash deve ter mudado (ResetPassword bcrypt gera novo).
	if f.users.hashes[target] == hashAntes {
		t.Error("password não foi resetado — hash idêntico")
	}
}

func TestAdminUsersPatch_NaoMoveParaOutraCompany(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	targetA := f.seedUser("t@x", domain.RoleViewer, "c-default", false)
	companyB := f.seedCompany("EmpresaB")

	rr := f.do(f.authRequest(http.MethodPatch, "/v1/admin/users/"+targetA, map[string]any{
		"companyId": companyB,
	}, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminUsersPatch_MudaRoleParaEditor(t *testing.T) {
	f := newFixture(t)
	admin := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	target := f.seedUser("t@x", domain.RoleViewer, "c-default", false)

	rr := f.do(f.authRequest(http.MethodPatch, "/v1/admin/users/"+target, map[string]any{
		"role": "editor",
	}, admin))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Role string }
	decodeJSON(t, rr, &body)
	if body.Role != "editor" {
		t.Errorf("role não mudou pra editor: %q", body.Role)
	}
}
