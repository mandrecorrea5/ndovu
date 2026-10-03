package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// fakePermStore materializa grants em memória. Não é ideal — não simula
// UNIQUE constraint do banco, mas serve pra ownership/lookup dos testes.
type fakePermStore struct {
	grants []domain.UserAppPermission
}

func (f *fakePermStore) GrantAppPermission(_ context.Context, userID, appID, role, _ string) (domain.UserAppPermission, error) {
	// UPSERT: se já existe, atualiza role e mantém.
	for i, g := range f.grants {
		if g.UserID == userID && g.AppID == appID {
			f.grants[i].Role = role
			return f.grants[i], nil
		}
	}
	p := domain.UserAppPermission{
		UserID: userID, AppID: appID, Role: role, GrantedAt: time.Now().UTC(),
	}
	f.grants = append(f.grants, p)
	return p, nil
}
func (f *fakePermStore) RevokeAppPermission(_ context.Context, userID, appID string) error {
	for i, g := range f.grants {
		if g.UserID == userID && g.AppID == appID {
			f.grants = append(f.grants[:i], f.grants[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}
func (f *fakePermStore) ListAppPermissionsForUser(_ context.Context, userID string) ([]domain.UserAppPermission, error) {
	out := []domain.UserAppPermission{}
	for _, g := range f.grants {
		if g.UserID == userID {
			out = append(out, g)
		}
	}
	return out, nil
}
func (f *fakePermStore) ListAppNamesForUser(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// fakeUserResolver só implementa GetUserByID (o resto do UserStore não é
// exercido pelo PermissionService). Devolve zero value pra métodos não-usados.
type fakeUserResolver struct {
	users map[string]domain.User
}

func (f *fakeUserResolver) GetUserByID(_ context.Context, id string) (domain.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return domain.User{}, domain.ErrNotFound
}
func (f *fakeUserResolver) CreateUser(context.Context, domain.User, string) (domain.User, error) {
	return domain.User{}, nil
}
func (f *fakeUserResolver) GetUserByEmail(context.Context, string) (domain.User, string, error) {
	return domain.User{}, "", nil
}
func (f *fakeUserResolver) ListUsers(context.Context) ([]domain.User, error) { return nil, nil }
func (f *fakeUserResolver) ListUsersByCompany(context.Context, string) ([]domain.User, error) {
	return nil, nil
}
func (f *fakeUserResolver) UpdateUser(context.Context, string, *domain.Role, *bool, *string, *string) (domain.User, error) {
	return domain.User{}, nil
}
func (f *fakeUserResolver) SetPassword(context.Context, string, string) error { return nil }
func (f *fakeUserResolver) SetSuperAdmin(context.Context, string) error       { return nil }
func (f *fakeUserResolver) CountActiveAdmins(context.Context) (int, error)    { return 1, nil }

// fakeAppResolver só implementa GetApp.
type fakeAppResolver struct {
	apps map[string]domain.App
}

func (f *fakeAppResolver) GetApp(_ context.Context, id string) (domain.App, error) {
	if a, ok := f.apps[id]; ok {
		return a, nil
	}
	return domain.App{}, domain.ErrNotFound
}
func (f *fakeAppResolver) CreateApp(context.Context, domain.App) (domain.App, error) {
	return domain.App{}, nil
}
func (f *fakeAppResolver) GetAppByName(context.Context, string) (domain.App, error) {
	return domain.App{}, nil
}
func (f *fakeAppResolver) ListApps(context.Context) ([]domain.App, error) { return nil, nil }
func (f *fakeAppResolver) ListAppsByCompany(context.Context, string) ([]domain.App, error) {
	return nil, nil
}
func (f *fakeAppResolver) UpdateApp(context.Context, string, domain.App) (domain.App, error) {
	return domain.App{}, nil
}
func (f *fakeAppResolver) DeleteApp(context.Context, string) error { return nil }
func (f *fakeAppResolver) ListAppNamesByCompany(context.Context, string) ([]string, error) {
	return nil, nil
}

// setup monta um PermissionService com 1 user na company-A e 2 apps: um
// da company-A e outro da company-B, pra cobrir os casos.
func setupPermSvc() (*PermissionService, *fakePermStore) {
	store := &fakePermStore{}
	users := &fakeUserResolver{
		users: map[string]domain.User{
			"u-a": {ID: "u-a", CompanyID: "company-A", Role: domain.RoleViewer},
			"u-b": {ID: "u-b", CompanyID: "company-B", Role: domain.RoleViewer},
		},
	}
	apps := &fakeAppResolver{
		apps: map[string]domain.App{
			"app-A": {ID: "app-A", Name: "portal-a", CompanyID: "company-A"},
			"app-B": {ID: "app-B", Name: "portal-b", CompanyID: "company-B"},
			"app-legado": {ID: "app-legado", Name: "sem-company", CompanyID: ""},
		},
	}
	return NewPermissionService(store, users, apps), store
}

func TestPermissions_GrantHappyPath(t *testing.T) {
	svc, store := setupPermSvc()
	_, err := svc.Grant(context.Background(), "u-a", "app-A", "viewer", "actor-1")
	if err != nil {
		t.Fatalf("grant válido falhou: %v", err)
	}
	if len(store.grants) != 1 {
		t.Fatalf("esperava 1 grant persistido, veio %d", len(store.grants))
	}
}

func TestPermissions_GrantIdempotente(t *testing.T) {
	// Chamadas idênticas repetidas não devem duplicar (upsert).
	svc, store := setupPermSvc()
	ctx := context.Background()
	_, _ = svc.Grant(ctx, "u-a", "app-A", "viewer", "actor-1")
	_, err := svc.Grant(ctx, "u-a", "app-A", "viewer", "actor-1")
	if err != nil {
		t.Fatalf("segundo grant falhou: %v", err)
	}
	if len(store.grants) != 1 {
		t.Fatalf("esperava 1 grant (upsert), veio %d", len(store.grants))
	}
}

func TestPermissions_GrantCrossCompanyBloqueado(t *testing.T) {
	// User da company-A não pode receber grant em app da company-B.
	svc, store := setupPermSvc()
	_, err := svc.Grant(context.Background(), "u-a", "app-B", "viewer", "actor-1")
	if err == nil {
		t.Fatal("esperava erro no grant cross-company")
	}
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("esperava ValidationError, veio %T: %v", err, err)
	}
	if !strings.Contains(strings.Join(ve.Issues, "; "), "mesma empresa") {
		t.Errorf("issues não mencionam 'mesma empresa': %v", ve.Issues)
	}
	if len(store.grants) != 0 {
		t.Errorf("nada deveria ter sido persistido, veio %d", len(store.grants))
	}
}

func TestPermissions_GrantEmAppSemCompanyBloqueado(t *testing.T) {
	// App legado sem company_id deve bloquear (evita vazamento inadvertido).
	svc, _ := setupPermSvc()
	_, err := svc.Grant(context.Background(), "u-a", "app-legado", "viewer", "actor-1")
	if err == nil {
		t.Fatal("esperava erro em app sem company_id")
	}
}

func TestPermissions_GrantValidaCamposObrigatorios(t *testing.T) {
	svc, _ := setupPermSvc()
	cases := []struct{ userID, appID string }{
		{"", "app-A"},
		{"u-a", ""},
		{"", ""},
	}
	for _, c := range cases {
		_, err := svc.Grant(context.Background(), c.userID, c.appID, "viewer", "actor-1")
		if err == nil {
			t.Errorf("(%q,%q): esperava erro de validação", c.userID, c.appID)
		}
		var ve *domain.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("(%q,%q): esperava ValidationError, veio %T", c.userID, c.appID, err)
		}
	}
}

func TestPermissions_GrantUserInexistenteRetornaNotFound(t *testing.T) {
	svc, _ := setupPermSvc()
	_, err := svc.Grant(context.Background(), "u-nao-existe", "app-A", "viewer", "actor")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestPermissions_GrantAppInexistenteRetornaNotFound(t *testing.T) {
	svc, _ := setupPermSvc()
	_, err := svc.Grant(context.Background(), "u-a", "app-nao-existe", "viewer", "actor")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestPermissions_RevokeExistente(t *testing.T) {
	svc, store := setupPermSvc()
	ctx := context.Background()
	_, _ = svc.Grant(ctx, "u-a", "app-A", "viewer", "actor")
	if err := svc.Revoke(ctx, "u-a", "app-A"); err != nil {
		t.Fatalf("revoke falhou: %v", err)
	}
	if len(store.grants) != 0 {
		t.Errorf("esperava 0 grants após revoke, veio %d", len(store.grants))
	}
}

func TestPermissions_RevokeInexistenteRetornaNotFound(t *testing.T) {
	svc, _ := setupPermSvc()
	err := svc.Revoke(context.Background(), "u-a", "app-A")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("esperava ErrNotFound em revoke sem grant, veio %v", err)
	}
}

func TestPermissions_ListForUserFiltraPorUser(t *testing.T) {
	svc, _ := setupPermSvc()
	ctx := context.Background()
	_, _ = svc.Grant(ctx, "u-a", "app-A", "viewer", "actor")
	// Grant em outro user (u-b em app-B — cross-company OK porque u-b é da B)
	_, _ = svc.Grant(ctx, "u-b", "app-B", "viewer", "actor")

	list, err := svc.ListForUser(ctx, "u-a")
	if err != nil {
		t.Fatalf("list falhou: %v", err)
	}
	if len(list) != 1 || list[0].AppID != "app-A" {
		t.Errorf("esperava só o grant de u-a, veio %+v", list)
	}
}
