package platform

import (
	"fmt"
	"log/slog"
	"strings"
)

// MailerConfig unifica os providers suportados. Provider vazio = "auto":
// escolhe o primeiro habilitado (SendGrid > SMTP > noop).
type MailerConfig struct {
	Provider string // "sendgrid" | "smtp" | "noop" | "" (auto)
	SMTP     SMTPConfig
	SendGrid SendGridConfig
}

// MailerConfigFromEnv monta o config a partir dos valores já resolvidos pelo
// config package. Fica aqui para o api e o writer usarem exatamente o mesmo
// wiring sem duplicar código.
func MailerConfigFromEnv(provider, smtpHost string, smtpPort int, smtpUser, smtpPassword, smtpFrom, sendGridAPIKey string) MailerConfig {
	return MailerConfig{
		Provider: provider,
		SMTP: SMTPConfig{
			Host:     smtpHost,
			Port:     smtpPort,
			Username: smtpUser,
			Password: smtpPassword,
			From:     smtpFrom,
		},
		SendGrid: SendGridConfig{APIKey: sendGridAPIKey},
	}
}

// NewMailer decide o provider baseado no config e retorna sempre um
// Mailer não-nil. Se nenhum estiver configurado, devolve um mailer noop
// que só loga — assim o DigestService pode chamar Send sem checar nil.
func NewMailer(cfg MailerConfig, logger *slog.Logger) Mailer {
	switch strings.ToLower(cfg.Provider) {
	case "sendgrid":
		if cfg.SendGrid.Enabled() {
			logger.Info("mailer: sendgrid selecionado explicitamente")
			return NewSendGridMailer(cfg.SendGrid)
		}
		logger.Warn("mailer: sendgrid pedido mas API key ausente — caindo pra noop")
		return newNoopMailer(logger, "sendgrid sem API key")
	case "smtp":
		if cfg.SMTP.Enabled() {
			logger.Info("mailer: smtp selecionado explicitamente", "host", cfg.SMTP.Host)
			return NewSMTPMailer(cfg.SMTP)
		}
		logger.Warn("mailer: smtp pedido mas host/from ausentes — caindo pra noop")
		return newNoopMailer(logger, "smtp sem host/from")
	case "noop":
		return newNoopMailer(logger, "provider=noop")
	case "", "auto":
		// Auto: prefere SendGrid se configurado (production-friendly),
		// depois SMTP (dev/self-hosted), senão noop.
		if cfg.SendGrid.Enabled() {
			logger.Info("mailer: sendgrid detectado (auto)")
			return NewSendGridMailer(cfg.SendGrid)
		}
		if cfg.SMTP.Enabled() {
			logger.Info("mailer: smtp detectado (auto)", "host", cfg.SMTP.Host)
			return NewSMTPMailer(cfg.SMTP)
		}
		return newNoopMailer(logger, "nenhum provider configurado")
	default:
		logger.Warn("mailer: provider desconhecido, caindo pra noop", "provider", cfg.Provider)
		return newNoopMailer(logger, fmt.Sprintf("provider %q desconhecido", cfg.Provider))
	}
}

// noopMailer não envia nada — só loga que teria enviado. Evita `mailer == nil`
// check espalhado no DigestService e permite que o serviço rode em ambientes
// sem SMTP/SendGrid configurados sem crash.
type noopMailer struct {
	logger *slog.Logger
	reason string
}

func newNoopMailer(logger *slog.Logger, reason string) *noopMailer {
	return &noopMailer{logger: logger, reason: reason}
}

// Send loga o envio como no-op e retorna sucesso (para não travar o loop).
func (m *noopMailer) Send(from string, to []string, subject, htmlBody, textBody string) error {
	m.logger.Info("mailer noop: envio ignorado",
		"reason", m.reason,
		"from", from,
		"to", to,
		"subject", subject,
		"html_bytes", len(htmlBody),
		"text_bytes", len(textBody),
	)
	return nil
}
