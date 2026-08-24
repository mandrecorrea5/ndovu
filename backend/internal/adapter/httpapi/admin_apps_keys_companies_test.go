package httpapi

import (
	"net/http"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// -----------------------------------------------------------------------
// /v1/admin/apps
// -----------------------------------------------------------------------

func TestAdminApps_ListagemFiltraPorCompany(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	f.seedApp("app-A", "c-default")
	f.seedApp("app-B", companyB)

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/apps", nil, adminA))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Apps []domain.App }
	decodeJSON(t, rr, &body)
	if len(body.Apps) != 1 || body.Apps[0].Name != "app-A" {
		t.Errorf("admin A deveria ver só app-A, veio %+v", body.Apps)
	}
}

func TestAdminApps_GetBloqueiaCrossTenant(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	appBID := f.seedApp("app-B", companyB)

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/apps/"+appBID, nil, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminApps_PostForceCompanyDoActor(t *testing.T) {
	// Admin A tenta criar app em company B → handler força ser A.
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")

	rr := f.do(f.authRequest(http.MethodPost, "/v1/admin/apps", map[string]any{
		"name":      "novo-app",
		"companyId": companyB,
	}, adminA))
	requireStatus(t, rr, http.StatusCreated)

	var body struct{ CompanyID string `json:"companyId"` }
	decodeJSON(t, rr, &body)
	if body.CompanyID != "c-default" {
		t.Errorf("app deveria ter sido criado em c-default, veio %q", body.CompanyID)
	}
}

func TestAdminApps_PatchBloqueiaCrossTenant(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	appBID := f.seedApp("app-B", companyB)

	newName := "hack"
	rr := f.do(f.authRequest(http.MethodPatch, "/v1/admin/apps/"+appBID, map[string]any{
		"name": newName,
	}, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminApps_DeleteBloqueiaCrossTenant(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	appBID := f.seedApp("app-B", companyB)

	rr := f.do(f.authRequest(http.MethodDelete, "/v1/admin/apps/"+appBID, nil, adminA))
	requireStatus(t, rr, http.StatusForbidden)
	// App B deve continuar existindo.
	if _, ok := f.apps.items[appBID]; !ok {
		t.Error("app B foi apagado apesar do 403")
	}
}

// -----------------------------------------------------------------------
// /v1/admin/api-keys
// -----------------------------------------------------------------------

func TestAdminKeys_ListagemFiltraPorCompany(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	f.seedApp("app-A", "c-default")
	_ = f.seedKey("app-A")

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/api-keys", nil, adminA))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Keys []domain.APIKey }
	decodeJSON(t, rr, &body)
	if len(body.Keys) != 1 || body.Keys[0].App != "app-A" {
		t.Errorf("admin A deveria ver 1 key de app-A, veio %+v", body.Keys)
	}
}

func TestAdminKeys_PostBloqueiaAppDeOutraCompany(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")
	f.seedApp("app-B", companyB)

	rr := f.do(f.authRequest(http.MethodPost, "/v1/admin/api-keys", map[string]any{
		"app": "app-B", "label": "hack",
	}, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminKeys_DeleteHappyPath(t *testing.T) {
	f := newFixture(t)
	admin := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	f.seedApp("app-A", "c-default")
	f.seedKey("app-A")

	// Pega o id da chave.
	var keyID string
	for id := range f.keys.keys {
		keyID = id
	}
	rr := f.do(f.authRequest(http.MethodDelete, "/v1/admin/api-keys/"+keyID, nil, admin))
	requireStatus(t, rr, http.StatusOK)

	if f.keys.keys[keyID].Active {
		t.Error("chave deveria ter sido revogada")
	}
}

// -----------------------------------------------------------------------
// /v1/admin/companies
// -----------------------------------------------------------------------

func TestAdminCompanies_AdminNaoSuperSoVeAPropria(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	f.seedCompany("B")
	f.seedCompany("C")

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/companies", nil, adminA))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Companies []domain.Company }
	decodeJSON(t, rr, &body)
	if len(body.Companies) != 1 || body.Companies[0].ID != "c-default" {
		t.Errorf("admin A deveria ver só c-default, veio %+v", body.Companies)
	}
}

func TestAdminCompanies_SuperVeTodas(t *testing.T) {
	f := newFixture(t)
	super := f.seedUser("s@x", domain.RoleAdmin, "c-default", true)
	f.seedCompany("B")
	f.seedCompany("C")

	rr := f.do(f.authRequest(http.MethodGet, "/v1/admin/companies", nil, super))
	requireStatus(t, rr, http.StatusOK)

	var body struct{ Companies []domain.Company }
	decodeJSON(t, rr, &body)
	if len(body.Companies) != 3 {
		t.Errorf("super deveria ver 3 companies, veio %d", len(body.Companies))
	}
}

func TestAdminCompanies_PostSoSuper(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)

	rr := f.do(f.authRequest(http.MethodPost, "/v1/admin/companies", map[string]any{
		"name": "Nova", "active": true,
	}, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminCompanies_PostSuperOK(t *testing.T) {
	f := newFixture(t)
	super := f.seedUser("s@x", domain.RoleAdmin, "c-default", true)

	rr := f.do(f.authRequest(http.MethodPost, "/v1/admin/companies", map[string]any{
		"name": "Nova", "active": true,
	}, super))
	requireStatus(t, rr, http.StatusCreated)
}

func TestAdminCompanies_DeleteSoSuper(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)

	rr := f.do(f.authRequest(http.MethodDelete, "/v1/admin/companies/c-default", nil, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}

func TestAdminCompanies_PatchBloqueiaOutraCompany(t *testing.T) {
	f := newFixture(t)
	adminA := f.seedUser("a@x", domain.RoleAdmin, "c-default", false)
	companyB := f.seedCompany("B")

	rr := f.do(f.authRequest(http.MethodPatch, "/v1/admin/companies/"+companyB,
		map[string]any{"name": "hack"}, adminA))
	requireStatus(t, rr, http.StatusForbidden)
}
