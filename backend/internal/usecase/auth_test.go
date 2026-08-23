package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// fakeUserStore implementa domain.UserStore em memória.
type fakeUserStore struct {
	users  map[string]domain.User // por id
	hashes map[string]string      // por id
	seq    int
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{users: map[string]domain.User{}, hashes: map[string]string{}}
}

func (f *fakeUserStore) CreateUser(_ context.Context, u domain.User, hash string) (domain.User, error) {
	for _, existing := range f.users {
		if existing.Email == strings.ToLower(u.Email) {
			return domain.User{}, domain.ErrConflict
		}
	}
	f.seq++
	u.ID = string(rune('a' + f.seq))
	u.Email = strings.ToLower(u.Email)
	u.CreatedAt = time.Now()
	u.UpdatedAt = u.CreatedAt
	f.users[u.ID] = u
	f.hashes[u.ID] = hash
	return u, nil
}

func (f *fakeUserStore) GetUserByEmail(_ context.Context, email string) (domain.User, string, error) {
	for id, u := range f.users {
		if u.Email == strings.ToLower(email) {
			return u, f.hashes[id], nil
		}
	}
	return domain.User{}, "", domain.ErrNotFound
}

func (f *fakeUserStore) GetUserByID(_ context.Context, id string) (domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (f *fakeUserStore) ListUsers(_ context.Context) ([]domain.User, error) {
	out := []domain.User{}
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, nil
}

func (f *fakeUserStore) UpdateUser(_ context.Context, id string, role *domain.Role, active *bool, name *string, companyID *string) (domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	if role != nil {
		u.Role = *role
	}
	if active != nil {
		u.Active = *active
	}
	if name != nil {
		u.Name = *name
	}
	if companyID != nil && *companyID != "" {
		u.CompanyID = *companyID
	}
	f.users[id] = u
	return u, nil
}

func (f *fakeUserStore) SetPassword(_ context.Context, id string, hash string) error {
	if _, ok := f.users[id]; !ok {
		return domain.ErrNotFound
	}
	f.hashes[id] = hash
	return nil
}

func (f *fakeUserStore) CountActiveAdmins(_ context.Context) (int, error) {
	n := 0
	for _, u := range f.users {
		if u.Role == domain.RoleAdmin && u.Active {
			n++
		}
	}
	return n, nil
}

// fakeCompanyStore implementa domain.CompanyStore em memória (só o suficiente
// para o bootstrap do admin — CreateCompany/UpdateCompany/etc não são exercidos
// pelos testes atuais).
type fakeCompanyStore struct {
	companies map[string]domain.Company
}

func newFakeCompanyStore() *fakeCompanyStore {
	return &fakeCompanyStore{companies: map[string]domain.Company{
		"c1": {ID: "c1", Name: "Padrão", Active: true},
	}}
}

func (f *fakeCompanyStore) CreateCompany(_ context.Context, c domain.Company) (domain.Company, error) {
	c.ID = "c" + string(rune('a'+len(f.companies)))
	f.companies[c.ID] = c
	return c, nil
}

func (f *fakeCompanyStore) GetCompany(_ context.Context, id string) (domain.Company, error) {
	c, ok := f.companies[id]
	if !ok {
		return domain.Company{}, domain.ErrNotFound
	}
	return c, nil
}

func (f *fakeCompanyStore) ListCompanies(_ context.Context) ([]domain.Company, error) {
	out := []domain.Company{}
	for _, c := range f.companies {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeCompanyStore) UpdateCompany(_ context.Context, id string, c domain.Company) (domain.Company, error) {
	if _, ok := f.companies[id]; !ok {
		return domain.Company{}, domain.ErrNotFound
	}
	c.ID = id
	f.companies[id] = c
	return c, nil
}

func (f *fakeCompanyStore) DeleteCompany(_ context.Context, id string) error {
	if _, ok := f.companies[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.companies, id)
	return nil
}

func newAuthForTest(t *testing.T) (*AuthService, *fakeUserStore) {
	t.Helper()
	store := newFakeUserStore()
	companies := newFakeCompanyStore()
	svc := NewAuthService(store, companies, "segredo-de-teste", time.Hour, slog.Default())
	if err := svc.EnsureBootstrapAdmin(context.Background(), "admin@ndovu.local", "admin12345"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return svc, store
}

func TestLoginEVerify(t *testing.T) {
	svc, _ := newAuthForTest(t)
	ctx := context.Background()

	res, err := svc.Login(ctx, "admin@ndovu.local", "admin12345")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.Token == "" || res.User.Role != domain.RoleAdmin {
		t.Fatalf("resultado inesperado: %+v", res)
	}

	id, err := svc.Verify(ctx, res.Token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.Email != "admin@ndovu.local" || id.Role != domain.RoleAdmin {
		t.Fatalf("identidade inesperada: %+v", id)
	}
}

func TestLoginRecusado(t *testing.T) {
	svc, _ := newAuthForTest(t)
	ctx := context.Background()

	if _, err := svc.Login(ctx, "admin@ndovu.local", "senha-errada"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("senha errada: esperado ErrUnauthorized, veio %v", err)
	}
	if _, err := svc.Login(ctx, "nao-existe@x.com", "qualquer1"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("usuário inexistente: esperado ErrUnauthorized, veio %v", err)
	}
	if _, err := svc.Verify(ctx, "token-invalido"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("token inválido: esperado ErrUnauthorized, veio %v", err)
	}
}

func TestUsuarioInativoNaoLoga(t *testing.T) {
	svc, store := newAuthForTest(t)
	ctx := context.Background()

	viewer, err := svc.CreateUser(ctx, CreateUserInput{
		Email: "v@x.com", Name: "Viewer", Password: "12345678", Role: domain.RoleViewer, CompanyID: "c1",
	})
	if err != nil {
		t.Fatalf("criando viewer: %v", err)
	}
	inactive := false
	if _, err := store.UpdateUser(ctx, viewer.ID, nil, &inactive, nil, nil); err != nil {
		t.Fatalf("desativando: %v", err)
	}
	if _, err := svc.Login(ctx, "v@x.com", "12345678"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("inativo: esperado ErrUnauthorized, veio %v", err)
	}
}

func TestNaoRemoveUltimoAdmin(t *testing.T) {
	svc, store := newAuthForTest(t)
	ctx := context.Background()

	var adminID string
	for id, u := range store.users {
		if u.Role == domain.RoleAdmin {
			adminID = id
		}
	}
	viewer := domain.RoleViewer
	if _, err := svc.UpdateUser(ctx, adminID, UpdateUserInput{Role: &viewer}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("rebaixar último admin: esperado ErrValidation, veio %v", err)
	}
	inactive := false
	if _, err := svc.UpdateUser(ctx, adminID, UpdateUserInput{Active: &inactive}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("desativar último admin: esperado ErrValidation, veio %v", err)
	}
}

func TestCreateUserValidacao(t *testing.T) {
	svc, _ := newAuthForTest(t)
	ctx := context.Background()

	_, err := svc.CreateUser(ctx, CreateUserInput{Email: "x@x.com", Name: "X", Password: "curta", Role: domain.RoleViewer})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("senha curta: esperado ErrValidation, veio %v", err)
	}
	_, err = svc.CreateUser(ctx, CreateUserInput{Email: "admin@ndovu.local", Name: "Dup", Password: "12345678", Role: domain.RoleViewer, CompanyID: "c1"})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("e-mail duplicado: esperado ErrConflict, veio %v", err)
	}
}

// fakeKeyStore implementa domain.APIKeyStore em memória.
type fakeKeyStore struct {
	keys   map[string]domain.APIKey // por id
	hashes map[string]string        // id -> hash
	seq    int
	finds  int
}

func newFakeKeyStore() *fakeKeyStore {
	return &fakeKeyStore{keys: map[string]domain.APIKey{}, hashes: map[string]string{}}
}

func (f *fakeKeyStore) CreateAPIKey(_ context.Context, k domain.APIKey, hash, _ string) (domain.APIKey, error) {
	f.seq++
	k.ID = string(rune('A' + f.seq))
	k.Active = true
	k.CreatedAt = time.Now()
	f.keys[k.ID] = k
	f.hashes[k.ID] = hash
	return k, nil
}

func (f *fakeKeyStore) ListAPIKeys(_ context.Context) ([]domain.APIKey, error) {
	out := []domain.APIKey{}
	for _, k := range f.keys {
		out = append(out, k)
	}
	return out, nil
}

func (f *fakeKeyStore) RevokeAPIKey(_ context.Context, id string) error {
	k, ok := f.keys[id]
	if !ok || !k.Active {
		return domain.ErrNotFound
	}
	k.Active = false
	f.keys[id] = k
	return nil
}

func (f *fakeKeyStore) FindActiveKeyByHash(_ context.Context, hash string) (domain.APIKey, error) {
	f.finds++
	for id, h := range f.hashes {
		if h == hash && f.keys[id].Active {
			return f.keys[id], nil
		}
	}
	return domain.APIKey{}, domain.ErrNotFound
}

func TestAPIKeyCicloDeVida(t *testing.T) {
	store := newFakeKeyStore()
	svc := NewAPIKeyService(store, time.Minute, 0, slog.Default())
	ctx := context.Background()

	created, err := svc.CreateKey(ctx, "", "portal-cliente", "chave web", "u1")
	if err != nil {
		t.Fatalf("criando: %v", err)
	}
	if !strings.HasPrefix(created.Key, "ndk_") || created.Prefix != created.Key[:12] {
		t.Fatalf("formato inesperado: %+v", created)
	}

	// valida (vai ao store) e revalida (cache: não vai ao store)
	if _, err := svc.Validate(ctx, created.Key); err != nil {
		t.Fatalf("validando: %v", err)
	}
	before := store.finds
	if _, err := svc.Validate(ctx, created.Key); err != nil {
		t.Fatalf("revalidando: %v", err)
	}
	if store.finds != before {
		t.Fatalf("cache não usado: finds %d → %d", before, store.finds)
	}

	// chave errada é recusada
	if _, err := svc.Validate(ctx, "ndk_invalida"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("chave inválida: esperado ErrUnauthorized, veio %v", err)
	}

	// revogação vale imediatamente (cache derrubado)
	if err := svc.RevokeKey(ctx, created.ID); err != nil {
		t.Fatalf("revogando: %v", err)
	}
	if _, err := svc.Validate(ctx, created.Key); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("chave revogada: esperado ErrUnauthorized, veio %v", err)
	}
}
