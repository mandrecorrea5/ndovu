package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// AnomalyService avalia regras periodicamente (roda no writer). Para cada
// regra ativa: mede a janela atual, mede a mesma hora/weekday em cada uma
// das últimas N semanas, calcula z-score, dispara se cruzar sensitivity.
type AnomalyService struct {
	store      domain.AnomalyStore
	reader     domain.EventReader
	dispatcher *AlertDispatcher
	logger     *slog.Logger
	now        func() time.Time
}

// NewAnomalyService cria o serviço.
func NewAnomalyService(store domain.AnomalyStore, reader domain.EventReader, dispatcher *AlertDispatcher, logger *slog.Logger) *AnomalyService {
	return &AnomalyService{
		store: store, reader: reader, dispatcher: dispatcher, logger: logger,
		now: time.Now,
	}
}

// Run avalia todas as regras a cada `interval`. Bloqueia até ctx cancelar.
func (s *AnomalyService) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	s.logger.Info("avaliador de anomalias iniciado", "interval", interval)
	s.EvaluateOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.EvaluateOnce(ctx)
		}
	}
}

// EvaluateOnce faz uma passada por todas as regras ativas.
func (s *AnomalyService) EvaluateOnce(ctx context.Context) {
	rules, err := s.store.ListAnomalyRules(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "falha ao listar anomaly rules", "err", err)
		return
	}
	for _, r := range rules {
		if !r.Active {
			continue
		}
		s.evaluate(ctx, r)
	}
}

// evaluate roda uma regra: coleta amostras, calcula z-score, dispara se
// necessário. Falha silenciosa (loga warn) — não trava o loop.
func (s *AnomalyService) evaluate(ctx context.Context, rule domain.AnomalyRule) {
	now := s.now().UTC()
	winDur := time.Duration(rule.WindowMinutes) * time.Minute
	currentFrom := now.Add(-winDur)
	current, err := s.reader.MetricInWindow(ctx, string(rule.Metric), rule.App, currentFrom, now)
	if err != nil {
		s.logger.WarnContext(ctx, "medindo janela atual", "rule", rule.Name, "err", err)
		return
	}

	// Baseline: mesma hora+weekday nas últimas N semanas. Colecionamos
	// pontos independentes (não fazemos janela deslizante) — cada semana
	// contribui com 1 valor.
	baseline := make([]float64, 0, rule.BaselineWeeks)
	for i := 1; i <= rule.BaselineWeeks; i++ {
		shift := time.Duration(i) * 7 * 24 * time.Hour
		from := currentFrom.Add(-shift)
		to := now.Add(-shift)
		v, err := s.reader.MetricInWindow(ctx, string(rule.Metric), rule.App, from, to)
		if err != nil {
			s.logger.WarnContext(ctx, "medindo baseline",
				"rule", rule.Name, "week_offset", i, "err", err)
			continue
		}
		baseline = append(baseline, v)
	}
	if len(baseline) < 2 {
		// Sem histórico suficiente pra ter stddev — não faz sentido avaliar.
		return
	}

	avg, stddev := statsOf(baseline)
	// Se stddev zero (todas as amostras iguais), pequeno epsilon para não dividir por 0.
	if stddev < 1e-6 {
		stddev = 1e-6
	}
	z := (current - avg) / stddev

	// Direção: acima só spike positivo, abaixo só negativo, both = |z|.
	triggered := false
	fired := domain.AnomalyAbove
	switch rule.Direction {
	case domain.AnomalyAbove:
		triggered = z > rule.Sensitivity
	case domain.AnomalyBelow:
		triggered = z < -rule.Sensitivity
		fired = domain.AnomalyBelow
	case domain.AnomalyBoth:
		triggered = math.Abs(z) > rule.Sensitivity
		if z < 0 {
			fired = domain.AnomalyBelow
		}
	}
	if !triggered {
		return
	}

	// Silence window: evita alertar de novo dentro do intervalo configurado.
	last, err := s.store.LastDetection(ctx, rule.ID)
	if err == nil && !last.DetectedAt.IsZero() &&
		s.now().Sub(last.DetectedAt) < time.Duration(rule.SilenceSeconds)*time.Second {
		return
	}

	// Notifica + registra em uma transação lógica (best-effort).
	msg := s.buildMessage(rule, current, avg, stddev, z, fired)
	payload := map[string]any{
		"rule":           rule.Name,
		"app":            rule.App,
		"metric":         string(rule.Metric),
		"currentValue":   current,
		"baselineAvg":    avg,
		"baselineStddev": stddev,
		"zScore":         z,
		"direction":      string(fired),
	}
	notifyErr := s.dispatcher.Dispatch(ctx, rule.Channel, rule.TargetURL, msg, payload)
	detail := ""
	if notifyErr != nil {
		detail = notifyErr.Error()
	}
	rec := domain.AnomalyDetection{
		RuleID: rule.ID, CurrentValue: current, BaselineAvg: avg,
		BaselineStddev: stddev, ZScore: z, Direction: fired,
		NotifyOK: notifyErr == nil, NotifyDetail: detail,
	}
	if err := s.store.RecordDetection(ctx, rec); err != nil {
		s.logger.WarnContext(ctx, "recording detection", "rule", rule.Name, "err", err)
	}
	if notifyErr != nil {
		s.logger.WarnContext(ctx, "notify falhou", "rule", rule.Name, "err", notifyErr)
	} else {
		s.logger.InfoContext(ctx, "anomalia detectada",
			"rule", rule.Name, "z", z, "current", current, "avg", avg)
	}
}

// buildMessage monta o corpo humano-legível do alerta.
func (s *AnomalyService) buildMessage(rule domain.AnomalyRule, current, avg, stddev, z float64, dir domain.AnomalyDirection) string {
	arrow := "↑"
	if dir == domain.AnomalyBelow {
		arrow = "↓"
	}
	appPart := ""
	if rule.App != "" {
		appPart = fmt.Sprintf(" (app %s)", rule.App)
	}
	return fmt.Sprintf(
		"Ndovu — anomalia detectada%s: %s %s %.2f (baseline média %.2f ± %.2f, z=%.2f, regra %q)",
		appPart, rule.Metric, arrow, current, avg, stddev, z, rule.Name)
}

// statsOf devolve média e stddev populacional de uma amostra pequena.
func statsOf(xs []float64) (float64, float64) {
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(len(xs))
	var sq float64
	for _, x := range xs {
		d := x - mean
		sq += d * d
	}
	return mean, math.Sqrt(sq / float64(len(xs)))
}

// ---------------------------------------------------------------------------
// CRUD passthrough (usado pelos handlers admin)
// ---------------------------------------------------------------------------

// AnomalyInput é o payload de criação/edição.
type AnomalyInput struct {
	Name           string
	App            string
	Metric         domain.AnomalyMetric
	WindowMinutes  int
	BaselineWeeks  int
	Sensitivity    float64
	Direction      domain.AnomalyDirection
	SilenceSeconds int
	Channel        domain.AlertChannel
	TargetURL      string
	Active         bool
}

// Create insere uma nova regra.
func (s *AnomalyService) Create(ctx context.Context, in AnomalyInput) (domain.AnomalyRule, error) {
	if err := validateAnomaly(in); err != nil {
		return domain.AnomalyRule{}, err
	}
	return s.store.CreateAnomalyRule(ctx, in.toDomain())
}

// Update atualiza uma regra existente.
func (s *AnomalyService) Update(ctx context.Context, id string, in AnomalyInput) (domain.AnomalyRule, error) {
	if err := validateAnomaly(in); err != nil {
		return domain.AnomalyRule{}, err
	}
	return s.store.UpdateAnomalyRule(ctx, id, in.toDomain())
}

// Delete remove uma regra.
func (s *AnomalyService) Delete(ctx context.Context, id string) error {
	return s.store.DeleteAnomalyRule(ctx, id)
}

// Get resolve uma regra por id (ownership check no admin).
func (s *AnomalyService) Get(ctx context.Context, id string) (domain.AnomalyRule, error) {
	return s.store.GetAnomalyRule(ctx, id)
}

// List devolve todas as regras.
func (s *AnomalyService) List(ctx context.Context) ([]domain.AnomalyRule, error) {
	return s.store.ListAnomalyRules(ctx)
}

// Detections lista o histórico paginado.
func (s *AnomalyService) Detections(ctx context.Context, limit, offset int) ([]domain.AnomalyDetection, int, error) {
	return s.store.ListDetections(ctx, limit, offset)
}

func (in AnomalyInput) toDomain() domain.AnomalyRule {
	if in.SilenceSeconds <= 0 {
		in.SilenceSeconds = 1800
	}
	return domain.AnomalyRule{
		Name: strings.TrimSpace(in.Name), App: strings.TrimSpace(in.App),
		Metric: in.Metric, WindowMinutes: in.WindowMinutes, BaselineWeeks: in.BaselineWeeks,
		Sensitivity: in.Sensitivity, Direction: in.Direction,
		SilenceSeconds: in.SilenceSeconds, Channel: in.Channel,
		TargetURL: in.TargetURL, Active: in.Active,
	}
}

func validateAnomaly(in AnomalyInput) error {
	var issues []string
	if strings.TrimSpace(in.Name) == "" {
		issues = append(issues, "name é obrigatório")
	}
	if !in.Metric.Valid() {
		issues = append(issues, "metric deve ser error_count|event_count|error_rate")
	}
	if in.WindowMinutes < 1 || in.WindowMinutes > 240 {
		issues = append(issues, "windowMinutes deve estar entre 1 e 240")
	}
	if in.BaselineWeeks < 1 || in.BaselineWeeks > 12 {
		issues = append(issues, "baselineWeeks deve estar entre 1 e 12")
	}
	if in.Sensitivity < 1 {
		issues = append(issues, "sensitivity deve ser >= 1")
	}
	if !in.Direction.Valid() {
		issues = append(issues, "direction deve ser above|below|both")
	}
	if !in.Channel.Valid() {
		issues = append(issues, "channel deve ser slack|webhook")
	}
	if strings.TrimSpace(in.TargetURL) == "" {
		issues = append(issues, "targetUrl é obrigatório")
	}
	if len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	return nil
}
