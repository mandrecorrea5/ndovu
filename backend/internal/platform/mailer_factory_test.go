package platform

import (
	"io"
	"log/slog"
	"testing"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestFactoryAutoPreferSendGrid(t *testing.T) {
	m := NewMailer(MailerConfig{
		SMTP:     SMTPConfig{Host: "smtp.local", From: "a@x"},
		SendGrid: SendGridConfig{APIKey: "SG.xxx"},
	}, silentLogger())
	if _, ok := m.(*SendGridMailer); !ok {
		t.Fatalf("auto com sendgrid+smtp deveria escolher sendgrid, veio %T", m)
	}
}

func TestFactoryAutoFallbackSMTP(t *testing.T) {
	m := NewMailer(MailerConfig{
		SMTP: SMTPConfig{Host: "smtp.local", From: "a@x"},
	}, silentLogger())
	if _, ok := m.(*SMTPMailer); !ok {
		t.Fatalf("sem sendgrid deveria escolher smtp, veio %T", m)
	}
}

func TestFactoryAutoFallbackNoop(t *testing.T) {
	m := NewMailer(MailerConfig{}, silentLogger())
	if _, ok := m.(*noopMailer); !ok {
		t.Fatalf("sem provider deveria escolher noop, veio %T", m)
	}
	if err := m.Send("a", []string{"b"}, "s", "h", "t"); err != nil {
		t.Fatalf("noop nunca deveria erro, veio %v", err)
	}
}

func TestFactoryExplicitSendGridWithoutKeyFallsToNoop(t *testing.T) {
	m := NewMailer(MailerConfig{Provider: "sendgrid"}, silentLogger())
	if _, ok := m.(*noopMailer); !ok {
		t.Fatalf("sendgrid sem key deveria virar noop, veio %T", m)
	}
}

func TestFactoryUnknownProviderFallsToNoop(t *testing.T) {
	m := NewMailer(MailerConfig{Provider: "postmark"}, silentLogger())
	if _, ok := m.(*noopMailer); !ok {
		t.Fatalf("provider desconhecido deveria virar noop, veio %T", m)
	}
}
