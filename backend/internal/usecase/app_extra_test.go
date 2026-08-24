package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// Testes adicionais para AppService cobrindo métodos ByCompany e GetByName
// (adicionados no isolamento cross-tenant).

func TestApp_ListByCompany(t *testing.T) {
	apps := &fakeAppStore{
		apps: []domain.App{
			{ID: "a", Name: "app-a", CompanyID: "c-1"},
			{ID: "b", Name: "app-b", CompanyID: "c-1"},
		},
	}
	svc := newTestAppService(apps, &fakeAppKeyStore{})
	got, err := svc.ListByCompany(context.Background(), "c-1")
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("esperava 2 apps, veio %d", len(got))
	}
}

func TestApp_GetByNameOK(t *testing.T) {
	apps := &fakeAppStore{
		apps: []domain.App{
			{ID: "x", Name: "portal-alfa", CompanyID: "c-1"},
		},
	}
	svc := newTestAppService(apps, &fakeAppKeyStore{})
	got, err := svc.GetByName(context.Background(), "portal-alfa")
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if got.ID != "x" || got.CompanyID != "c-1" {
		t.Errorf("app errado: %+v", got)
	}
}

func TestApp_GetByNameInexistenteRetornaNotFound(t *testing.T) {
	svc := newTestAppService(&fakeAppStore{}, &fakeAppKeyStore{})
	_, err := svc.GetByName(context.Background(), "nao-existe")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestApp_GetPorIDInexistenteRetornaNotFound(t *testing.T) {
	svc := newTestAppService(&fakeAppStore{}, &fakeAppKeyStore{})
	_, err := svc.Get(context.Background(), "no-app")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestApp_UpdateTrimNome(t *testing.T) {
	apps := &fakeAppStore{apps: []domain.App{{ID: "app-1", Name: "old"}}}
	svc := newTestAppService(apps, &fakeAppKeyStore{})
	newName := "  novo-nome  "
	got, err := svc.Update(context.Background(), "app-1", UpdateAppInput{Name: &newName})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Name != "novo-nome" {
		t.Errorf("nome não foi trimmed: %q", got.Name)
	}
}

func TestApp_UpdateNomeVazioRejeita(t *testing.T) {
	// Se o nome vier explicitamente vazio (ou só espaço), Update deve falhar.
	apps := &fakeAppStore{apps: []domain.App{{ID: "app-1", Name: "old"}}}
	svc := newTestAppService(apps, &fakeAppKeyStore{})
	empty := "   "
	_, err := svc.Update(context.Background(), "app-1", UpdateAppInput{Name: &empty})
	if err == nil {
		t.Fatal("update com name vazio deveria falhar")
	}
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("esperava ValidationError, veio %T", err)
	}
}

func TestApp_UpdateAppInexistenteRetornaNotFound(t *testing.T) {
	svc := newTestAppService(&fakeAppStore{}, &fakeAppKeyStore{})
	name := "x"
	_, err := svc.Update(context.Background(), "no-app", UpdateAppInput{Name: &name})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}
