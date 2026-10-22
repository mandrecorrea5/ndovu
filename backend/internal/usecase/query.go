package usecase

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

const (
	defaultPageSize = 50
	maxPageSize     = 500
)

// QueryService expõe as consultas read-only da ferramenta de troubleshooting.
// Nenhum método aqui modifica dados — por construção, só recebe um EventReader.
type QueryService struct {
	reader    domain.EventReader
	feedbacks domain.FeedbackStore // opcional: usado como fallback em Session()
	spans     domain.SpanReader    // opcional: spans OTLP no waterfall do trace
}

// NewQueryService cria o serviço de consulta.
func NewQueryService(reader domain.EventReader) *QueryService {
	return &QueryService{reader: reader}
}

// WithFeedbackFallback registra o store de feedbacks. Quando uma consulta a
// GetSession não encontrar eventos no ClickHouse, o serviço tenta materializar
// a sessão a partir de feedbacks (widget do SDK pode ter criado a sessão sem
// nunca ter mandado eventos, ou o sampling agressivo pode ter descartado).
func (s *QueryService) WithFeedbackFallback(store domain.FeedbackStore) *QueryService {
	s.feedbacks = store
	return s
}

// WithSpans liga os spans OTLP à consulta de trace.
func (s *QueryService) WithSpans(reader domain.SpanReader) *QueryService {
	s.spans = reader
	return s
}

// Events lista eventos aplicando o filtro combinado do usuário.
func (s *QueryService) Events(ctx context.Context, f domain.EventFilter) (domain.EventPage, error) {
	f.Limit = clampLimit(f.Limit)
	return s.reader.FindEvents(ctx, f)
}

// Event retorna um evento único com todos os payloads.
func (s *QueryService) Event(ctx context.Context, id string) (domain.TraceEvent, error) {
	if id == "" {
		return domain.TraceEvent{}, domain.NewValidationError("id é obrigatório")
	}
	return s.reader.GetEvent(ctx, id)
}

// Sessions lista sessões (o agrupador do rastro).
func (s *QueryService) Sessions(ctx context.Context, f domain.SessionFilter) (domain.SessionPage, error) {
	f.Limit = clampLimit(f.Limit)
	return s.reader.FindSessions(ctx, f)
}

// SessionDetail é a sessão + seu rastro cronológico completo.
type SessionDetail struct {
	Session  domain.Session      `json:"session"`
	Timeline []domain.TraceEvent `json:"timeline"`
}

// Session retorna o rastro completo de uma sessão para troubleshooting.
// Se o ClickHouse não tiver eventos dessa sessão (foi criada só por widget
// de feedback, ou o sampling descartou), tenta materializar uma shell a
// partir do FeedbackStore — assim o deep-link do painel de feedbacks
// nunca cai em 404 quando a sessão existe conceitualmente em algum lugar.
func (s *QueryService) Session(ctx context.Context, sessionID string) (SessionDetail, error) {
	if sessionID == "" {
		return SessionDetail{}, domain.NewValidationError("sessionId é obrigatório")
	}
	sess, err := s.reader.GetSession(ctx, sessionID)
	if err != nil {
		// Não é ErrNotFound → erro real, propaga.
		if err != domain.ErrNotFound {
			return SessionDetail{}, err
		}
		// ErrNotFound: tenta fallback via feedback.
		shell, ok := s.sessionFromFeedback(ctx, sessionID)
		if !ok {
			return SessionDetail{}, domain.ErrNotFound
		}
		return SessionDetail{Session: shell, Timeline: []domain.TraceEvent{}}, nil
	}
	timeline, err := s.reader.SessionTimeline(ctx, sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	return SessionDetail{Session: sess, Timeline: timeline}, nil
}

// sessionFromFeedback tenta montar uma Session sintética quando o ClickHouse
// não conhece a sessão mas o FeedbackStore tem registro dela. Devolve
// (Session, true) se conseguiu, ou (_, false) se não há nenhum rastro.
func (s *QueryService) sessionFromFeedback(ctx context.Context, sessionID string) (domain.Session, bool) {
	if s.feedbacks == nil {
		return domain.Session{}, false
	}
	feedbacks, _, err := s.feedbacks.ListFeedbacks(ctx, domain.FeedbackFilter{
		SessionID: sessionID,
		Limit:     1,
	})
	if err != nil || len(feedbacks) == 0 {
		return domain.Session{}, false
	}
	fb := feedbacks[0]
	return domain.Session{
		SessionID:   fb.SessionID,
		UserID:      fb.UserID,
		App:         fb.App,
		StartedAt:   fb.CreatedAt,
		LastEventAt: fb.CreatedAt,
		EventCount:  0,
		ErrorCount:  0,
	}, true
}

// Overview agrega métricas da janela de tempo pedida.
func (s *QueryService) Overview(ctx context.Context, from, to time.Time, app string) (domain.Overview, error) {
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}
	if !from.Before(to) {
		return domain.Overview{}, domain.NewValidationError("'from' deve ser anterior a 'to'")
	}
	return s.reader.GetOverview(ctx, from, to, app)
}

// FilterOptions devolve os valores distintos existentes para montar filtros.
func (s *QueryService) FilterOptions(ctx context.Context) (domain.FilterOptions, error) {
	return s.reader.GetFilterOptions(ctx)
}

// WebVitals agrega LCP/CLS/INP/... por rota (p75, p95, good/poor counts).
func (s *QueryService) WebVitals(ctx context.Context, f domain.WebVitalFilter) ([]domain.WebVitalStat, error) {
	return s.reader.FindWebVitals(ctx, f)
}

// TraceView é o trace unificado: eventos do SDK (clique, fetch, erro JS,
// request do servidor) e spans OTLP do backend, ligados pelo mesmo trace_id.
// A montagem da árvore fica com quem exibe — os dois lados trazem
// span_id/parent_span_id.
type TraceView struct {
	TraceID string              `json:"traceId"`
	Events  []domain.TraceEvent `json:"events"`
	Spans   []domain.Span       `json:"spans"`
}

// Trace devolve o trace unificado, restrito aos apps do scope (vazio = sem
// restrição). Um trace atravessa apps — browser em um, servidor em outro —,
// e quem vê só um deles recebe só a parte dele.
func (s *QueryService) Trace(ctx context.Context, traceID string, scope []string) (TraceView, error) {
	traceID = strings.ToLower(strings.TrimSpace(traceID))
	if traceID == "" {
		return TraceView{}, domain.NewValidationError("traceId é obrigatório")
	}
	events, err := s.reader.TraceTimeline(ctx, traceID)
	if err != nil {
		return TraceView{}, err
	}
	view := TraceView{TraceID: traceID, Events: filterByScope(events, scope), Spans: []domain.Span{}}
	if s.spans != nil {
		spans, err := s.spans.TraceSpans(ctx, traceID, scope)
		if err != nil {
			return TraceView{}, err
		}
		view.Spans = spans
	}
	return view, nil
}

func filterByScope(events []domain.TraceEvent, scope []string) []domain.TraceEvent {
	if len(scope) == 0 {
		if events == nil {
			return []domain.TraceEvent{}
		}
		return events
	}
	out := make([]domain.TraceEvent, 0, len(events))
	for _, e := range events {
		if slices.Contains(scope, e.App) {
			out = append(out, e)
		}
	}
	return out
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultPageSize
	}
	if limit > maxPageSize {
		return maxPageSize
	}
	return limit
}
