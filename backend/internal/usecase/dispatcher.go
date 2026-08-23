package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// AlertDispatcher envia mensagens de alerta pros canais suportados (Slack,
// webhook genérico). Extraído do AlertService pra ser reusado pelo
// AnomalyService — evita duplicar o mesmo Slack/webhook code.
type AlertDispatcher struct {
	http *http.Client
}

// NewAlertDispatcher cria com timeout curto (falha rápido se o alvo estiver fora).
func NewAlertDispatcher() *AlertDispatcher {
	return &AlertDispatcher{http: &http.Client{Timeout: 5 * time.Second}}
}

// Dispatch envia para o canal. Slack quer {"text": ...}; webhook recebe
// o payload completo com o campo `message` adicionado.
func (d *AlertDispatcher) Dispatch(ctx context.Context, channel domain.AlertChannel, targetURL, message string, payload map[string]any) error {
	var body []byte
	var err error
	switch channel {
	case domain.AlertChannelSlack:
		body, err = json.Marshal(map[string]string{"text": message})
	case domain.AlertChannelWebhook:
		if payload == nil {
			payload = map[string]any{}
		}
		payload["message"] = message
		body, err = json.Marshal(payload)
	default:
		return fmt.Errorf("canal não suportado: %s", channel)
	}
	if err != nil {
		return fmt.Errorf("serializando: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("montando request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("enviando: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d do destino", res.StatusCode)
	}
	return nil
}
