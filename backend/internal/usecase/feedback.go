package usecase

import (
	"context"
	"log/slog"
	"strings"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// FeedbackService faz o CRUD/triagem de feedbacks. Ingest é público (usa a
// X-Api-Key do SDK); consulta/triagem exige bearer (backoffice).
type FeedbackService struct {
	store  domain.FeedbackStore
	logger *slog.Logger
}

// NewFeedbackService cria o serviço.
func NewFeedbackService(store domain.FeedbackStore, logger *slog.Logger) *FeedbackService {
	return &FeedbackService{store: store, logger: logger}
}

// FeedbackInput é o payload vindo do widget SDK.
type FeedbackInput struct {
	App       string
	SessionID string
	EventID   string
	UserID    string
	Type      domain.FeedbackType
	Message   string
	Email     string
	URL       string
	ViewportW int
	ViewportH int
}

// Ingest grava. Type default = "bug"; message obrigatório. Limite generoso
// (~5k chars) — mensagens muito longas geralmente são spam/paste acidental.
func (s *FeedbackService) Ingest(ctx context.Context, in FeedbackInput) (domain.UserFeedback, error) {
	var issues []string
	if strings.TrimSpace(in.App) == "" {
		issues = append(issues, "app é obrigatório")
	}
	if strings.TrimSpace(in.SessionID) == "" {
		issues = append(issues, "sessionId é obrigatório")
	}
	msg := strings.TrimSpace(in.Message)
	if msg == "" {
		issues = append(issues, "message é obrigatório")
	}
	if len(msg) > 5000 {
		issues = append(issues, "message excede 5000 caracteres")
	}
	if in.Type == "" {
		in.Type = domain.FeedbackBug
	}
	if !in.Type.Valid() {
		issues = append(issues, "type deve ser bug|suggestion|praise|other")
	}
	if len(issues) > 0 {
		return domain.UserFeedback{}, domain.NewValidationError(issues...)
	}
	fb, err := s.store.CreateFeedback(ctx, domain.UserFeedback{
		App: in.App, SessionID: in.SessionID, EventID: in.EventID,
		UserID: in.UserID, Type: in.Type, Message: msg,
		Email: in.Email, URL: in.URL,
		ViewportW: in.ViewportW, ViewportH: in.ViewportH,
	})
	if err != nil {
		return domain.UserFeedback{}, err
	}
	s.logger.InfoContext(ctx, "feedback recebido",
		"app", fb.App, "type", fb.Type, "session_id", fb.SessionID)
	return fb, nil
}

// List devolve página + total.
func (s *FeedbackService) List(ctx context.Context, f domain.FeedbackFilter) ([]domain.UserFeedback, int, error) {
	return s.store.ListFeedbacks(ctx, f)
}

// Get resolve um feedback por id (ownership check no admin).
func (s *FeedbackService) Get(ctx context.Context, id string) (domain.UserFeedback, error) {
	return s.store.GetFeedback(ctx, id)
}

// SetStatus muda status; resolvedBy = user do backoffice que triageou.
func (s *FeedbackService) SetStatus(ctx context.Context, id string, status domain.FeedbackStatus, resolvedBy string) (domain.UserFeedback, error) {
	if !status.Valid() {
		return domain.UserFeedback{}, domain.NewValidationError("status inválido")
	}
	return s.store.UpdateFeedbackStatus(ctx, id, status, resolvedBy)
}

// Delete apaga.
func (s *FeedbackService) Delete(ctx context.Context, id string) error {
	return s.store.DeleteFeedback(ctx, id)
}
