package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// Testes adicionais para SavedViewService cobrindo validações que faltavam
// em savedviews_test.go (name/viewType/owner vazios, trim, delegação).

func TestSavedView_CreateValidaCamposObrigatorios(t *testing.T) {
	store := &fakeSavedViewStore{}
	svc := NewSavedViewService(store)

	cases := []struct {
		name string
		in   CreateSavedViewInput
	}{
		{"name vazio", CreateSavedViewInput{
			OwnerUserID: "u", OwnerRole: domain.RoleViewer, ViewType: "traces",
		}},
		{"name só espaço", CreateSavedViewInput{
			OwnerUserID: "u", OwnerRole: domain.RoleViewer, ViewType: "traces", Name: "   ",
		}},
		{"viewType vazio", CreateSavedViewInput{
			OwnerUserID: "u", OwnerRole: domain.RoleViewer, Name: "x",
		}},
		{"owner vazio", CreateSavedViewInput{
			OwnerRole: domain.RoleViewer, ViewType: "traces", Name: "x",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.Create(context.Background(), c.in)
			if err == nil {
				t.Fatal("esperava erro de validação")
			}
			var ve *domain.ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("esperava ValidationError, veio %T", err)
			}
		})
	}
}

func TestSavedView_CreateTrimNomeEViewType(t *testing.T) {
	store := &fakeSavedViewStore{}
	svc := NewSavedViewService(store)
	_, err := svc.Create(context.Background(), CreateSavedViewInput{
		OwnerUserID: "u", OwnerRole: domain.RoleViewer,
		ViewType: "  traces  ", Name: "  minha view  ",
	})
	if err != nil {
		t.Fatalf("criar falhou: %v", err)
	}
	if store.created.Name != "minha view" || store.created.ViewType != "traces" {
		t.Errorf("trim não aplicado: name=%q viewType=%q",
			store.created.Name, store.created.ViewType)
	}
}

func TestSavedView_UpdateNameVazioRejeita(t *testing.T) {
	store := &fakeSavedViewStore{}
	svc := NewSavedViewService(store)
	_, err := svc.Update(context.Background(), "id", "owner", UpdateSavedViewInput{
		Name: "   ", OwnerRole: domain.RoleAdmin,
	})
	if err == nil {
		t.Fatal("esperava erro em name vazio")
	}
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("esperava ValidationError, veio %T", err)
	}
}

func TestSavedView_UpdateTrimNome(t *testing.T) {
	store := &fakeSavedViewStore{}
	svc := NewSavedViewService(store)
	_, err := svc.Update(context.Background(), "id", "owner", UpdateSavedViewInput{
		Name: "  atualizado  ", OwnerRole: domain.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("update falhou: %v", err)
	}
	if store.updated.name != "atualizado" {
		t.Errorf("trim não aplicado: %q", store.updated.name)
	}
}

func TestSavedView_UpdateAdminPodeCompartilhar(t *testing.T) {
	store := &fakeSavedViewStore{}
	svc := NewSavedViewService(store)
	_, err := svc.Update(context.Background(), "id", "owner", UpdateSavedViewInput{
		Name: "x", IsShared: true, OwnerRole: domain.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("admin deveria conseguir compartilhar: %v", err)
	}
	if !store.updated.isShared {
		t.Error("store não recebeu isShared=true")
	}
}

func TestSavedView_DeleteDelegaProStore(t *testing.T) {
	// Ownership real é enforcado no store (WHERE owner_user_id = ?).
	// Aqui só validamos que o service delega.
	store := &fakeSavedViewStore{}
	svc := NewSavedViewService(store)
	if err := svc.Delete(context.Background(), "id-1", "owner-1"); err != nil {
		t.Errorf("delete não deveria falhar: %v", err)
	}
}

func TestSavedView_MensagemDeErroCitaEmpresa(t *testing.T) {
	// Regressão: mensagem do erro deve informar claramente que só editor+admin
	// pode compartilhar — pra facilitar debugging pelo dashboard.
	store := &fakeSavedViewStore{}
	svc := NewSavedViewService(store)
	_, err := svc.Create(context.Background(), CreateSavedViewInput{
		OwnerUserID: "u", OwnerRole: domain.RoleViewer,
		ViewType: "traces", Name: "shared", IsShared: true,
	})
	var ve *domain.ValidationError
	_ = errors.As(err, &ve)
	if !strings.Contains(strings.Join(ve.Issues, "|"), "editor ou admin") {
		t.Errorf("mensagem não menciona editor/admin: %v", ve.Issues)
	}
}
