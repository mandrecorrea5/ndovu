//go:build integration

package ctlpostgres_test

import (
	"context"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/testenv"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// TestSmoke_MigrationsAplicam garante que testcontainers sobe Postgres,
// ctlpostgres.Connect roda as 16 migrações sem erro, e o repo responde
// consultas triviais. Se este falha, todos os outros testes deste pacote
// vão falhar — é o canary do sprint.
func TestSmoke_MigrationsAplicam(t *testing.T) {
	repo := testenv.StartPostgres(t)
	ctx := context.Background()

	// Companies: migração 000004 já cria a "Padrão".
	companies, err := repo.ListCompanies(ctx)
	if err != nil {
		t.Fatalf("list companies: %v", err)
	}
	found := false
	for _, c := range companies {
		if c.Name == "Padrão" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("company 'Padrão' deveria existir pós-migração; veio %+v", companies)
	}

	// Users: início vazio (bootstrap admin é criado por AuthService, não migração).
	users, err := repo.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("esperava 0 users no banco fresco, veio %d", len(users))
	}

	// CountActiveAdmins deve começar em 0.
	n, err := repo.CountActiveAdmins(ctx)
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if n != 0 {
		t.Errorf("esperava 0 admins ativos, veio %d", n)
	}

	// Consultando entidade que ainda não existe.
	_, err = repo.GetUserByID(ctx, "00000000-0000-0000-0000-000000000000")
	if err != domain.ErrNotFound {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}
