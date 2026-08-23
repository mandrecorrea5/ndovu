package usecase

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// AuditService encapsula gravação e consulta de auditoria. A gravação é
// best-effort: se o Postgres estiver fora do ar, o handler não trava — só
// loga warn. A consulta é usada pela tela /admin/audit-log.
type AuditService struct {
	store  domain.AuditStore
	logger *slog.Logger
}

// NewAuditService cria o serviço.
func NewAuditService(store domain.AuditStore, logger *slog.Logger) *AuditService {
	return &AuditService{store: store, logger: logger}
}

// RecordInput é o payload de gravação — reflete AuditEntry mas facilita o
// caller preencher só o que interessa.
type RecordInput struct {
	Actor        domain.Identity
	Action       string
	ResourceType string
	ResourceID   string
	Details      any // serializado em JSON; pode ser struct ou map
	IP           string
	UserAgent    string
}

// Record grava com log de warning em caso de falha (não propaga o erro
// para não travar a mutação original).
func (s *AuditService) Record(ctx context.Context, in RecordInput) {
	var details json.RawMessage
	if in.Details != nil {
		b, err := json.Marshal(in.Details)
		if err == nil {
			details = b
		}
	}
	err := s.store.RecordAudit(ctx, domain.AuditEntry{
		ActorUserID:  in.Actor.UserID,
		ActorEmail:   in.Actor.Email,
		Action:       in.Action,
		ResourceType: in.ResourceType,
		ResourceID:   in.ResourceID,
		Details:      details,
		IP:           in.IP,
		UserAgent:    in.UserAgent,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "audit falhou (não bloqueia a operação)",
			"action", in.Action, "err", err)
	}
}

// List devolve entradas paginadas + total.
func (s *AuditService) List(ctx context.Context, f domain.AuditFilter) ([]domain.AuditEntry, int, error) {
	return s.store.ListAudit(ctx, f)
}
