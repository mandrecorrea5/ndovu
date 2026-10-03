package usecase

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

type fakeAppStore struct {
	apps    []domain.App
	created []domain.App
	deleted []string
	failOn  error
}

func (f *fakeAppStore) CreateApp(_ context.Context, a domain.App) (domain.App, error) {
	if f.failOn != nil {
		return domain.App{}, f.failOn
	}
	a.ID = "app-1"
	a.CreatedAt = time.Now()
	a.UpdatedAt = time.Now()
	f.created = append(f.created, a)
	f.apps = append(f.apps, a)
	return a, nil
}
func (f *fakeAppStore) GetApp(_ context.Context, id string) (domain.App, error) {
	for _, a := range f.apps {
		if a.ID == id {
			return a, nil
		}
	}
	return domain.App{}, domain.ErrNotFound
}
func (f *fakeAppStore) ListApps(_ context.Context) ([]domain.App, error) { return f.apps, nil }

func (f *fakeAppStore) ListAppNamesByCompany(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (f *fakeAppStore) ListAppsByCompany(_ context.Context, _ string) ([]domain.App, error) {
	return f.apps, nil
}

func (f *fakeAppStore) GetAppByName(_ context.Context, name string) (domain.App, error) {
	for _, a := range f.apps {
		if a.Name == name {
			return a, nil
		}
	}
	return domain.App{}, domain.ErrNotFound
}

func (f *fakeAppStore) UpdateApp(_ context.Context, id string, a domain.App) (domain.App, error) {
	return a, nil
}
func (f *fakeAppStore) DeleteApp(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeAppKeyStore struct {
	keys    []domain.APIKey
	revoked []string
}

func (f *fakeAppKeyStore) CreateAPIKey(_ context.Context, k domain.APIKey, keyHash, createdBy string) (domain.APIKey, error) {
	k.ID = "key-1"
	k.Prefix = "ndk_abcdef"
	f.keys = append(f.keys, k)
	return k, nil
}
func (f *fakeAppKeyStore) ListAPIKeys(_ context.Context) ([]domain.APIKey, error) { return f.keys, nil }
func (f *fakeAppKeyStore) ListAPIKeysByCompany(_ context.Context, _ string) ([]domain.APIKey, error) {
	return f.keys, nil
}
func (f *fakeAppKeyStore) GetAPIKeyByID(_ context.Context, id string) (domain.APIKey, error) {
	for _, k := range f.keys {
		if k.ID == id {
			return k, nil
		}
	}
	return domain.APIKey{}, domain.ErrNotFound
}
func (f *fakeAppKeyStore) RevokeAPIKey(_ context.Context, id string) error {
	f.revoked = append(f.revoked, id)
	return nil
}
func (f *fakeAppKeyStore) FindActiveKeyByHash(_ context.Context, _ string) (domain.APIKey, error) {
	return domain.APIKey{}, domain.ErrNotFound
}

func newTestAppService(apps *fakeAppStore, keys *fakeAppKeyStore) *AppService {
	return NewAppService(apps, NewAPIKeyService(keys, 0, 0, slog.Default(), "test-secret"), slog.Default())
}

func TestAppCreateGeraChave(t *testing.T) {
	apps := &fakeAppStore{}
	keys := &fakeAppKeyStore{}
	svc := newTestAppService(apps, keys)

	created, err := svc.Create(context.Background(), CreateAppInput{
		Name: "portal-cliente", Technology: "React", Company: "ACME", Responsible: "ana@acme.com",
	}, "user-1")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if created.Name != "portal-cliente" || created.Technology != "React" {
		t.Fatalf("app não preenchido: %+v", created)
	}
	if created.Key == "" {
		t.Fatal("esperado chave gerada no cadastro")
	}
	if len(keys.keys) != 1 || keys.keys[0].AppID != "app-1" || keys.keys[0].App != "portal-cliente" {
		t.Fatalf("chave não vinculada ao app: %+v", keys.keys)
	}
}

func TestAppCreateSemNome(t *testing.T) {
	svc := newTestAppService(&fakeAppStore{}, &fakeAppKeyStore{})
	_, err := svc.Create(context.Background(), CreateAppInput{Name: "  "}, "u")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("esperado ErrValidation, veio: %v", err)
	}
}

func TestAppDeleteRevogaChaves(t *testing.T) {
	apps := &fakeAppStore{}
	keys := &fakeAppKeyStore{
		keys: []domain.APIKey{
			{ID: "k1", AppID: "app-1", Active: true},
			{ID: "k2", AppID: "app-1", Active: true},
			{ID: "k3", AppID: "app-2", Active: true},
		},
	}
	svc := newTestAppService(apps, keys)

	if err := svc.Delete(context.Background(), "app-1"); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(keys.revoked) != 2 {
		t.Fatalf("esperado revogar 2 chaves do app, veio %+v", keys.revoked)
	}
	if len(apps.deleted) != 1 || apps.deleted[0] != "app-1" {
		t.Fatalf("app não deletado: %+v", apps.deleted)
	}
}
