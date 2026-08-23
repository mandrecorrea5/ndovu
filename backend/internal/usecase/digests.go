package usecase

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/platform"
)

// DigestService monta e envia o digest semanal dos top erros/issues/releases
// da janela dos últimos 7 dias. Roda no processo do writer via ticker.
type DigestService struct {
	reader     domain.EventReader
	issues     *IssueService
	mailer     platform.Mailer
	recipients []string
	from       string
	dashboard  string // URL pública do dashboard (para links no e-mail)
	logger     *slog.Logger
	now        func() time.Time

	weekday   time.Weekday // dia da semana para enviar (default domingo)
	hourUTC   int          // hora UTC (default 20)
	minuteUTC int          // minuto (default 0)

	lastSent time.Time // idempotência dentro do processo
}

// DigestConfig são os parâmetros do serviço.
type DigestConfig struct {
	Recipients   []string
	From         string
	DashboardURL string
	Weekday      time.Weekday
	HourUTC      int
	MinuteUTC    int
}

// NewDigestService cria o serviço. Se recipients estiver vazio, o Run() vira no-op.
func NewDigestService(reader domain.EventReader, issues *IssueService, mailer platform.Mailer, cfg DigestConfig, logger *slog.Logger) *DigestService {
	return &DigestService{
		reader:     reader,
		issues:     issues,
		mailer:     mailer,
		recipients: cfg.Recipients,
		from:       cfg.From,
		dashboard:  strings.TrimRight(cfg.DashboardURL, "/"),
		logger:     logger,
		now:        time.Now,
		weekday:    cfg.Weekday,
		hourUTC:    cfg.HourUTC,
		minuteUTC:  cfg.MinuteUTC,
	}
}

// Run bloqueia até ctx cancelar. A cada tick (5min) verifica se está na
// janela do agendamento e ainda não enviou esta semana.
func (s *DigestService) Run(ctx context.Context, interval time.Duration) {
	if len(s.recipients) == 0 || s.mailer == nil {
		s.logger.Info("digest semanal desligado (sem SMTP ou sem recipients)")
		return
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	s.logger.Info("digest semanal ativo",
		"recipients", len(s.recipients),
		"weekday", s.weekday.String(),
		"time_utc", fmt.Sprintf("%02d:%02d", s.hourUTC, s.minuteUTC),
	)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// Chance de envio já no primeiro tick — útil para catch-up após restart
	// se a hora do envio já passou hoje.
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *DigestService) tick(ctx context.Context) {
	now := s.now().UTC()
	if now.Weekday() != s.weekday {
		return
	}
	if now.Hour() < s.hourUTC || (now.Hour() == s.hourUTC && now.Minute() < s.minuteUTC) {
		return
	}
	// Já enviou nas últimas 24h? Evita duplicar dentro do mesmo dia
	// (se o processo reiniciou entre uma janela e outra).
	if !s.lastSent.IsZero() && now.Sub(s.lastSent) < 24*time.Hour {
		return
	}
	if err := s.SendOnce(ctx); err != nil {
		s.logger.WarnContext(ctx, "digest falhou", "err", err)
		return
	}
	s.lastSent = now
}

// SendOnce monta o payload e envia imediatamente. Útil pra teste manual
// via CLI/endpoint futuro.
func (s *DigestService) SendOnce(ctx context.Context) error {
	if len(s.recipients) == 0 {
		return fmt.Errorf("nenhum destinatário")
	}
	end := s.now().UTC()
	start := end.Add(-7 * 24 * time.Hour)

	overview, err := s.reader.GetOverview(ctx, start, end, "")
	if err != nil {
		return fmt.Errorf("overview: %w", err)
	}
	issues, err := s.issues.List(ctx, ListInput{
		Filter:   domain.IssueFilter{From: &start, To: &end, Limit: 10},
		OnlyOpen: true,
	})
	if err != nil {
		return fmt.Errorf("issues: %w", err)
	}
	releases, err := s.reader.FindReleases(ctx, domain.ReleaseFilter{From: &start, To: &end, Limit: 5})
	if err != nil {
		return fmt.Errorf("releases: %w", err)
	}

	subject := fmt.Sprintf("Ndovu — digest semanal (%s → %s)",
		start.Format("02/01"), end.Format("02/01"))
	htmlBody, textBody := s.render(overview, issues, releases, start, end)

	s.logger.InfoContext(ctx, "enviando digest",
		"recipients", len(s.recipients),
		"events", overview.TotalEvents,
		"errors", overview.TotalErrors,
		"issues", len(issues),
	)
	return s.mailer.Send(s.from, s.recipients, subject, htmlBody, textBody)
}

// render monta o corpo do digest — HTML minimalista + fallback texto.
// Sem CSS externo (clientes de e-mail bloqueiam); estilos inline básicos.
func (s *DigestService) render(o domain.Overview, issues []domain.Issue, releases []domain.Release, start, end time.Time) (htmlOut, textOut string) {
	dash := s.dashboard
	if dash == "" {
		dash = "http://localhost:13000"
	}

	// --- HTML ---
	var h strings.Builder
	h.WriteString(`<!doctype html><html><body style="font-family:system-ui,-apple-system,sans-serif;color:#0b0b0b;max-width:640px;margin:auto;">`)
	fmt.Fprintf(&h, `<h1 style="margin:0 0 4px">🐘 Ndovu · digest semanal</h1>`)
	fmt.Fprintf(&h, `<p style="color:#666;margin:0 0 20px">%s → %s (UTC)</p>`,
		start.Format("02 Jan"), end.Format("02 Jan"))

	// Tiles
	h.WriteString(`<table cellpadding="12" cellspacing="6" style="width:100%;border-collapse:separate;"><tr>`)
	tile(&h, "Eventos", fmt.Sprintf("%d", o.TotalEvents), "")
	tile(&h, "Sessões", fmt.Sprintf("%d", o.TotalSessions), "")
	tile(&h, "Usuários", fmt.Sprintf("%d", o.TotalUsers), "")
	errTone := ""
	if o.TotalErrors > 0 {
		errTone = "color:#d03b3b;"
	}
	tile(&h, "Erros", fmt.Sprintf("%d", o.TotalErrors),
		fmt.Sprintf("style=\"%s\"", errTone))
	h.WriteString(`</tr></table>`)

	// Top issues
	if len(issues) > 0 {
		h.WriteString(`<h2 style="margin-top:24px;font-size:16px;">Top issues abertas</h2><ul style="padding-left:16px;">`)
		for _, i := range issues {
			fmt.Fprintf(&h, `<li style="margin-bottom:6px;"><a href="%s/issues/%s" style="color:#2a78d6;text-decoration:none;"><code>%s</code></a> — %s <span style="color:#666;">(%d ocorrências, %d usuários)</span></li>`,
				dash, html.EscapeString(i.Fingerprint),
				html.EscapeString(defaultStr(i.Code, "—")),
				html.EscapeString(truncate(defaultStr(i.Message, i.Name), 90)),
				i.Count, i.AffectedUsers)
		}
		h.WriteString(`</ul>`)
	} else {
		h.WriteString(`<p style="color:#0ca30c;">Sem issues abertas na semana. 🎉</p>`)
	}

	// Releases
	if len(releases) > 0 {
		h.WriteString(`<h2 style="margin-top:24px;font-size:16px;">Releases ativos</h2><ul style="padding-left:16px;">`)
		for _, r := range releases {
			pct := r.ErrorRate * 100
			tone := ""
			if pct > 5 {
				tone = "color:#d03b3b;"
			}
			fmt.Fprintf(&h, `<li><code>%s</code> · %s · %d eventos · <span style="%s">%.2f%%</span> erro</li>`,
				html.EscapeString(r.Release), html.EscapeString(r.App), r.Events, tone, pct)
		}
		h.WriteString(`</ul>`)
	}

	fmt.Fprintf(&h, `<p style="margin-top:32px;color:#999;font-size:12px;">Ver dashboard → <a href="%s" style="color:#2a78d6;">%s</a></p>`,
		dash, dash)
	h.WriteString(`</body></html>`)

	// --- Text fallback ---
	var t strings.Builder
	fmt.Fprintf(&t, "Ndovu · digest semanal — %s → %s (UTC)\n\n", start.Format("02 Jan"), end.Format("02 Jan"))
	fmt.Fprintf(&t, "Eventos:  %d\nSessões:  %d\nUsuários: %d\nErros:    %d (%.2f%%)\n\n",
		o.TotalEvents, o.TotalSessions, o.TotalUsers, o.TotalErrors, o.ErrorRate*100)
	if len(issues) > 0 {
		t.WriteString("Top issues abertas:\n")
		for _, i := range issues {
			fmt.Fprintf(&t, "  · [%s] %s (%d ocorrências, %d usuários) — %s/issues/%s\n",
				defaultStr(i.Code, "—"),
				truncate(defaultStr(i.Message, i.Name), 80),
				i.Count, i.AffectedUsers,
				dash, i.Fingerprint)
		}
		t.WriteString("\n")
	}
	if len(releases) > 0 {
		t.WriteString("Releases ativos:\n")
		for _, r := range releases {
			fmt.Fprintf(&t, "  · %s (%s) · %d eventos · %.2f%% erro\n",
				r.Release, r.App, r.Events, r.ErrorRate*100)
		}
		t.WriteString("\n")
	}
	fmt.Fprintf(&t, "Dashboard: %s\n", dash)

	return h.String(), t.String()
}

func tile(w *strings.Builder, label, value, valueAttrs string) {
	fmt.Fprintf(w, `<td style="background:#f9f9f7;border:1px solid #e1e0d9;border-radius:8px;padding:12px;text-align:center;width:25%%;">
<div style="font-size:11px;color:#898781;text-transform:uppercase;letter-spacing:0.05em;">%s</div>
<div %s style="font-size:22px;font-weight:600;margin-top:4px;">%s</div>
</td>`, label, valueAttrs, value)
}

func defaultStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
