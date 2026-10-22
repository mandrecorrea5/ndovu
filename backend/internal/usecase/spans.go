package usecase

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
	"github.com/marcoscorrea/ndovu/backend/internal/platform"
)

// SpanRuleType é o event_type das regras de sampling que valem para spans
// OTLP. Regras de evento (inclusive a global, event_type vazio) não se
// aplicam a spans: o volume e a granularidade são outros, e uma regra criada
// para page_view não deve passar a cortar traces do backend.
const SpanRuleType = "span"

// ---------------------------------------------------------------------------
// Ingestão (API)
// ---------------------------------------------------------------------------

// SpanIngestService recebe spans já decodificados do OTLP, redige PII e
// publica no stream. Como na ingestão de eventos, nunca espera o banco.
type SpanIngestService struct {
	stream domain.SpanStream
	logger *slog.Logger
}

// NewSpanIngestService cria o serviço.
func NewSpanIngestService(stream domain.SpanStream, logger *slog.Logger) *SpanIngestService {
	return &SpanIngestService{stream: stream, logger: logger}
}

// Ingest redige e enfileira os spans de um app. A redação acontece aqui, e
// não no writer, para o dado sensível não chegar nem ao disco do stream.
func (s *SpanIngestService) Ingest(ctx context.Context, app string, spans []domain.Span) error {
	if len(spans) == 0 {
		return nil
	}
	for i := range spans {
		RedactSpan(&spans[i])
	}
	if err := s.stream.PublishSpans(ctx, domain.SpanBatch{App: app, Spans: spans}); err != nil {
		return fmt.Errorf("publicando spans no stream: %w", err)
	}
	s.logger.DebugContext(ctx, "spans enfileirados", "app", app, "spans", len(spans))
	return nil
}

// redactKeys é a mesma lista do SDK (sdk/src/ndovu-node.js), mais cookie e
// variações de api key — headers capturados pelas instrumentações HTTP viram
// atributos como http.request.header.cookie.
var redactKeys = regexp.MustCompile(`(?i)pass(word)?|senha|token|secret|authorization|cookie|api[-_.]?key|cvv|card|cart[aã]o`)

// urlKeys são atributos cuja query string pode carregar PII ou credencial
// (?email=, ?token=); a query sai por padrão (AVALIACAO-OTLP §4.2).
var urlKeys = map[string]bool{
	"url.full":    true,
	"http.url":    true,
	"http.target": true,
}

// RedactedValue substitui o valor de um atributo sensível.
const RedactedValue = "***"

// RedactSpan aplica a redação a todos os mapas de atributos do span.
func RedactSpan(s *domain.Span) {
	redactAttrs(s.Attrs)
	redactAttrs(s.ResourceAttrs)
	for i := range s.Events {
		redactAttrs(s.Events[i].Attrs)
	}
	for i := range s.Links {
		redactAttrs(s.Links[i].Attrs)
	}
}

func redactAttrs(m map[string]string) {
	for k, v := range m {
		switch {
		case redactKeys.MatchString(k):
			m[k] = RedactedValue
		case k == "url.query":
			if v != "" {
				m[k] = RedactedValue
			}
		case urlKeys[k]:
			m[k] = stripQuery(v)
		}
	}
}

// stripQuery remove query string e fragmento, preservando o resto da URL
// (ou do path, no caso de http.target).
func stripQuery(raw string) string {
	if cut := strings.IndexAny(raw, "?#"); cut >= 0 {
		return raw[:cut]
	}
	return raw
}

// ---------------------------------------------------------------------------
// Sampling por trace + cota diária (writer)
// ---------------------------------------------------------------------------

// SpanRuleSource fornece a regra de sampling de spans de um app.
type SpanRuleSource interface {
	SpanRule(app string) (domain.SamplingRule, bool)
}

// QuotaNotifier é avisado quando um app entra ou sai do sampling forçado.
type QuotaNotifier interface {
	QuotaChanged(ctx context.Context, app string, forced bool, count, quota int)
}

// SpanPolicyConfig parametriza o SpanPolicy.
type SpanPolicyConfig struct {
	DailyQuota int            // spans/dia por app; 0 desliga a cota
	ForcedRate float64        // taxa aplicada a quem estourou a cota
	Location   *time.Location // fuso da virada do dia
}

// SpanPolicy decide quais spans são gravados. Duas camadas:
//
//  1. Regra de sampling (event_type "span"), por app.
//  2. Cota diária: passou de DailyQuota spans gravados no dia, o app entra
//     em sampling forçado a ForcedRate até a virada (decisão 2 da §6). Não é
//     429: exporter que recebe 429 por horas descarta tudo, inclusive erros.
//
// A decisão é por trace — hash do trace_id —, então o trace inteiro fica ou
// sai, em qualquer serviço que o veja. Trace com erro no mesmo lote fica
// sempre. É head sampling no servidor, não tail sampling: o erro que chega
// num lote posterior não resgata spans já descartados.
//
// Roda só no writer (processo único), o que mantém o contador consistente
// sem coordenação.
type SpanPolicy struct {
	rules    SpanRuleSource // opcional
	counter  domain.SpanReader
	notifier QuotaNotifier // opcional
	cfg      SpanPolicyConfig
	logger   *slog.Logger
	now      func() time.Time

	mu     sync.Mutex
	day    string
	counts map[string]int
	forced map[string]bool
}

// NewSpanPolicy cria a política. counter reconstrói a contagem do dia depois
// de um restart; sem ele (nil), o dia recomeça do zero.
func NewSpanPolicy(rules SpanRuleSource, counter domain.SpanReader, notifier QuotaNotifier, cfg SpanPolicyConfig, logger *slog.Logger) *SpanPolicy {
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	return &SpanPolicy{
		rules: rules, counter: counter, notifier: notifier, cfg: cfg, logger: logger,
		now: time.Now, counts: map[string]int{}, forced: map[string]bool{},
	}
}

// Filter devolve os spans do lote que devem ser gravados. Não conta nada: o
// writer chama Record depois que o insert deu certo, para reentrega do
// stream não inflar a cota.
func (p *SpanPolicy) Filter(ctx context.Context, batch domain.SpanBatch) domain.SpanBatch {
	rate, keepErrors := 1.0, true
	if p.rules != nil {
		if rule, ok := p.rules.SpanRule(batch.App); ok {
			rate, keepErrors = rule.SampleRate, rule.KeepErrors
		}
	}
	if p.isForced(ctx, batch.App) {
		rate = math.Min(rate, p.cfg.ForcedRate)
		keepErrors = true // erros passam mesmo no modo forçado
	}
	if rate >= 1 {
		return batch
	}

	var errorTraces map[string]bool
	if keepErrors {
		for _, s := range batch.Spans {
			if s.HasError() {
				if errorTraces == nil {
					errorTraces = map[string]bool{}
				}
				errorTraces[s.TraceID] = true
			}
		}
	}
	kept := make([]domain.Span, 0, len(batch.Spans))
	for _, s := range batch.Spans {
		if errorTraces[s.TraceID] || KeepTrace(s.TraceID, rate) {
			kept = append(kept, s)
		}
	}
	batch.Spans = kept
	return batch
}

// Record soma os spans gravados à cota do dia e liga o modo forçado de quem
// passou do limite.
func (p *SpanPolicy) Record(ctx context.Context, app string, n int) {
	if p.cfg.DailyQuota <= 0 || n == 0 {
		return
	}
	p.mu.Lock()
	left := p.rollDayLocked(ctx)
	p.counts[app] += n
	count := p.counts[app]
	entered := !p.forced[app] && count >= p.cfg.DailyQuota
	if entered {
		p.forced[app] = true
	}
	p.mu.Unlock()

	p.notifyLeft(ctx, left)
	if entered && p.notifier != nil {
		p.notifier.QuotaChanged(ctx, app, true, count, p.cfg.DailyQuota)
	}
}

func (p *SpanPolicy) isForced(ctx context.Context, app string) bool {
	if p.cfg.DailyQuota <= 0 {
		return false
	}
	p.mu.Lock()
	left := p.rollDayLocked(ctx)
	forced := p.forced[app]
	p.mu.Unlock()
	p.notifyLeft(ctx, left)
	return forced
}

// notifyLeft avisa, fora do lock, quem saiu do modo forçado na virada.
func (p *SpanPolicy) notifyLeft(ctx context.Context, apps []string) {
	if p.notifier == nil {
		return
	}
	for _, app := range apps {
		p.notifier.QuotaChanged(ctx, app, false, 0, p.cfg.DailyQuota)
	}
}

// rollDayLocked zera a contagem na virada do dia (no fuso configurado) e
// devolve quem estava em modo forçado, para o caller avisar a saída. No
// primeiro uso depois do boot, reconstrói a contagem do dia a partir do
// armazém — uma query, uma vez por processo.
func (p *SpanPolicy) rollDayLocked(ctx context.Context) []string {
	now := p.now().In(p.cfg.Location)
	day := now.Format(time.DateOnly)
	if day == p.day {
		return nil
	}
	booting := p.day == ""
	var left []string
	for app := range p.forced {
		left = append(left, app)
	}
	p.day = day
	p.counts = map[string]int{}
	p.forced = map[string]bool{}

	if !booting || p.counter == nil {
		return left
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, p.cfg.Location)
	counts, err := p.counter.CountSpansByApp(ctx, midnight)
	if err != nil {
		p.logger.WarnContext(ctx, "cota de spans: contagem do dia indisponível, recomeçando do zero", "err", err)
		return nil
	}
	for app, n := range counts {
		p.counts[app] = n
		if n >= p.cfg.DailyQuota {
			// Já estava forçado antes do restart: sem novo aviso.
			p.forced[app] = true
		}
	}
	return nil
}

// KeepTrace decide de forma determinística se um trace entra na amostra de
// taxa `rate`. O mesmo trace_id dá a mesma resposta em qualquer lote, então
// spans de serviços diferentes do mesmo trace ficam (ou saem) juntos.
func KeepTrace(traceID string, rate float64) bool {
	if rate >= 1 {
		return true
	}
	if rate <= 0 {
		return false
	}
	// FNV em vez dos bytes crus do trace_id: nem todo gerador de id é
	// aleatório nos bits altos (ids sequenciais, X-Ray).
	h := fnv.New64a()
	_, _ = h.Write([]byte(traceID))
	return float64(h.Sum64()) < rate*math.MaxUint64
}

// ---------------------------------------------------------------------------
// Persistência (writer)
// ---------------------------------------------------------------------------

// SpanDeadLetterSink recebe lotes de spans que o armazém rejeita.
type SpanDeadLetterSink func(ctx context.Context, batch domain.SpanBatch, reason string) error

// SpanWriteService persiste os lotes de spans vindos do stream.
type SpanWriteService struct {
	writer domain.SpanWriter
	policy *SpanPolicy // opcional; nil = grava tudo
	dlq    SpanDeadLetterSink
	logger *slog.Logger
}

// NewSpanWriteService cria o serviço.
func NewSpanWriteService(writer domain.SpanWriter, policy *SpanPolicy, dlq SpanDeadLetterSink, logger *slog.Logger) *SpanWriteService {
	return &SpanWriteService{writer: writer, policy: policy, dlq: dlq, logger: logger}
}

// HandleBatches aplica a política e grava tudo num INSERT só. Se o insert
// combinado falhar, tenta lote a lote e manda o que falhar para a DLQ — a
// mesma estratégia do WriteService de eventos.
//
// O contexto chega sem tracing (WithoutTracing): o Ndovu pode exportar os
// próprios spans para este receptor, e rastrear a gravação de spans geraria
// spans novos a cada lote, para sempre.
func (s *SpanWriteService) HandleBatches(ctx context.Context, batches []domain.SpanBatch) error {
	ctx = platform.WithoutTracing(ctx)
	if s.policy != nil {
		filtered := batches[:0]
		for _, b := range batches {
			if f := s.policy.Filter(ctx, b); len(f.Spans) > 0 {
				filtered = append(filtered, f)
			}
		}
		batches = filtered
	}
	if len(batches) == 0 {
		return nil
	}

	if _, err := s.writer.SaveSpans(ctx, batches); err == nil {
		s.record(ctx, batches)
		return nil
	} else if len(batches) == 1 {
		return s.quarantine(ctx, batches[0], err)
	} else {
		s.logger.WarnContext(ctx, "insert combinado de spans falhou; tentando lote a lote", "err", err)
	}
	for _, b := range batches {
		if _, err := s.writer.SaveSpans(ctx, []domain.SpanBatch{b}); err != nil {
			if qErr := s.quarantine(ctx, b, err); qErr != nil {
				return qErr
			}
			continue
		}
		s.record(ctx, []domain.SpanBatch{b})
	}
	return nil
}

func (s *SpanWriteService) record(ctx context.Context, batches []domain.SpanBatch) {
	if s.policy == nil {
		return
	}
	for _, b := range batches {
		s.policy.Record(ctx, b.App, len(b.Spans))
	}
}

func (s *SpanWriteService) quarantine(ctx context.Context, b domain.SpanBatch, cause error) error {
	s.logger.ErrorContext(ctx, "lote de spans rejeitado pelo armazém — enviando para DLQ",
		"app", b.App, "spans", len(b.Spans), "err", cause)
	if s.dlq == nil {
		return fmt.Errorf("persistindo spans (sem DLQ configurada): %w", cause)
	}
	if err := s.dlq(ctx, b, cause.Error()); err != nil {
		return fmt.Errorf("enviando spans para DLQ: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Aviso de cota
// ---------------------------------------------------------------------------

// QuotaAlerts avisa a entrada e a saída do sampling forçado: audit log
// (visível em /admin/audit-log), log WARN e e-mail para os admins.
type QuotaAlerts struct {
	audit  *AuditService
	mailer platform.Mailer // opcional
	from   string
	to     []string
	rate   float64
	logger *slog.Logger
}

// NewQuotaAlerts cria o notificador. mailer nil ou to vazio = sem e-mail.
func NewQuotaAlerts(audit *AuditService, mailer platform.Mailer, from string, to []string, forcedRate float64, logger *slog.Logger) *QuotaAlerts {
	return &QuotaAlerts{audit: audit, mailer: mailer, from: from, to: to, rate: forcedRate, logger: logger}
}

// QuotaChanged implementa QuotaNotifier.
func (q *QuotaAlerts) QuotaChanged(ctx context.Context, app string, forced bool, count, quota int) {
	action, subject := "otlp.quota.forced_sampling_on",
		fmt.Sprintf("[Ndovu] %s passou da cota de spans — sampling forçado a %.0f%%", app, q.rate*100)
	if !forced {
		action, subject = "otlp.quota.forced_sampling_off",
			fmt.Sprintf("[Ndovu] %s voltou ao sampling normal de spans", app)
	}
	q.logger.WarnContext(ctx, "cota diária de spans", "app", app, "forced", forced, "count", count, "quota", quota)
	if q.audit != nil {
		q.audit.Record(ctx, RecordInput{
			Actor:        domain.Identity{Email: "system"},
			Action:       action,
			ResourceType: "app",
			ResourceID:   app,
			Details:      map[string]any{"count": count, "quota": quota, "forcedRate": q.rate},
		})
	}
	if q.mailer == nil || len(q.to) == 0 {
		return
	}
	body := fmt.Sprintf("App %s: %d spans gravados hoje (cota %d). ", app, count, quota)
	if forced {
		body += fmt.Sprintf("Até a virada do dia, só %.0f%% dos traces são gravados; traces com erro continuam passando.", q.rate*100)
	} else {
		body = fmt.Sprintf("App %s: dia novo, a cota de spans recomeçou e o sampling forçado foi desligado.", app)
	}
	if err := q.mailer.Send(q.from, q.to, subject, "<p>"+body+"</p>", body); err != nil {
		q.logger.WarnContext(ctx, "cota de spans: e-mail de aviso falhou", "app", app, "err", err)
	}
}
