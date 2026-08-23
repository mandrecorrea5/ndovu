package usecase

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// fakeSamplingStore serve os testes de precedência sem tocar Postgres.
type fakeSamplingStore struct{ rules []domain.SamplingRule }

func (f *fakeSamplingStore) ListSamplingRules(context.Context) ([]domain.SamplingRule, error) {
	return f.rules, nil
}
func (f *fakeSamplingStore) CreateSamplingRule(context.Context, domain.SamplingRule) (domain.SamplingRule, error) {
	return domain.SamplingRule{}, nil
}
func (f *fakeSamplingStore) UpdateSamplingRule(context.Context, string, domain.SamplingRule) (domain.SamplingRule, error) {
	return domain.SamplingRule{}, nil
}
func (f *fakeSamplingStore) DeleteSamplingRule(context.Context, string) error { return nil }

func newSamplingSvc(rules ...domain.SamplingRule) *SamplingService {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewSamplingService(&fakeSamplingStore{rules: rules}, nil, logger)
	_ = svc.Refresh(context.Background())
	return svc
}

func TestSamplingMaisEspecificaGanha(t *testing.T) {
	svc := newSamplingSvc(
		domain.SamplingRule{App: "", EventType: "", SampleRate: 0.1, Active: true},
		domain.SamplingRule{App: "portal", EventType: "", SampleRate: 0.5, Active: true},
		domain.SamplingRule{App: "portal", EventType: "action", SampleRate: 1.0, KeepErrors: true, Active: true},
	)

	// A regra (portal, action) tem score 2 → escolhida.
	e := domain.TraceEvent{App: "portal", Type: domain.EventAction}
	// Chama várias vezes para reduzir chance de false-positive; com rate=1.0 sempre passa.
	for i := 0; i < 20; i++ {
		if !svc.ShouldKeep(e) {
			t.Fatalf("evento (portal, action) devia sempre passar (rate=1.0), foi dropado")
		}
	}
}

func TestSamplingKeepErrorsBypass(t *testing.T) {
	svc := newSamplingSvc(
		domain.SamplingRule{App: "portal", EventType: "http_request",
			SampleRate: 0, KeepErrors: true, Active: true},
	)

	// Evento normal com essa regra deveria ser dropado (rate=0).
	normal := domain.TraceEvent{App: "portal", Type: domain.EventHTTPRequest}
	if svc.ShouldKeep(normal) {
		t.Fatalf("evento normal deveria ser dropado com rate=0")
	}

	// Mas evento com http_status 500+ passa mesmo com rate=0 (keep_errors).
	status := 500
	e := domain.TraceEvent{
		App:  "portal",
		Type: domain.EventHTTPRequest,
		HTTP: &domain.HTTPInfo{StatusCode: &status},
	}
	if !svc.ShouldKeep(e) {
		t.Fatalf("evento 5xx devia passar por causa de keep_errors, foi dropado")
	}

	// Evento com Error também bypassa.
	errEvt := domain.TraceEvent{
		App:   "portal",
		Type:  domain.EventHTTPRequest,
		Error: &domain.ErrorInfo{Code: "X"},
	}
	if !svc.ShouldKeep(errEvt) {
		t.Fatalf("evento com error devia passar por keep_errors")
	}
}

func TestSamplingKeepErrorsFalseDropaErro(t *testing.T) {
	// keep_errors=false: até erros são amostrados.
	svc := newSamplingSvc(
		domain.SamplingRule{App: "portal", EventType: "error",
			SampleRate: 0, KeepErrors: false, Active: true},
	)
	e := domain.TraceEvent{App: "portal", Type: domain.EventError,
		Error: &domain.ErrorInfo{Code: "X"}}
	if svc.ShouldKeep(e) {
		t.Fatalf("com keep_errors=false, erro deve ser dropado quando rate=0")
	}
}

func TestSamplingSemRegraMantemTudo(t *testing.T) {
	svc := newSamplingSvc() // nenhuma regra
	e := domain.TraceEvent{App: "portal", Type: domain.EventAction}
	if !svc.ShouldKeep(e) {
		t.Fatalf("sem regra deve manter tudo")
	}
}

func TestSamplingRegraInativaNaoConta(t *testing.T) {
	svc := newSamplingSvc(
		domain.SamplingRule{App: "portal", EventType: "action",
			SampleRate: 0, Active: false},
	)
	e := domain.TraceEvent{App: "portal", Type: domain.EventAction}
	if !svc.ShouldKeep(e) {
		t.Fatalf("regra inativa não deveria dropar; deveria cair no default (1.0)")
	}
}

func TestSamplingFiltraBatch(t *testing.T) {
	svc := newSamplingSvc(
		domain.SamplingRule{App: "portal", EventType: "action",
			SampleRate: 0, KeepErrors: true, Active: true},
	)
	status5xx := 500
	batch := domain.IngestBatch{Events: []domain.TraceEvent{
		{App: "portal", Type: domain.EventAction},                                   // dropado
		{App: "portal", Type: domain.EventAction, Error: &domain.ErrorInfo{Code: "X"}}, // mantém (keep_errors)
		{App: "portal", Type: domain.EventPageView},                                 // mantém (não bate a regra)
		{App: "portal", Type: domain.EventHTTPRequest,                               // mantém (não bate)
			HTTP: &domain.HTTPInfo{StatusCode: &status5xx}},
	}}
	out := svc.FilterBatch(batch)
	if len(out.Events) != 3 {
		t.Fatalf("esperado 3 eventos, veio %d", len(out.Events))
	}
}
