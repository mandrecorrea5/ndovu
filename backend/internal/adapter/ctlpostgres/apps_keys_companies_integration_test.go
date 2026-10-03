//go:build integration

package ctlpostgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/testenv"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// -- Companies ---------------------------------------------------------

func TestCompanies_CRUD(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()

	c, err := repo.CreateCompany(ctx, domain.Company{Name: "Acme", Document: "12345", Active: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.ID == "" {
		t.Error("id não veio")
	}

	got, err := repo.GetCompany(ctx, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Acme" || got.Document != "12345" {
		t.Errorf("get errado: %+v", got)
	}

	updated, err := repo.UpdateCompany(ctx, c.ID, domain.Company{Name: "Acme2", Document: "999", Active: false})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Acme2" || updated.Active {
		t.Errorf("update falhou: %+v", updated)
	}

	if err := repo.DeleteCompany(ctx, c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetCompany(ctx, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("company deveria ter sido deletada")
	}
}

func TestCompanies_ListaInclusiveASeedadas(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()

	_, _ = repo.CreateCompany(ctx, domain.Company{Name: "X"})
	_, _ = repo.CreateCompany(ctx, domain.Company{Name: "Y"})

	list, err := repo.ListCompanies(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// >=3 (a "Padrão" da migração + 2 novas).
	if len(list) < 3 {
		t.Errorf("esperava ≥3 companies, veio %d", len(list))
	}
}

// -- Apps --------------------------------------------------------------

func TestApps_CRUDBasico(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)

	a, err := repo.CreateApp(ctx, domain.App{
		Name: "portal", Technology: "React", CompanyID: cID, Responsible: "ana@x",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.ID == "" {
		t.Error("id não veio")
	}

	got, err := repo.GetApp(ctx, a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "portal" || got.CompanyID != cID {
		t.Errorf("get errado: %+v", got)
	}

	byName, err := repo.GetAppByName(ctx, "portal")
	if err != nil {
		t.Fatalf("get by name: %v", err)
	}
	if byName.ID != a.ID {
		t.Error("get by name errado")
	}
}

func TestApps_NameUniqueRetornaConflict(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)

	_, err := repo.CreateApp(ctx, domain.App{Name: "dup", CompanyID: cID})
	if err != nil {
		t.Fatalf("primeiro create: %v", err)
	}
	_, err = repo.CreateApp(ctx, domain.App{Name: "dup", CompanyID: cID})
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("name duplicado deveria dar ErrConflict, veio %v", err)
	}
}

func TestApps_ListAppsByCompany(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cA := firstCompanyID(t, repo)
	cB, _ := repo.CreateCompany(ctx, domain.Company{Name: "B", Active: true})

	_, _ = repo.CreateApp(ctx, domain.App{Name: "a1", CompanyID: cA})
	_, _ = repo.CreateApp(ctx, domain.App{Name: "a2", CompanyID: cA})
	_, _ = repo.CreateApp(ctx, domain.App{Name: "b1", CompanyID: cB.ID})

	apps, err := repo.ListAppsByCompany(ctx, cA)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(apps) != 2 {
		t.Errorf("esperava 2 apps em A, veio %d", len(apps))
	}
}

func TestApps_ListAppNamesByCompany(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)
	_, _ = repo.CreateApp(ctx, domain.App{Name: "alpha", CompanyID: cID})
	_, _ = repo.CreateApp(ctx, domain.App{Name: "beta", CompanyID: cID})

	names, err := repo.ListAppNamesByCompany(ctx, cID)
	if err != nil {
		t.Fatalf("list names: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("esperava 2 nomes, veio %d: %v", len(names), names)
	}
}

func TestApps_UpdateEDelete(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)
	a, _ := repo.CreateApp(ctx, domain.App{Name: "orig", CompanyID: cID})

	updated, err := repo.UpdateApp(ctx, a.ID, domain.App{
		Name: "renomeado", Technology: "Vue", CompanyID: cID, Responsible: "b",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "renomeado" || updated.Technology != "Vue" {
		t.Errorf("update falhou: %+v", updated)
	}

	if err := repo.DeleteApp(ctx, a.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetApp(ctx, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("app deveria ter sido deletado")
	}
}

// -- API Keys ---------------------------------------------------------

func TestAPIKeys_CRUD(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)
	app, _ := repo.CreateApp(ctx, domain.App{Name: "portal", CompanyID: cID})

	k, err := repo.CreateAPIKey(ctx, domain.APIKey{
		App: "portal", AppID: app.ID, Label: "prod", Prefix: "ndk_abc123",
	}, "hash-plain", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if k.ID == "" || !k.Active {
		t.Errorf("key mal preenchida: %+v", k)
	}

	got, err := repo.GetAPIKeyByID(ctx, k.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.App != "portal" {
		t.Errorf("get errado: %+v", got)
	}

	found, err := repo.FindActiveKeyByHash(ctx, "hash-plain")
	if err != nil {
		t.Fatalf("find by hash: %v", err)
	}
	if found.ID != k.ID {
		t.Errorf("hash lookup errado")
	}
}

func TestAPIKeys_RevokeInvalidaLookup(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)
	app, _ := repo.CreateApp(ctx, domain.App{Name: "portal", CompanyID: cID})
	k, _ := repo.CreateAPIKey(ctx, domain.APIKey{App: "portal", AppID: app.ID, Prefix: "ndk_x"}, "h1", "")

	if err := repo.RevokeAPIKey(ctx, k.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_, err := repo.FindActiveKeyByHash(ctx, "h1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("chave revogada deveria sumir do lookup, veio %v", err)
	}
	// GetByID ainda encontra (histórico), mas Active=false.
	got, _ := repo.GetAPIKeyByID(ctx, k.ID)
	if got.Active {
		t.Errorf("Active deveria ser false após revoke")
	}
}

func TestAPIKeys_RotationRevogaAnteriorEMantemHistorico(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)
	app, err := repo.CreateApp(ctx, domain.App{Name: "portal-rotation", CompanyID: cID})
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.CreateAPIKey(ctx, domain.APIKey{
		App: app.Name, AppID: app.ID, Prefix: "ndk_first", EncryptedKey: "ciphertext-1",
	}, "hash-1", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.CreateAPIKey(ctx, domain.APIKey{
		App: app.Name, AppID: app.ID, Prefix: "ndk_second", EncryptedKey: "ciphertext-2",
	}, "hash-2", "")
	if err != nil {
		t.Fatal(err)
	}

	history, err := repo.ListAPIKeysByCompany(ctx, cID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("esperava 2 entradas de histórico, veio %d", len(history))
	}
	if !history[0].Active || history[0].ID != second.ID || history[0].EncryptedKey != "ciphertext-2" {
		t.Errorf("chave atual incorreta: %+v", history[0])
	}
	if history[1].Active || history[1].ID != first.ID || history[1].RevokedAt == nil || history[1].EncryptedKey != "ciphertext-1" {
		t.Errorf("chave anterior não foi revogada e preservada: %+v", history[1])
	}
	if _, err := repo.FindActiveKeyByHash(ctx, "hash-1"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("hash antigo não deveria autenticar: %v", err)
	}
}

func TestAPIKeys_ListAPIKeysByCompany_JoinPorApp(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cA := firstCompanyID(t, repo)
	cB, _ := repo.CreateCompany(ctx, domain.Company{Name: "B", Active: true})

	appA, _ := repo.CreateApp(ctx, domain.App{Name: "app-A", CompanyID: cA})
	appB, _ := repo.CreateApp(ctx, domain.App{Name: "app-B", CompanyID: cB.ID})
	_, _ = repo.CreateAPIKey(ctx, domain.APIKey{App: appA.Name, AppID: appA.ID, Prefix: "pA"}, "hA", "")
	_, _ = repo.CreateAPIKey(ctx, domain.APIKey{App: appB.Name, AppID: appB.ID, Prefix: "pB"}, "hB", "")

	keysA, err := repo.ListAPIKeysByCompany(ctx, cA)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keysA) != 1 || keysA[0].App != "app-A" {
		t.Errorf("esperava só chave de app-A, veio %+v", keysA)
	}
}
