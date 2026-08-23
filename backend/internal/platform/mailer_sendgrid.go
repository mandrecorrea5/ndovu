package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SendGridMailer implementa Mailer sobre a REST API v3 do SendGrid.
// Endpoint: POST https://api.sendgrid.com/v3/mail/send.
// Sem SDK oficial — o payload é simples e não vale adicionar dep.
type SendGridMailer struct {
	apiKey  string
	http    *http.Client
	baseURL string // customizável para testes; default = api.sendgrid.com
}

// SendGridConfig — o From vem do SMTPConfig.From compartilhado (mesmo remetente
// vale para SMTP e SendGrid; a chave é a única coisa exclusiva).
type SendGridConfig struct {
	APIKey  string
	Timeout time.Duration
	BaseURL string
}

// Enabled indica se a config permite enviar.
func (c SendGridConfig) Enabled() bool { return c.APIKey != "" }

// NewSendGridMailer cria o mailer. Se APIKey vazio, Send retorna erro.
func NewSendGridMailer(cfg SendGridConfig) *SendGridMailer {
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.sendgrid.com"
	}
	return &SendGridMailer{
		apiKey:  cfg.APIKey,
		http:    &http.Client{Timeout: cfg.Timeout},
		baseURL: cfg.BaseURL,
	}
}

type sgAddr struct {
	Email string `json:"email"`
}

type sgPersonalization struct {
	To      []sgAddr `json:"to"`
	Subject string   `json:"subject"`
}

type sgContent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type sgPayload struct {
	From             sgAddr              `json:"from"`
	Personalizations []sgPersonalization `json:"personalizations"`
	Content          []sgContent         `json:"content"`
}

// Send envia via API do SendGrid. Um único "personalization" com todos os
// destinatários em TO (não usa BCC — se quiser esconder recipients basta
// mandar uma personalization por endereço).
func (m *SendGridMailer) Send(from string, to []string, subject, htmlBody, textBody string) error {
	if m.apiKey == "" {
		return fmt.Errorf("sendgrid: API key ausente")
	}
	if from == "" || len(to) == 0 {
		return fmt.Errorf("sendgrid: from e to são obrigatórios")
	}

	payload := sgPayload{
		From: sgAddr{Email: from},
		Personalizations: []sgPersonalization{{
			To:      buildAddrs(to),
			Subject: subject,
		}},
		Content: []sgContent{
			// SendGrid exige text/plain ANTES de text/html — a ordem importa.
			{Type: "text/plain", Value: textBody},
			{Type: "text/html", Value: htmlBody},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sendgrid: serializando payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), m.http.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/v3/mail/send", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sendgrid: montando request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("sendgrid: enviando: %w", err)
	}
	defer res.Body.Close()
	// Sucesso: 202 Accepted (documentado). Qualquer outra coisa é erro —
	// lemos o corpo para expor a mensagem que o SendGrid devolveu.
	if res.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("sendgrid: HTTP %d: %s", res.StatusCode, string(respBody))
	}
	return nil
}

func buildAddrs(list []string) []sgAddr {
	out := make([]sgAddr, 0, len(list))
	for _, e := range list {
		out = append(out, sgAddr{Email: e})
	}
	return out
}
