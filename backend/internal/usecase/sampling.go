package usecase

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/platform"
)

// SamplingService avalia regras de sampling contra eventos individuais.
// Cache in-memory (RWMutex) para não tocar Postgres em cada evento; refresh
// periódico via ticker no writer.
type SamplingService struct {
	store   domain.SamplingRuleStore
	metrics *platform.Metrics
	logger  *slog.Logger

	mu    sync.RWMutex
	rules []domain.SamplingRule // só as active=true, snapshot
}

// NewSamplingService cria o serviço. metrics é opcional (nil → sem métricas).
func NewSamplingService(store domain.SamplingRuleStore, metrics *platform.Metrics, logger *slog.Logger) *SamplingService {
	return &SamplingService{store: store, metrics: metrics, logger: logger}
}

// Refresh recarrega o snapshot de regras do Postgres. Chamado no boot e
// depois periodicamente pelo Run.
func (s *SamplingService) Refresh(ctx context.Context) error {
	all, err := s.store.ListSamplingRules(ctx)
	if err != nil {
		return err
	}
	active := make([]domain.SamplingRule, 0, len(all))
	for _, r := range all {
		if r.Active {
			active = append(active, r)
		}
	}
	s.mu.Lock()
	s.rules = active
	s.mu.Unlock()
	s.logger.Debug("sampling: snapshot atualizado", "active_rules", len(active))
	return nil
}

// Run inicia o refresh periódico. Bloqueia até ctx cancelar.
func (s *SamplingService) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if err := s.Refresh(ctx); err != nil {
		s.logger.WarnContext(ctx, "sampling: refresh inicial falhou", "err", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Refresh(ctx); err != nil {
				s.logger.WarnContext(ctx, "sampling: refresh falhou", "err", err)
			}
		}
	}
}

// ShouldKeep decide se um evento passa. Regra selecionada por precedência:
//  1. (app == e.App, event_type == e.Type)
//  2. (app == e.App, event_type == "")
//  3. (app == "",     event_type == e.Type)
//  4. (app == "",     event_type == "")
//  5. sem regra → mantém (rate implícito = 1.0).
//
// Se a regra escolhida tem keep_errors=true e o evento é erro (HasError ou
// http_status >= 500), passa sempre — não aplica sample_rate.
func (s *SamplingService) ShouldKeep(e domain.TraceEvent) bool {
	rule, matched := s.selectRule(e.App, string(e.Type))
	if !matched {
		return true
	}
	if rule.KeepErrors && isCriticalEvent(e) {
		s.count(e, "kept_error")
		return true
	}
	// Decisão random simples. Se quiser session-stable no futuro, hash o session_id.
	if rand.Float64() < rule.SampleRate { //nolint:gosec // sampling não é crypto
		s.count(e, "kept")
		return true
	}
	s.count(e, "dropped")
	return false
}

// FilterBatch aplica ShouldKeep em cada evento e devolve o subset mantido.
// Usado pelo writer antes do SaveBatches. Batches vazios são filtrados fora
// pelo caller (nada a persistir).
func (s *SamplingService) FilterBatch(batch domain.IngestBatch) domain.IngestBatch {
	if len(s.rules) == 0 {
		return batch
	}
	kept := make([]domain.TraceEvent, 0, len(batch.Events))
	for _, e := range batch.Events {
		if s.ShouldKeep(e) {
			kept = append(kept, e)
		}
	}
	batch.Events = kept
	return batch
}

// selectRule aplica a regra de precedência descrita em ShouldKeep.
// Iteração linear porque a lista é pequena (dezenas de regras no máximo).
func (s *SamplingService) selectRule(app, eventType string) (domain.SamplingRule, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var best domain.SamplingRule
	bestScore := -1
	for _, r := range s.rules {
		score, ok := matchScore(r, app, eventType)
		if !ok {
			continue
		}
		if score > bestScore {
			best = r
			bestScore = score
		}
	}
	return best, bestScore >= 0
}

// matchScore devolve a "especificidade" da regra vs (app, type):
//
//	2 = ambos batem literalmente
//	1 = um bate literalmente e o outro é wildcard
//	0 = ambos wildcards
//	-1 = não bate (ex.: regra fixa em app diferente)
func matchScore(r domain.SamplingRule, app, eventType string) (int, bool) {
	if r.App != "" && r.App != app {
		return -1, false
	}
	if r.EventType != "" && r.EventType != eventType {
		return -1, false
	}
	score := 0
	if r.App != "" {
		score++
	}
	if r.EventType != "" {
		score++
	}
	return score, true
}

func isCriticalEvent(e domain.TraceEvent) bool {
	if e.HasError() {
		return true
	}
	if e.HTTP != nil && e.HTTP.StatusCode != nil && *e.HTTP.StatusCode >= 500 {
		return true
	}
	return false
}

func (s *SamplingService) count(e domain.TraceEvent, decision string) {
	if s.metrics == nil {
		return
	}
	s.metrics.Counter("ndovu_events_sampled_total",
		"Eventos processados pelo sampling adaptativo",
		map[string]string{
			"app":      strings.ToLower(e.App),
			"type":     string(e.Type),
			"decision": decision,
		}, 1)
}

// ---------------------------------------------------------------------------
// CRUD passthrough (usado pelos handlers admin)
// ---------------------------------------------------------------------------

// SamplingInput é o payload de criação/edição.
type SamplingInput struct {
	App        string
	EventType  string
	SampleRate float64
	KeepErrors bool
	Active     bool
	Note       string
}

// Create insere uma regra e força refresh do cache.
func (s *SamplingService) Create(ctx context.Context, in SamplingInput) (domain.SamplingRule, error) {
	if err := validateSampling(in); err != nil {
		return domain.SamplingRule{}, err
	}
	r, err := s.store.CreateSamplingRule(ctx, domain.SamplingRule{
		App: in.App, EventType: in.EventType, SampleRate: in.SampleRate,
		KeepErrors: in.KeepErrors, Active: in.Active, Note: in.Note,
	})
	if err != nil {
		return domain.SamplingRule{}, err
	}
	_ = s.Refresh(ctx)
	return r, nil
}

// Update atualiza regra existente + refresh.
func (s *SamplingService) Update(ctx context.Context, id string, in SamplingInput) (domain.SamplingRule, error) {
	if err := validateSampling(in); err != nil {
		return domain.SamplingRule{}, err
	}
	r, err := s.store.UpdateSamplingRule(ctx, id, domain.SamplingRule{
		App: in.App, EventType: in.EventType, SampleRate: in.SampleRate,
		KeepErrors: in.KeepErrors, Active: in.Active, Note: in.Note,
	})
	if err != nil {
		return domain.SamplingRule{}, err
	}
	_ = s.Refresh(ctx)
	return r, nil
}

// Delete remove e refresh.
func (s *SamplingService) Delete(ctx context.Context, id string) error {
	if err := s.store.DeleteSamplingRule(ctx, id); err != nil {
		return err
	}
	_ = s.Refresh(ctx)
	return nil
}

// List retorna todas as regras (ativas + inativas) para a UI.
func (s *SamplingService) List(ctx context.Context) ([]domain.SamplingRule, error) {
	return s.store.ListSamplingRules(ctx)
}

// Get resolve uma regra por id (ownership check no admin).
func (s *SamplingService) Get(ctx context.Context, id string) (domain.SamplingRule, error) {
	return s.store.GetSamplingRule(ctx, id)
}

func validateSampling(in SamplingInput) error {
	if in.SampleRate < 0 || in.SampleRate > 1 {
		return domain.NewValidationError("sampleRate deve estar em [0, 1]")
	}
	if in.EventType != "" && !domain.EventType(in.EventType).Valid() {
		return domain.NewValidationError("eventType inválido (page_view|action|http_request|error|custom)")
	}
	return nil
}
