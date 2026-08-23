package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// AlertNotifier envia a mensagem final. Um adapter separado para Slack e
// outro para webhook genérico — o serviço só compõe a mensagem.
type AlertNotifier interface {
	Send(ctx context.Context, rule domain.AlertRule, message string, payload map[string]any) error
}

// AlertService cria/lista/remove regras e avalia periodicamente contra o
// ClickHouse. Um único evaluator roda no writer para não duplicar disparos.
type AlertService struct {
	rules   domain.AlertRuleStore
	reader  domain.EventReader
	logger  *slog.Logger
	http    *http.Client
	now     func() time.Time
}

// NewAlertService cria o serviço de alertas.
func NewAlertService(rules domain.AlertRuleStore, reader domain.EventReader, logger *slog.Logger) *AlertService {
	return &AlertService{
		rules:  rules,
		reader: reader,
		logger: logger,
		http:   &http.Client{Timeout: 5 * time.Second},
		now:    time.Now,
	}
}

// Create valida e persiste uma nova regra.
func (s *AlertService) Create(ctx context.Context, rule domain.AlertRule) (domain.AlertRule, error) {
	if rule.Name == "" {
		return domain.AlertRule{}, domain.NewValidationError("name é obrigatório")
	}
	if rule.Threshold <= 0 || rule.WindowSeconds <= 0 {
		return domain.AlertRule{}, domain.NewValidationError("threshold e windowSeconds devem ser > 0")
	}
	if !rule.Channel.Valid() {
		return domain.AlertRule{}, domain.NewValidationError("channel deve ser slack ou webhook")
	}
	if rule.TargetURL == "" {
		return domain.AlertRule{}, domain.NewValidationError("targetUrl é obrigatório")
	}
	if rule.SilenceSeconds <= 0 {
		rule.SilenceSeconds = 900
	}
	return s.rules.CreateAlertRule(ctx, rule)
}

// List retorna todas as regras.
func (s *AlertService) List(ctx context.Context) ([]domain.AlertRule, error) {
	return s.rules.ListAlertRules(ctx)
}

// Delete remove uma regra.
func (s *AlertService) Delete(ctx context.Context, id string) error {
	return s.rules.DeleteAlertRule(ctx, id)
}

// EvaluateOnce roda uma passada em todas as regras ativas: para cada regra,
// conta erros na janela e, se cruzou o threshold e passou o silêncio, dispara.
// É chamado por um ticker no writer.
func (s *AlertService) EvaluateOnce(ctx context.Context) {
	rules, err := s.rules.ListAlertRules(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "falha ao listar regras de alerta", "err", err)
		return
	}
	for _, rule := range rules {
		if !rule.Active {
			continue
		}
		s.evaluateRule(ctx, rule)
	}
}

func (s *AlertService) evaluateRule(ctx context.Context, rule domain.AlertRule) {
	windowStart := s.now().Add(-time.Duration(rule.WindowSeconds) * time.Second).UTC()
	count, err := s.reader.CountErrorsSince(ctx, rule.App, rule.ErrorCode, windowStart)
	if err != nil {
		s.logger.WarnContext(ctx, "falha ao contar erros para alerta", "rule", rule.Name, "err", err)
		return
	}
	if count < rule.Threshold {
		return
	}

	last, err := s.rules.LastDeliveryAt(ctx, rule.ID)
	if err != nil {
		s.logger.WarnContext(ctx, "falha ao consultar último disparo", "rule", rule.Name, "err", err)
	}
	if !last.IsZero() && s.now().Sub(last) < time.Duration(rule.SilenceSeconds)*time.Second {
		return
	}

	msg := fmt.Sprintf("Ndovu — alerta \"%s\": %d erros em %ds", rule.Name, count, rule.WindowSeconds)
	if rule.App != "" {
		msg += fmt.Sprintf(" (app: %s)", rule.App)
	}
	if rule.ErrorCode != "" {
		msg += fmt.Sprintf(" (code: %s)", rule.ErrorCode)
	}

	payload := map[string]any{
		"rule":          rule.Name,
		"app":           rule.App,
		"errorCode":     rule.ErrorCode,
		"count":         count,
		"threshold":     rule.Threshold,
		"windowSeconds": rule.WindowSeconds,
		"triggeredAt":   s.now().UTC().Format(time.RFC3339),
	}

	err = s.deliver(ctx, rule, msg, payload)
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	if recErr := s.rules.RecordDelivery(ctx, rule.ID, count, err == nil, detail); recErr != nil {
		s.logger.WarnContext(ctx, "falha ao registrar delivery", "rule", rule.Name, "err", recErr)
	}
	if err != nil {
		s.logger.WarnContext(ctx, "falha ao entregar alerta", "rule", rule.Name, "err", err)
	} else {
		s.logger.InfoContext(ctx, "alerta disparado", "rule", rule.Name, "count", count, "channel", rule.Channel)
	}
}

// deliver envia para o canal certo. Slack quer {"text":"..."}; webhook genérico
// recebe o payload completo.
func (s *AlertService) deliver(ctx context.Context, rule domain.AlertRule, msg string, payload map[string]any) error {
	var body []byte
	var err error
	switch rule.Channel {
	case domain.AlertChannelSlack:
		body, err = json.Marshal(map[string]string{"text": msg})
	case domain.AlertChannelWebhook:
		payload["message"] = msg
		body, err = json.Marshal(payload)
	default:
		return fmt.Errorf("canal não suportado: %s", rule.Channel)
	}
	if err != nil {
		return fmt.Errorf("serializando alerta: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rule.TargetURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("montando request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("enviando alerta: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("resposta do destino: HTTP %d", res.StatusCode)
	}
	return nil
}

// Run inicia um loop periódico de avaliação. Bloqueia até ctx cancelar.
func (s *AlertService) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.logger.Info("avaliador de alertas iniciado", "interval", interval)
	// Avalia uma vez no boot (útil para catch-up após restart)
	s.EvaluateOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.EvaluateOnce(ctx)
		}
	}
}
