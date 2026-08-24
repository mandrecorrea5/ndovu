package usecase

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// Testes adicionais para AuthService cobrindo cenários que faltavam em
// auth_test.go: ListUsersByCompany, GetUserByID, ResetPassword,
// UpdateUser com role editor, validações de senha, EnsureBootstrapAdmin.

func newAuthSvcExtra() (*AuthService, *fakeUserStore, *fakeCompanyStore) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	users := newFakeUserStore()
	// newFakeCompanyStore vem seedado com a company "Padrão" (id=c1) —
	// necessária para EnsureBootstrapAdmin não falhar com "sem empresa".
	companies := newFakeCompanyStore()
	svc := NewAuthService(users, companies, "test-secret", time.Hour, logger)
	return svc, users, companies
}

func seedUser(t *testing.T, users *fakeUserStore, email, companyID string, role domain.Role, active bool) domain.User {
	t.Helper()
	u, err := users.CreateUser(context.Background(), domain.User{
		Email: email, Name: "u", Role: role, Active: active, CompanyID: companyID,
	}, "hash")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func TestAuth_ListUsersByCompany(t *testing.T) {
	svc, users, _ := newAuthSvcExtra()
	_ = seedUser(t, users, "a@x", "company-A", domain.RoleAdmin, true)
	_ = seedUser(t, users, "b@x", "company-B", domain.RoleAdmin, true)
	_ = seedUser(t, users, "c@x", "company-A", domain.RoleViewer, true)

	list, err := svc.ListUsersByCompany(context.Background(), "company-A")
	if err != nil {
		t.Fatalf("list falhou: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("esperava 2 users em company-A, veio %d", len(list))
	}
	for _, u := range list {
		if u.CompanyID != "company-A" {
			t.Errorf("user %s de company errada: %s", u.Email, u.CompanyID)
		}
	}
}

func TestAuth_GetUserByIDNaoExisteRetornaNotFound(t *testing.T) {
	svc, _, _ := newAuthSvcExtra()
	_, err := svc.GetUserByID(context.Background(), "id-inexistente")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestAuth_ResetPasswordValidaTamanhoMinimo(t *testing.T) {
	svc, users, _ := newAuthSvcExtra()
	u := seedUser(t, users, "u@x", "c", domain.RoleAdmin, true)
	cases := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"vazia", "", true},
		{"7 chars", "1234567", true},
		{"8 chars mínimo", "12345678", false},
		{"longa", "senha-super-longa-123", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := svc.ResetPassword(context.Background(), u.ID, c.password)
			if c.wantErr && err == nil {
				t.Errorf("esperava erro pra senha %q", c.password)
			}
			if !c.wantErr && err != nil {
				t.Errorf("senha %q deveria ser aceita: %v", c.password, err)
			}
		})
	}
}

func TestAuth_UpdateUserRoleInvalidaRejeita(t *testing.T) {
	svc, users, _ := newAuthSvcExtra()
	u := seedUser(t, users, "u@x", "c", domain.RoleAdmin, true)
	invalid := domain.Role("guest")
	_, err := svc.UpdateUser(context.Background(), u.ID, UpdateUserInput{Role: &invalid})
	if err == nil {
		t.Fatal("esperava erro pra role inválida")
	}
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("esperava ValidationError, veio %T", err)
	}
}

func TestAuth_UpdateUserAceitaEditorComoRoleValida(t *testing.T) {
	svc, users, _ := newAuthSvcExtra()
	u := seedUser(t, users, "u@x", "c", domain.RoleViewer, true)
	editor := domain.RoleEditor
	_, err := svc.UpdateUser(context.Background(), u.ID, UpdateUserInput{Role: &editor})
	if err != nil {
		t.Errorf("editor deveria ser aceito como role, veio: %v", err)
	}
}

func TestAuth_UpdateUserProtegeUltimoAdminAtivo(t *testing.T) {
	// Já testado em TestNaoRemoveUltimoAdmin, mas cobrimos a via de
	// "downgrade de role" (não só desativar).
	svc, users, _ := newAuthSvcExtra()
	u := seedUser(t, users, "solo@x", "c", domain.RoleAdmin, true)
	viewer := domain.RoleViewer
	_, err := svc.UpdateUser(context.Background(), u.ID, UpdateUserInput{Role: &viewer})
	if err == nil {
		t.Fatal("downgrade do último admin deveria falhar")
	}
}

func TestAuth_UpdateUserDowngradeOKSeExisteOutroAdmin(t *testing.T) {
	svc, users, _ := newAuthSvcExtra()
	_ = seedUser(t, users, "admin1@x", "c", domain.RoleAdmin, true)
	solo := seedUser(t, users, "admin2@x", "c", domain.RoleAdmin, true)

	viewer := domain.RoleViewer
	_, err := svc.UpdateUser(context.Background(), solo.ID, UpdateUserInput{Role: &viewer})
	if err != nil {
		t.Errorf("downgrade com 2 admins deveria funcionar: %v", err)
	}
}

func TestAuth_EnsureBootstrapAdminSemUsuariosCria(t *testing.T) {
	svc, users, _ := newAuthSvcExtra()
	if err := svc.EnsureBootstrapAdmin(context.Background(), "boot@ndovu.local", "senha1234"); err != nil {
		t.Fatalf("bootstrap falhou: %v", err)
	}
	if len(users.users) != 1 {
		t.Errorf("esperava 1 admin bootstrap, veio %d", len(users.users))
	}
	// Deve ser admin ativo super.
	var created domain.User
	for _, u := range users.users {
		created = u
	}
	if created.Role != domain.RoleAdmin || !created.Active {
		t.Errorf("bootstrap admin com dados errados: %+v", created)
	}
}

func TestAuth_EnsureBootstrapAdminComUsuariosNaoRecria(t *testing.T) {
	svc, users, _ := newAuthSvcExtra()
	_ = seedUser(t, users, "existente@x", "c", domain.RoleAdmin, true)
	before := len(users.users)

	if err := svc.EnsureBootstrapAdmin(context.Background(), "boot@ndovu.local", "senha1234"); err != nil {
		t.Fatalf("erro: %v", err)
	}
	if len(users.users) != before {
		t.Errorf("bootstrap não deveria criar quando já há usuários; antes=%d depois=%d", before, len(users.users))
	}
}
