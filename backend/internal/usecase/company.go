package usecase

import (
	"context"
	"log/slog"
	"strings"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// CompanyService faz o CRUD de empresas do control plane.
type CompanyService struct {
	store  domain.CompanyStore
	logger *slog.Logger
}

// NewCompanyService cria o serviço de empresas.
func NewCompanyService(store domain.CompanyStore, logger *slog.Logger) *CompanyService {
	return &CompanyService{store: store, logger: logger}
}

// CreateCompanyInput são os dados de cadastro de uma empresa.
type CreateCompanyInput struct {
	Name     string
	Document string
	Active   bool
}

// Create cadastra uma empresa. Nome duplicado vira ErrConflict.
func (s *CompanyService) Create(ctx context.Context, in CreateCompanyInput) (domain.Company, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return domain.Company{}, domain.NewValidationError("name é obrigatório")
	}
	c, err := s.store.CreateCompany(ctx, domain.Company{
		Name:     name,
		Document: strings.TrimSpace(in.Document),
		Active:   in.Active,
	})
	if err != nil {
		return domain.Company{}, err
	}
	s.logger.InfoContext(ctx, "empresa cadastrada", "company", c.Name, "id", c.ID)
	return c, nil
}

// List retorna todas as empresas.
func (s *CompanyService) List(ctx context.Context) ([]domain.Company, error) {
	return s.store.ListCompanies(ctx)
}

// Get retorna uma empresa por id.
func (s *CompanyService) Get(ctx context.Context, id string) (domain.Company, error) {
	return s.store.GetCompany(ctx, id)
}

// UpdateCompanyInput são os campos editáveis de uma empresa.
type UpdateCompanyInput struct {
	Name     *string
	Document *string
	Active   *bool
}

// Update altera os metadados de uma empresa.
func (s *CompanyService) Update(ctx context.Context, id string, in UpdateCompanyInput) (domain.Company, error) {
	cur, err := s.store.GetCompany(ctx, id)
	if err != nil {
		return domain.Company{}, err
	}
	if in.Name != nil {
		cur.Name = strings.TrimSpace(*in.Name)
	}
	if in.Document != nil {
		cur.Document = strings.TrimSpace(*in.Document)
	}
	if in.Active != nil {
		cur.Active = *in.Active
	}
	if cur.Name == "" {
		return domain.Company{}, domain.NewValidationError("name não pode ser vazio")
	}
	return s.store.UpdateCompany(ctx, id, cur)
}

// Delete remove uma empresa. O store bloqueia se ainda houver usuários vinculados.
func (s *CompanyService) Delete(ctx context.Context, id string) error {
	return s.store.DeleteCompany(ctx, id)
}
