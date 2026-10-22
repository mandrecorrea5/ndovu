package usecase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

var quietLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func span(app, traceID string) domain.Span {
	return domain.Span{App: app, TraceID: traceID, SpanID: "s-" + traceID, StatusCode: domain.SpanStatusUnset}
}

func spansOf(app string, n int) []domain.Span {
	out := make([]domain.Span, n)
	for i := range out {
		out[i] = span(app, fmt.Sprintf("%032x", i))
	}
	return out
}

// ---------------------------------------------------------------------------
// Redação
// ---------------------------------------------------------------------------

func TestRedactSpan(t *testing.T) {
	s := domain.Span{
		Attrs: map[string]string{
			"http.request.header.authorization": "Bearer abc",
			"http.request.header.cookie":        "sid=1",
			"db.user.password":                  "hunter2",
			"url.full":                          "https://x.com/busca?email=a@b.com#top",
			"http.target":                       "/contato?cpf=123",
			"url.query":                         "token=1",
			"http.route":                        "/contato",
		},
		ResourceAttrs: map[string]string{"service.api_key": "k"},
		Events:        []domain.SpanEvent{{Name: "log", Attrs: map[string]string{"secret": "s"}}},
		Links:         []domain.SpanLink{{Attrs: map[string]string{"x-api-key": "k"}}},
	}
	RedactSpan(&s)

	want := map[string]string{
		"http.request.header.authorization": RedactedValue,
		"http.request.header.cookie":        RedactedValue,
		"db.user.password":                  RedactedValue,
		"url.full":                          "https://x.com/busca",
		"http.target":                       "/contato",
		"url.query":                         RedactedValue,
		"http.route":                        "/contato",
	}
	for k, v := range want {
		if s.Attrs[k] != v {
			t.Errorf("%s = %q, quero %q", k, s.Attrs[k], v)
		}
	}
	if s.ResourceAttrs["service.api_key"] != RedactedValue ||
		s.Events[0].Attrs["secret"] != RedactedValue ||
		s.Links[0].Attrs["x-api-key"] != RedactedValue {
		t.Error("resource, eventos e links também devem ser redigidos")
	}
}

type fakeSpanStream struct {
	got []domain.SpanBatch
	err error
}

func (f *fakeSpanStream) PublishSpans(_ context.Context, b domain.SpanBatch) error {
	f.got = append(f.got, b)
	return f.err
}

func TestSpanIngestRedigeAntesDePublicar(t *testing.T) {
	stream := &fakeSpanStream{}
	svc := NewSpanIngestService(stream, quietLogger)
	s := span("api", "t1")
	s.Attrs = map[string]string{"password": "x"}
	if err := svc.Ingest(context.Background(), "api", []domain.Span{s}); err != nil {
		t.Fatal(err)
	}
	if len(stream.got) != 1 || stream.got[0].App != "api" || stream.got[0].Spans[0].Attrs["password"] != RedactedValue {
		t.Fatalf("publicado = %+v", stream.got)
	}

	stream.err = errors.New("nats fora")
	if err := svc.Ingest(context.Background(), "api", []domain.Span{span("api", "t2")}); err == nil {
		t.Fatal("falha do stream precisa voltar para o handler responder 503")
	}
}

// ---------------------------------------------------------------------------
// Sampling por trace
// ---------------------------------------------------------------------------

func TestKeepTraceDeterministicoEProporcional(t *testing.T) {
	const n = 20000
	kept := 0
	for i := range n {
		id := fmt.Sprintf("%032x", i*7919)
		a := KeepTrace(id, 0.1)
		if a != KeepTrace(id, 0.1) {
			t.Fatal("mesmo trace_id deu decisões diferentes")
		}
		if a {
			kept++
		}
	}
	if frac := float64(kept) / n; frac < 0.08 || frac > 0.12 {
		t.Fatalf("taxa efetiva %.3f, quero ~0.10", frac)
	}
	if !KeepTrace("x", 1) || KeepTrace("x", 0) {
		t.Fatal("bordas 0 e 1")
	}
}

type fakeRules map[string]domain.SamplingRule

func (f fakeRules) SpanRule(app string) (domain.SamplingRule, bool) {
	r, ok := f[app]
	return r, ok
}

func TestFilterMantemTraceInteiroEErros(t *testing.T) {
	rules := fakeRules{"api": {SampleRate: 0, KeepErrors: true}}
	p := NewSpanPolicy(rules, nil, nil, SpanPolicyConfig{}, quietLogger)

	failed := span("api", "trace-com-erro")
	failed.StatusCode = domain.SpanStatusError
	sibling := span("api", "trace-com-erro")
	sibling.SpanID = "irmao"
	batch := domain.SpanBatch{App: "api", Spans: []domain.Span{span("api", "trace-ok"), failed, sibling}}

	got := p.Filter(context.Background(), batch)
	if len(got.Spans) != 2 {
		t.Fatalf("mantidos %d spans, quero os 2 do trace com erro", len(got.Spans))
	}
	for _, s := range got.Spans {
		if s.TraceID != "trace-com-erro" {
			t.Fatalf("trace %s não deveria passar com rate 0", s.TraceID)
		}
	}

	rules["api"] = domain.SamplingRule{SampleRate: 0, KeepErrors: false}
	if got := p.Filter(context.Background(), batch); len(got.Spans) != 0 {
		t.Fatalf("keepErrors=false com rate 0 deveria descartar tudo, sobrou %d", len(got.Spans))
	}
	if got := p.Filter(context.Background(), domain.SpanBatch{App: "outro", Spans: batch.Spans}); len(got.Spans) != 3 {
		t.Fatal("app sem regra mantém tudo")
	}
}

func TestSpanRuleSoValeParaEventTypeSpan(t *testing.T) {
	svc := NewSamplingService(&fakeSamplingStore{rules: []domain.SamplingRule{
		{App: "", EventType: "", SampleRate: 0.5, Active: true},           // regra de evento: não vale
		{App: "", EventType: SpanRuleType, SampleRate: 0.3, Active: true}, // global de span
		{App: "api", EventType: SpanRuleType, SampleRate: 0.1, Active: true},
	}}, nil, quietLogger)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r, ok := svc.SpanRule("api"); !ok || r.SampleRate != 0.1 {
		t.Fatalf("api: %+v %v", r, ok)
	}
	if r, ok := svc.SpanRule("web"); !ok || r.SampleRate != 0.3 {
		t.Fatalf("web deveria cair na global de span: %+v %v", r, ok)
	}
	// E o contrário: regra de span não vira regra de evento.
	if r, ok := svc.selectRule("api", string(domain.EventAction)); !ok || r.SampleRate != 0.5 {
		t.Fatalf("evento de api deveria usar a global de evento (0.5): %+v %v", r, ok)
	}
	if _, err := svc.Create(context.Background(), SamplingInput{EventType: SpanRuleType, SampleRate: 0.2}); err != nil {
		t.Fatalf("event_type span deveria ser aceito: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Cota diária
// ---------------------------------------------------------------------------

type quotaCall struct {
	app    string
	forced bool
}

type fakeNotifier struct {
	mu    sync.Mutex
	calls []quotaCall
}

func (f *fakeNotifier) QuotaChanged(_ context.Context, app string, forced bool, _, _ int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, quotaCall{app, forced})
}

type fakeSpanCounter struct {
	counts map[string]int
	since  time.Time
}

func (f *fakeSpanCounter) TraceSpans(context.Context, string, []string) ([]domain.Span, error) {
	return nil, nil
}

func (f *fakeSpanCounter) CountSpansByApp(_ context.Context, since time.Time) (map[string]int, error) {
	f.since = since
	return f.counts, nil
}

func TestCotaLigaSamplingForcadoAteAViradaDoDia(t *testing.T) {
	saoPaulo, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Skip("tzdata indisponível")
	}
	notifier := &fakeNotifier{}
	p := NewSpanPolicy(nil, nil, notifier, SpanPolicyConfig{DailyQuota: 100, ForcedRate: 0, Location: saoPaulo}, quietLogger)
	clock := time.Date(2026, 10, 22, 23, 0, 0, 0, saoPaulo)
	p.now = func() time.Time { return clock }
	ctx := context.Background()
	batch := domain.SpanBatch{App: "api", Spans: spansOf("api", 10)}

	p.Record(ctx, "api", 99)
	if got := p.Filter(ctx, batch); len(got.Spans) != 10 {
		t.Fatal("abaixo da cota grava tudo")
	}
	p.Record(ctx, "api", 1) // chegou em 100
	if got := p.Filter(ctx, batch); len(got.Spans) != 0 {
		t.Fatalf("na cota com taxa forçada 0, sobrou %d", len(got.Spans))
	}
	failed := span("api", "erro")
	failed.StatusCode = domain.SpanStatusError
	if got := p.Filter(ctx, domain.SpanBatch{App: "api", Spans: []domain.Span{failed}}); len(got.Spans) != 1 {
		t.Fatal("erro passa mesmo no modo forçado")
	}
	if got := p.Filter(ctx, domain.SpanBatch{App: "outro", Spans: spansOf("outro", 3)}); len(got.Spans) != 3 {
		t.Fatal("a cota é por app")
	}
	p.Record(ctx, "api", 50) // continua forçado: sem segundo aviso

	// 23:00 em São Paulo já é dia 23 em UTC — a virada é pelo fuso configurado.
	clock = time.Date(2026, 10, 23, 0, 0, 1, 0, saoPaulo)
	if got := p.Filter(ctx, batch); len(got.Spans) != 10 {
		t.Fatal("dia novo volta à regra normal")
	}

	want := []quotaCall{{"api", true}, {"api", false}}
	if fmt.Sprint(notifier.calls) != fmt.Sprint(want) {
		t.Fatalf("avisos = %v, quero %v", notifier.calls, want)
	}
}

func TestCotaReconstroiContagemNoBoot(t *testing.T) {
	notifier := &fakeNotifier{}
	counter := &fakeSpanCounter{counts: map[string]int{"api": 150, "web": 10}}
	p := NewSpanPolicy(nil, counter, notifier, SpanPolicyConfig{DailyQuota: 100, ForcedRate: 0, Location: time.UTC}, quietLogger)
	p.now = func() time.Time { return time.Date(2026, 10, 22, 15, 30, 0, 0, time.UTC) }

	if got := p.Filter(context.Background(), domain.SpanBatch{App: "api", Spans: spansOf("api", 5)}); len(got.Spans) != 0 {
		t.Fatal("app que já tinha estourado antes do restart continua forçado")
	}
	if !counter.since.Equal(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("contou desde %v, quero a meia-noite do dia", counter.since)
	}
	if len(notifier.calls) != 0 {
		t.Fatalf("restart não deve reavisar: %v", notifier.calls)
	}
}

// ---------------------------------------------------------------------------
// Persistência
// ---------------------------------------------------------------------------

type fakeSpanWriter struct {
	calls   int
	failAll bool
	failApp string
	saved   []domain.SpanBatch
}

func (f *fakeSpanWriter) SaveSpans(_ context.Context, batches []domain.SpanBatch) (int, error) {
	f.calls++
	if f.failAll && len(batches) > 1 {
		return 0, errors.New("insert combinado falhou")
	}
	n := 0
	for _, b := range batches {
		if b.App == f.failApp {
			return 0, errors.New("lote venenoso")
		}
		n += len(b.Spans)
	}
	f.saved = append(f.saved, batches...)
	return n, nil
}

func TestSpanWriteCaiParaLoteALoteEMandaVenenoParaDLQ(t *testing.T) {
	writer := &fakeSpanWriter{failAll: true, failApp: "ruim"}
	var dlq []string
	notifier := &fakeNotifier{}
	policy := NewSpanPolicy(nil, nil, notifier, SpanPolicyConfig{DailyQuota: 2, Location: time.UTC}, quietLogger)
	svc := NewSpanWriteService(writer, policy, func(_ context.Context, b domain.SpanBatch, _ string) error {
		dlq = append(dlq, b.App)
		return nil
	}, quietLogger)

	err := svc.HandleBatches(context.Background(), []domain.SpanBatch{
		{App: "bom", Spans: spansOf("bom", 2)},
		{App: "ruim", Spans: spansOf("ruim", 2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(writer.saved) != 1 || writer.saved[0].App != "bom" || fmt.Sprint(dlq) != "[ruim]" {
		t.Fatalf("saved=%v dlq=%v", writer.saved, dlq)
	}
	// Só o que foi gravado conta para a cota: "bom" chegou em 2, "ruim" não.
	if fmt.Sprint(notifier.calls) != fmt.Sprint([]quotaCall{{"bom", true}}) {
		t.Fatalf("avisos = %v", notifier.calls)
	}
}

func TestSpanWriteSemDLQReentrega(t *testing.T) {
	svc := NewSpanWriteService(&fakeSpanWriter{failApp: "ruim"}, nil, nil, quietLogger)
	if err := svc.HandleBatches(context.Background(), []domain.SpanBatch{{App: "ruim", Spans: spansOf("ruim", 1)}}); err == nil {
		t.Fatal("sem DLQ o erro precisa voltar para o stream reentregar")
	}
}

func TestQuotaAlertsGravaAuditEEnviaEmail(t *testing.T) {
	store := &fakeAuditStore{}
	mailer := &recordingMailer{}
	q := NewQuotaAlerts(NewAuditService(store, quietLogger), mailer, "ndovu@x", []string{"admin@x"}, 0.1, quietLogger)
	q.QuotaChanged(context.Background(), "api", true, 500000, 500000)
	q.QuotaChanged(context.Background(), "api", false, 0, 500000)

	if len(store.entries) != 2 || store.entries[0].Action != "otlp.quota.forced_sampling_on" ||
		store.entries[1].Action != "otlp.quota.forced_sampling_off" || store.entries[0].ResourceID != "api" {
		t.Fatalf("audit = %+v", store.entries)
	}
	if len(mailer.subjects) != 2 {
		t.Fatalf("e-mails = %v", mailer.subjects)
	}
}

type recordingMailer struct{ subjects []string }

func (m *recordingMailer) Send(_ string, _ []string, subject, _, _ string) error {
	m.subjects = append(m.subjects, subject)
	return nil
}

// ---------------------------------------------------------------------------
// Trace unificado
// ---------------------------------------------------------------------------

type traceReader struct {
	nopEventReader
	events []domain.TraceEvent
}

func (r traceReader) TraceTimeline(context.Context, string) ([]domain.TraceEvent, error) {
	return r.events, nil
}

type scopedSpanReader struct {
	fakeSpanCounter
	gotScope []string
}

func (r *scopedSpanReader) TraceSpans(_ context.Context, _ string, scope []string) ([]domain.Span, error) {
	r.gotScope = scope
	return []domain.Span{span("felpma-api", "t")}, nil
}

func TestTraceUneEventosESpansRespeitandoScope(t *testing.T) {
	reader := traceReader{events: []domain.TraceEvent{{App: "felpma"}, {App: "outra-empresa"}}}
	spans := &scopedSpanReader{}
	svc := NewQueryService(reader).WithSpans(spans)

	view, err := svc.Trace(context.Background(), " 4BF92F ", []string{"felpma", "felpma-api"})
	if err != nil {
		t.Fatal(err)
	}
	if view.TraceID != "4bf92f" {
		t.Fatalf("traceId normalizado = %q", view.TraceID)
	}
	if len(view.Events) != 1 || view.Events[0].App != "felpma" {
		t.Fatalf("evento de app fora do scope vazou: %+v", view.Events)
	}
	if len(view.Spans) != 1 || fmt.Sprint(spans.gotScope) != "[felpma felpma-api]" {
		t.Fatalf("spans=%d scope=%v", len(view.Spans), spans.gotScope)
	}

	all, err := svc.Trace(context.Background(), "t", nil)
	if err != nil || len(all.Events) != 2 {
		t.Fatalf("super-admin vê tudo: %+v %v", all.Events, err)
	}
	if _, err := svc.Trace(context.Background(), "  ", nil); err == nil {
		t.Fatal("traceId vazio deveria ser validação")
	}
}

func TestTraceSemSpanReaderDevolveListaVazia(t *testing.T) {
	view, err := NewQueryService(traceReader{}).Trace(context.Background(), "t", nil)
	if err != nil || view.Spans == nil || view.Events == nil {
		t.Fatalf("listas devem vir vazias, não null: %+v %v", view, err)
	}
}
