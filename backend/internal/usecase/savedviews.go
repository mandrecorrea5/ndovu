package usecase

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// SavedViewService faz o CRUD de views nomeadas. Cada tela do dashboard
// decide o formato de "filters" que salva — o serviço só valida obrigatórios.
type SavedViewService struct {
	store domain.SavedViewStore
}

// NewSavedViewService cria o serviço.
func NewSavedViewService(store domain.SavedViewStore) *SavedViewService {
	return &SavedViewService{store: store}
}

// CreateSavedViewInput são os dados de criação.
type CreateSavedViewInput struct {
	OwnerUserID string
	OwnerRole   domain.Role // viewer só cria privadas; editor+admin podem compartilhar
	ViewType    string
	Name        string
	Filters     json.RawMessage
	IsShared    bool
}

// Create insere uma view. Nome obrigatório; se filters vier vazio, salvamos {}.
func (s *SavedViewService) Create(ctx context.Context, in CreateSavedViewInput) (domain.SavedView, error) {
	var issues []string
	if strings.TrimSpace(in.Name) == "" {
		issues = append(issues, "name é obrigatório")
	}
	if strings.TrimSpace(in.ViewType) == "" {
		issues = append(issues, "viewType é obrigatório")
	}
	if in.OwnerUserID == "" {
		issues = append(issues, "usuário autenticado ausente")
	}
	// Viewer nunca pode compartilhar view — mantém-a sempre privada.
	if in.IsShared && !in.OwnerRole.AtLeastEditor() {
		issues = append(issues, "apenas editor ou admin pode compartilhar views")
	}
	if len(issues) > 0 {
		return domain.SavedView{}, domain.NewValidationError(issues...)
	}
	return s.store.CreateSavedView(ctx, domain.SavedView{
		OwnerUserID: in.OwnerUserID,
		ViewType:    strings.TrimSpace(in.ViewType),
		Name:        strings.TrimSpace(in.Name),
		Filters:     in.Filters,
		IsShared:    in.IsShared,
	})
}

// UpdateSavedViewInput são os campos editáveis.
type UpdateSavedViewInput struct {
	Name      string
	Filters   json.RawMessage
	IsShared  bool
	OwnerRole domain.Role // viewer não pode transformar sua view em compartilhada
}

// Update só o dono. O store enforça isso via WHERE.
func (s *SavedViewService) Update(ctx context.Context, id, ownerID string, in UpdateSavedViewInput) (domain.SavedView, error) {
	if strings.TrimSpace(in.Name) == "" {
		return domain.SavedView{}, domain.NewValidationError("name não pode ser vazio")
	}
	if in.IsShared && !in.OwnerRole.AtLeastEditor() {
		return domain.SavedView{}, domain.NewValidationError("apenas editor ou admin pode compartilhar views")
	}
	return s.store.UpdateSavedView(ctx, id, ownerID, strings.TrimSpace(in.Name), in.Filters, in.IsShared)
}

// Delete só o dono.
func (s *SavedViewService) Delete(ctx context.Context, id, ownerID string) error {
	return s.store.DeleteSavedView(ctx, id, ownerID)
}

// List devolve as minhas + as compartilhadas por outros, opcionalmente filtradas por tipo.
func (s *SavedViewService) List(ctx context.Context, ownerID, viewType string) ([]domain.SavedView, error) {
	return s.store.ListSavedViews(ctx, ownerID, viewType)
}
