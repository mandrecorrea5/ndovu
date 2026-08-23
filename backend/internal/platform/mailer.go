package platform

import (
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Mailer envia e-mails via SMTP. Interface para permitir mock em teste.
type Mailer interface {
	Send(from string, to []string, subject, htmlBody, textBody string) error
}

// SMTPConfig são as credenciais + destino do servidor SMTP.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	Timeout  time.Duration
}

// Enabled indica se o config está completo o suficiente para enviar.
// Sem host ou from, o mailer é considerado desligado (retorna erro no Send).
func (c SMTPConfig) Enabled() bool {
	return c.Host != "" && c.From != ""
}

// SMTPMailer implementa Mailer sobre net/smtp. Sem TLS obrigatório
// (para funcionar com MailHog local que não fala TLS); em produção o Host
// deve apontar para um relay TLS-friendly ou usar STARTTLS.
type SMTPMailer struct {
	cfg SMTPConfig
}

// NewSMTPMailer cria o mailer. Se cfg.Enabled() == false, Send retorna erro.
func NewSMTPMailer(cfg SMTPConfig) *SMTPMailer {
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &SMTPMailer{cfg: cfg}
}

// Send envia um e-mail multipart (text + html) para todos os destinatários.
// Auth só é feita se Username != "" (MailHog não exige auth).
func (m *SMTPMailer) Send(from string, to []string, subject, htmlBody, textBody string) error {
	if !m.cfg.Enabled() {
		return errors.New("SMTP desligado (defina NDOVU_SMTP_HOST e NDOVU_SMTP_FROM)")
	}
	if len(to) == 0 {
		return errors.New("nenhum destinatário")
	}
	if from == "" {
		from = m.cfg.From
	}

	// net.JoinHostPort trata IPv6 (colchetes) — go vet reclama de "%s:%d".
	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))

	// net.DialTimeout garante que host inacessível não trava o loop do writer.
	conn, err := net.DialTimeout("tcp", addr, m.cfg.Timeout)
	if err != nil {
		return fmt.Errorf("conectando SMTP %s: %w", addr, err)
	}
	defer conn.Close()

	c, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return fmt.Errorf("cliente SMTP: %w", err)
	}
	defer c.Quit()

	if m.cfg.Username != "" {
		auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("auth SMTP: %w", err)
		}
	}

	if err := c.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", rcpt, err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	msg := buildMultipart(from, to, subject, htmlBody, textBody)
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("escrevendo corpo: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("fechando DATA: %w", err)
	}
	return nil
}

// buildMultipart monta um MIME multipart/alternative — o cliente escolhe
// html ou text conforme suporte. Preview de e-mail (Gmail/Outlook) usa text.
func buildMultipart(from string, to []string, subject, html, text string) string {
	boundary := "ndovu-mp-boundary"
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprint(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", boundary)

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprint(&b, "Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(text)
	b.WriteString("\r\n\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprint(&b, "Content-Type: text/html; charset=UTF-8\r\n\r\n")
	b.WriteString(html)
	b.WriteString("\r\n\r\n")

	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.String()
}
