package usecase

import (
	"context"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// RetentionService encapsula a query de retenção. Sem CRUD por enquanto —
// a análise é on-demand com parâmetros vindos da UI.
type RetentionService struct {
	reader domain.EventReader
}

// NewRetentionService cria o serviço.
func NewRetentionService(reader domain.EventReader) *RetentionService {
	return &RetentionService{reader: reader}
}

// Query executa a matriz cohort × Dn.
func (s *RetentionService) Query(ctx context.Context, f domain.RetentionFilter) (domain.RetentionResult, error) {
	return s.reader.Retention(ctx, f)
}
