// Package usecase orquestra as regras de aplicação sobre o domínio.
package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// AppService gerencia o cadastro de apps emissores (frontends) e a geração da
// chave de API de cada um. Criar um app gera a primeira chave — exibida uma
// única vez para ser enviada ao responsável da integração.
type AppService struct {
	apps   domain.AppStore
	keys   *APIKeyService
	logger *slog.Logger
}

// NewAppService cria o serviço de apps.
func NewAppService(apps domain.AppStore, keys *APIKeyService, logger *slog.Logger) *AppService {
	return &AppService{apps: apps, keys: keys, logger: logger}
}

// CreateAppInput são os dados de cadastro de um app.
type CreateAppInput struct {
	Name        string
	Technology  string
	Company     string // rótulo textual (usado quando não há CompanyID)
	CompanyID   string
	Responsible string
}

// CreatedApp é o resultado do cadastro: o app + a chave gerada (única exibição).
type CreatedApp struct {
	domain.App
	Key string `json:"key"`
}

// Create cadastra um app e gera a primeira chave de API para ele. Se a geração
// da chave falhar, o app é removido (rollback best-effort) para não deixar
// cadastro órfão.
func (s *AppService) Create(ctx context.Context, in CreateAppInput, createdBy string) (CreatedApp, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return CreatedApp{}, domain.NewValidationError("name é obrigatório")
	}

	app, err := s.apps.CreateApp(ctx, domain.App{
		Name:        name,
		Technology:  strings.TrimSpace(in.Technology),
		Company:     strings.TrimSpace(in.Company),
		CompanyID:   strings.TrimSpace(in.CompanyID),
		Responsible: strings.TrimSpace(in.Responsible),
	})
	if err != nil {
		return CreatedApp{}, err
	}

	key, err := s.keys.CreateKey(ctx, app.ID, app.Name, "", createdBy)
	if err != nil {
		_ = s.apps.DeleteApp(ctx, app.ID)
		return CreatedApp{}, fmt.Errorf("gerando chave do app: %w", err)
	}

	s.logger.InfoContext(ctx, "app cadastrado", "app", app.Name, "app_id", app.ID)
	return CreatedApp{App: app, Key: key.Key}, nil
}

// List retorna todos os apps cadastrados.
func (s *AppService) List(ctx context.Context) ([]domain.App, error) {
	return s.apps.ListApps(ctx)
}

// Get retorna um app por id.
func (s *AppService) Get(ctx context.Context, id string) (domain.App, error) {
	return s.apps.GetApp(ctx, id)
}

// UpdateAppInput são os campos editáveis de um app.
type UpdateAppInput struct {
	Name        *string
	Technology  *string
	Company     *string
	CompanyID   *string
	Responsible *string
}

// Update altera os metadados de um app.
func (s *AppService) Update(ctx context.Context, id string, in UpdateAppInput) (domain.App, error) {
	cur, err := s.apps.GetApp(ctx, id)
	if err != nil {
		return domain.App{}, err
	}
	if in.Name != nil {
		cur.Name = strings.TrimSpace(*in.Name)
	}
	if in.Technology != nil {
		cur.Technology = strings.TrimSpace(*in.Technology)
	}
	if in.Company != nil {
		cur.Company = strings.TrimSpace(*in.Company)
	}
	if in.CompanyID != nil {
		cur.CompanyID = strings.TrimSpace(*in.CompanyID)
	}
	if in.Responsible != nil {
		cur.Responsible = strings.TrimSpace(*in.Responsible)
	}
	if cur.Name == "" {
		return domain.App{}, domain.NewValidationError("name não pode ser vazio")
	}
	return s.apps.UpdateApp(ctx, id, cur)
}

// Delete remove um app e revoga todas as suas chaves (efeito imediato na
// ingestão).
func (s *AppService) Delete(ctx context.Context, id string) error {
	if err := s.keys.RevokeByApp(ctx, id); err != nil {
		return err
	}
	return s.apps.DeleteApp(ctx, id)
}
