package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// fakeSavedViewStore só devolve o que recebeu, sem tocar em SQL.
type fakeSavedViewStore struct {
	created domain.SavedView
	updated struct {
		id, owner, name string
		isShared        bool
	}
}

func (f *fakeSavedViewStore) CreateSavedView(_ context.Context, v domain.SavedView) (domain.SavedView, error) {
	v.ID = "generated"
	f.created = v
	return v, nil
}
func (f *fakeSavedViewStore) UpdateSavedView(_ context.Context, id, owner, name string, _ json.RawMessage, isShared bool) (domain.SavedView, error) {
	f.updated.id = id
	f.updated.owner = owner
	f.updated.name = name
	f.updated.isShared = isShared
	return domain.SavedView{ID: id, OwnerUserID: owner, Name: name, IsShared: isShared}, nil
}
func (f *fakeSavedViewStore) DeleteSavedView(context.Context, string, string) error { return nil }
func (f *fakeSavedViewStore) ListSavedViews(context.Context, string, string) ([]domain.SavedView, error) {
	return nil, nil
}

// TestSavedView_ViewerNaoPodeCompartilhar_NaCriacao garante que viewer
// tentando criar view com isShared=true recebe erro.
func TestSavedView_ViewerNaoPodeCompartilhar_NaCriacao(t *testing.T) {
	store := &fakeSavedViewStore{}
	svc := NewSavedViewService(store)

	_, err := svc.Create(context.Background(), CreateSavedViewInput{
		OwnerUserID: "u-viewer",
		OwnerRole:   domain.RoleViewer,
		ViewType:    "traces",
		Name:        "meus favoritos",
		IsShared:    true,
	})
	if err == nil {
		t.Fatal("esperava erro ao viewer compartilhar view")
	}
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("esperava ValidationError, veio %T: %v", err, err)
	}
	if !strings.Contains(strings.Join(ve.Issues, "; "), "editor ou admin") {
		t.Fatalf("mensagem inesperada nos issues: %v", ve.Issues)
	}
}

// TestSavedView_ViewerPodeCriarPrivada — o caso normal do viewer.
func TestSavedView_ViewerPodeCriarPrivada(t *testing.T) {
	store := &fakeSavedViewStore{}
	svc := NewSavedViewService(store)

	v, err := svc.Create(context.Background(), CreateSavedViewInput{
		OwnerUserID: "u-viewer",
		OwnerRole:   domain.RoleViewer,
		ViewType:    "traces",
		Name:        "meus favoritos",
		IsShared:    false,
	})
	if err != nil {
		t.Fatalf("viewer deveria criar view privada, veio: %v", err)
	}
	if v.IsShared {
		t.Fatal("view não deveria ter sido gravada como compartilhada")
	}
}

// TestSavedView_EditorEAdminPodemCompartilhar.
func TestSavedView_EditorEAdminPodemCompartilhar(t *testing.T) {
	for _, role := range []domain.Role{domain.RoleEditor, domain.RoleAdmin} {
		store := &fakeSavedViewStore{}
		svc := NewSavedViewService(store)
		_, err := svc.Create(context.Background(), CreateSavedViewInput{
			OwnerUserID: "u",
			OwnerRole:   role,
			ViewType:    "traces",
			Name:        "view compartilhada",
			IsShared:    true,
		})
		if err != nil {
			t.Fatalf("role=%s deveria compartilhar, veio: %v", role, err)
		}
		if !store.created.IsShared {
			t.Fatalf("role=%s: store não recebeu IsShared=true", role)
		}
	}
}

// TestSavedView_UpdateBloqueiaViewerCompartilhando garante que viewer
// tentando transformar sua view privada em compartilhada (via PATCH) é
// bloqueado antes de chegar ao store.
func TestSavedView_UpdateBloqueiaViewerCompartilhando(t *testing.T) {
	store := &fakeSavedViewStore{}
	svc := NewSavedViewService(store)

	_, err := svc.Update(context.Background(), "view-1", "u-viewer", UpdateSavedViewInput{
		Name:      "nome",
		IsShared:  true,
		OwnerRole: domain.RoleViewer,
	})
	if err == nil {
		t.Fatal("esperava erro no update")
	}
	if store.updated.id != "" {
		t.Fatal("store não deveria ter sido chamado")
	}
}
