package usecase

import (
	"context"
	"log/slog"
	"strings"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// GDPRService atende os direitos LGPD/GDPR mais comuns:
//   - portabilidade: exportar todos os eventos de um user
//   - direito ao esquecimento: apagar todos os eventos de um user
//
// A gravação em audit log é feita pelo handler (ele tem acesso à identidade
// do requerente + IP).
type GDPRService struct {
	reader domain.EventReader
	logger *slog.Logger
}

// NewGDPRService cria o serviço.
func NewGDPRService(reader domain.EventReader, logger *slog.Logger) *GDPRService {
	return &GDPRService{reader: reader, logger: logger}
}

// Export retorna todos os eventos do user (portabilidade). O caller entrega
// como JSON download.
func (s *GDPRService) Export(ctx context.Context, userID string) ([]domain.TraceEvent, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, domain.NewValidationError("userId é obrigatório")
	}
	return s.reader.FindEventsByUser(ctx, userID)
}

// Forget dispara o DELETE. Retorna imediatamente (ClickHouse aplica async);
// o caller já pode responder 202 Accepted.
func (s *GDPRService) Forget(ctx context.Context, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return domain.NewValidationError("userId é obrigatório")
	}
	if err := s.reader.DeleteEventsByUser(ctx, userID); err != nil {
		return err
	}
	s.logger.InfoContext(ctx, "GDPR: forget dispatched", "user_id", userID)
	return nil
}
