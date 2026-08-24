//go:build integration

package ctlpostgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/testenv"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// -- helper: seedCompany usado por vários testes.
func firstCompanyID(t *testing.T, repo interface {
	ListCompanies(ctx context.Context) ([]domain.Company, error)
}) string {
	t.Helper()
	list, err := repo.ListCompanies(context.Background())
	if err != nil {
		t.Fatalf("list companies: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("nenhuma company no banco fresco")
	}
	return list[0].ID
}

func TestUsers_CreateEGetPorID(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)

	u, err := repo.CreateUser(ctx, domain.User{
		Email: "a@x", Name: "A", Role: domain.RoleAdmin, Active: true, CompanyID: cID,
	}, "hash-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ID == "" || u.Email != "a@x" {
		t.Errorf("user mal preenchido: %+v", u)
	}

	got, err := repo.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Email != "a@x" || got.CompanyID != cID {
		t.Errorf("get errado: %+v", got)
	}
}

func TestUsers_EmailUniqueRetornaConflict(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)

	_, err := repo.CreateUser(ctx, domain.User{
		Email: "dup@x", Name: "1", Role: domain.RoleViewer, Active: true, CompanyID: cID,
	}, "h")
	if err != nil {
		t.Fatalf("primeiro create: %v", err)
	}
	_, err = repo.CreateUser(ctx, domain.User{
		Email: "DUP@X", Name: "2", Role: domain.RoleAdmin, Active: true, CompanyID: cID,
	}, "h")
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("email duplicado (case insensitive) deveria dar ErrConflict, veio %v", err)
	}
}

func TestUsers_GetByEmailCasoInsensitivo(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)
	_, _ = repo.CreateUser(ctx, domain.User{
		Email: "MiXeD@X", Name: "m", Role: domain.RoleViewer, Active: true, CompanyID: cID,
	}, "hash-1")

	u, hash, err := repo.GetUserByEmail(ctx, "mixed@x")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if u.Email != "mixed@x" {
		t.Errorf("email não normalizado: %q", u.Email)
	}
	if hash != "hash-1" {
		t.Errorf("hash errado: %q", hash)
	}
}

func TestUsers_RoleCheckConstraintRejeitaInvalida(t *testing.T) {
	// A migração 000016 amplia o CHECK pra incluir 'editor'. Cobre que
	// as 3 roles são aceitas e que qualquer outra string é rejeitada
	// direto pelo Postgres (não só pelo domínio).
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)

	for _, r := range []domain.Role{domain.RoleAdmin, domain.RoleEditor, domain.RoleViewer} {
		_, err := repo.CreateUser(ctx, domain.User{
			Email: string(r) + "@x", Name: string(r), Role: r, Active: true, CompanyID: cID,
		}, "h")
		if err != nil {
			t.Errorf("role %q válida rejeitada: %v", r, err)
		}
	}

	_, err := repo.CreateUser(ctx, domain.User{
		Email: "hacker@x", Name: "x", Role: domain.Role("god"), Active: true, CompanyID: cID,
	}, "h")
	if err == nil {
		t.Error("role 'god' deveria ser rejeitada pelo CHECK")
	}
}

func TestUsers_ListUsersByCompanyFiltra(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cA := firstCompanyID(t, repo)
	cB, err := repo.CreateCompany(ctx, domain.Company{Name: "EmpB", Active: true})
	if err != nil {
		t.Fatalf("create company: %v", err)
	}

	_, _ = repo.CreateUser(ctx, domain.User{Email: "a1@x", Name: "a1", Role: domain.RoleViewer, Active: true, CompanyID: cA}, "h")
	_, _ = repo.CreateUser(ctx, domain.User{Email: "a2@x", Name: "a2", Role: domain.RoleEditor, Active: true, CompanyID: cA}, "h")
	_, _ = repo.CreateUser(ctx, domain.User{Email: "b1@x", Name: "b1", Role: domain.RoleAdmin, Active: true, CompanyID: cB.ID}, "h")

	listA, err := repo.ListUsersByCompany(ctx, cA)
	if err != nil {
		t.Fatalf("list company A: %v", err)
	}
	if len(listA) != 2 {
		t.Errorf("A deveria ter 2 users, veio %d", len(listA))
	}
	for _, u := range listA {
		if u.CompanyID != cA {
			t.Errorf("user de company errada em listagem A: %+v", u)
		}
	}
}

func TestUsers_UpdateAltera(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)
	u, _ := repo.CreateUser(ctx, domain.User{
		Email: "u@x", Name: "old", Role: domain.RoleViewer, Active: true, CompanyID: cID,
	}, "h")

	newRole := domain.RoleEditor
	newName := "new-name"
	inactive := false

	updated, err := repo.UpdateUser(ctx, u.ID, &newRole, &inactive, &newName, nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Role != domain.RoleEditor || updated.Active || updated.Name != "new-name" {
		t.Errorf("update não aplicou: %+v", updated)
	}
}

func TestUsers_CountActiveAdmins(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)

	// Cria 2 admins ativos + 1 admin inativo + 1 viewer.
	_, _ = repo.CreateUser(ctx, domain.User{Email: "a1@x", Name: "a1", Role: domain.RoleAdmin, Active: true, CompanyID: cID}, "h")
	_, _ = repo.CreateUser(ctx, domain.User{Email: "a2@x", Name: "a2", Role: domain.RoleAdmin, Active: true, CompanyID: cID}, "h")
	_, _ = repo.CreateUser(ctx, domain.User{Email: "a3@x", Name: "a3", Role: domain.RoleAdmin, Active: false, CompanyID: cID}, "h")
	_, _ = repo.CreateUser(ctx, domain.User{Email: "v@x", Name: "v", Role: domain.RoleViewer, Active: true, CompanyID: cID}, "h")

	n, err := repo.CountActiveAdmins(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("esperava 2 admins ativos, veio %d", n)
	}
}

func TestUsers_SetPassword(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()
	cID := firstCompanyID(t, repo)
	u, _ := repo.CreateUser(ctx, domain.User{
		Email: "x@x", Name: "x", Role: domain.RoleViewer, Active: true, CompanyID: cID,
	}, "hash-original")

	if err := repo.SetPassword(ctx, u.ID, "hash-novo"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	_, hash, err := repo.GetUserByEmail(ctx, "x@x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if hash != "hash-novo" {
		t.Errorf("hash não atualizou: %q", hash)
	}
}
