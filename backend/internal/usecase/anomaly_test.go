package usecase

import (
	"context"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// fakeAnomalyStore para orquestrar cenários sem Postgres.
type fakeAnomalyStore struct {
	rules      []domain.AnomalyRule
	last       map[string]domain.AnomalyDetection
	detections []domain.AnomalyDetection
}

func newFakeAnomalyStore(rules ...domain.AnomalyRule) *fakeAnomalyStore {
	return &fakeAnomalyStore{rules: rules, last: map[string]domain.AnomalyDetection{}}
}

func (f *fakeAnomalyStore) CreateAnomalyRule(context.Context, domain.AnomalyRule) (domain.AnomalyRule, error) {
	return domain.AnomalyRule{}, nil
}
func (f *fakeAnomalyStore) UpdateAnomalyRule(context.Context, string, domain.AnomalyRule) (domain.AnomalyRule, error) {
	return domain.AnomalyRule{}, nil
}
func (f *fakeAnomalyStore) DeleteAnomalyRule(context.Context, string) error { return nil }
func (f *fakeAnomalyStore) ListAnomalyRules(context.Context) ([]domain.AnomalyRule, error) {
	return f.rules, nil
}
func (f *fakeAnomalyStore) LastDetection(_ context.Context, ruleID string) (domain.AnomalyDetection, error) {
	if d, ok := f.last[ruleID]; ok {
		return d, nil
	}
	return domain.AnomalyDetection{}, domain.ErrNotFound
}
func (f *fakeAnomalyStore) RecordDetection(_ context.Context, d domain.AnomalyDetection) error {
	d.DetectedAt = time.Now().UTC()
	f.last[d.RuleID] = d
	f.detections = append(f.detections, d)
	return nil
}
func (f *fakeAnomalyStore) ListDetections(context.Context, int, int) ([]domain.AnomalyDetection, int, error) {
	return f.detections, len(f.detections), nil
}

// fakeReader devolve valores planejados para MetricInWindow. A cada chamada
// consume um valor da fila `values`.
type fakeReader struct {
	values []float64
	pos    int
}

func (r *fakeReader) MetricInWindow(context.Context, string, string, time.Time, time.Time) (float64, error) {
	v := r.values[r.pos%len(r.values)]
	r.pos++
	return v, nil
}

// Métodos não usados por esse teste.
func (r *fakeReader) FindEvents(context.Context, domain.EventFilter) (domain.EventPage, error) {
	return domain.EventPage{}, nil
}
func (r *fakeReader) GetEvent(context.Context, string) (domain.TraceEvent, error) {
	return domain.TraceEvent{}, nil
}
func (r *fakeReader) FindSessions(context.Context, domain.SessionFilter) (domain.SessionPage, error) {
	return domain.SessionPage{}, nil
}
func (r *fakeReader) GetSession(context.Context, string) (domain.Session, error) {
	return domain.Session{}, nil
}
func (r *fakeReader) SessionTimeline(context.Context, string) ([]domain.TraceEvent, error) {
	return nil, nil
}
func (r *fakeReader) GetOverview(context.Context, time.Time, time.Time, string) (domain.Overview, error) {
	return domain.Overview{}, nil
}
func (r *fakeReader) GetFilterOptions(context.Context) (domain.FilterOptions, error) {
	return domain.FilterOptions{}, nil
}
func (r *fakeReader) FindIssues(context.Context, domain.IssueFilter) ([]domain.Issue, error) {
	return nil, nil
}
func (r *fakeReader) FindWebVitals(context.Context, domain.WebVitalFilter) ([]domain.WebVitalStat, error) {
	return nil, nil
}
func (r *fakeReader) CountErrorsSince(context.Context, string, string, time.Time) (int, error) {
	return 0, nil
}
func (r *fakeReader) FindReleases(context.Context, domain.ReleaseFilter) ([]domain.Release, error) {
	return nil, nil
}
func (r *fakeReader) GetRelease(context.Context, string, string, time.Time, time.Time) (domain.Release, error) {
	return domain.Release{}, nil
}
func (r *fakeReader) RunFunnel(context.Context, domain.FunnelRun) (domain.FunnelResult, error) {
	return domain.FunnelResult{}, nil
}
func (r *fakeReader) Retention(context.Context, domain.RetentionFilter) (domain.RetentionResult, error) {
	return domain.RetentionResult{}, nil
}
func (r *fakeReader) TraceTimeline(context.Context, string) ([]domain.TraceEvent, error) {
	return nil, nil
}
func (r *fakeReader) FindEventsByUser(context.Context, string) ([]domain.TraceEvent, error) {
	return nil, nil
}
func (r *fakeReader) DeleteEventsByUser(context.Context, string) error { return nil }

func silentAnomalyLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStatsOfMediaEDesvio(t *testing.T) {
	avg, sd := statsOf([]float64{10, 20, 30, 40, 50})
	if math.Abs(avg-30) > 1e-6 {
		t.Fatalf("média esperada 30, veio %v", avg)
	}
	if math.Abs(sd-math.Sqrt(200)) > 1e-6 {
		t.Fatalf("stddev esperado sqrt(200), veio %v", sd)
	}
}

func TestAnomalyDispara_SpikeAlem_da_Sensitivity(t *testing.T) {
	// Baseline = [10,10,10,10] (média 10, stddev 0). Atual = 100.
	// Z-score enorme; direction=above deve disparar.
	rule := domain.AnomalyRule{
		ID: "r1", Name: "err spike", Active: true,
		Metric: domain.AnomalyErrorCount, WindowMinutes: 15, BaselineWeeks: 4,
		Sensitivity: 3, Direction: domain.AnomalyAbove,
		SilenceSeconds: 60, Channel: domain.AlertChannelWebhook,
		TargetURL: "http://ignored.local/webhook",
	}
	store := newFakeAnomalyStore(rule)
	// Ordem das chamadas MetricInWindow: 1a = janela atual, depois cada semana.
	reader := &fakeReader{values: []float64{100, 10, 10, 10, 10}}
	// Dispatcher inline sem HTTP real — sobrescreve http.Client para bloquear rede.
	svc := &AnomalyService{
		store: store, reader: reader,
		dispatcher: NewAlertDispatcher(), // vai falhar no dispatch (URL inexistente)
		logger:     silentAnomalyLogger(),
		now:        func() time.Time { return time.Date(2026, 8, 23, 14, 0, 0, 0, time.UTC) },
	}
	svc.evaluate(context.Background(), rule)
	if len(store.detections) != 1 {
		t.Fatalf("esperava 1 detection, veio %d", len(store.detections))
	}
	d := store.detections[0]
	if d.CurrentValue != 100 {
		t.Fatalf("current=%v, esperado 100", d.CurrentValue)
	}
	if d.BaselineAvg != 10 {
		t.Fatalf("avg=%v, esperado 10", d.BaselineAvg)
	}
	// NotifyOK deve ser false pq a URL não existe — mas a detecção grava mesmo assim.
	if d.NotifyOK {
		t.Fatalf("esperava NotifyOK=false (URL inexistente)")
	}
}

func TestAnomaliaNaoDispara_QuandoDentroDoBaseline(t *testing.T) {
	rule := domain.AnomalyRule{
		ID: "r1", Name: "err", Active: true,
		Metric: domain.AnomalyErrorCount, WindowMinutes: 15, BaselineWeeks: 4,
		Sensitivity: 3, Direction: domain.AnomalyAbove,
		Channel: domain.AlertChannelWebhook, TargetURL: "http://x.local",
	}
	store := newFakeAnomalyStore(rule)
	// Atual 12, baseline média 10 stddev ~1.4 → z ~1.4 (< 3). Não dispara.
	reader := &fakeReader{values: []float64{12, 8, 10, 12, 10}}
	svc := &AnomalyService{
		store: store, reader: reader,
		dispatcher: NewAlertDispatcher(),
		logger:     silentAnomalyLogger(),
		now:        func() time.Time { return time.Date(2026, 8, 23, 14, 0, 0, 0, time.UTC) },
	}
	svc.evaluate(context.Background(), rule)
	if len(store.detections) != 0 {
		t.Fatalf("não devia disparar (z dentro do baseline), veio %d detections", len(store.detections))
	}
}

func TestSilenceWindow_ImpedeReDisparo(t *testing.T) {
	rule := domain.AnomalyRule{
		ID: "r1", Name: "err", Active: true,
		Metric: domain.AnomalyErrorCount, WindowMinutes: 15, BaselineWeeks: 4,
		Sensitivity: 3, Direction: domain.AnomalyAbove, SilenceSeconds: 3600,
		Channel: domain.AlertChannelWebhook, TargetURL: "http://x.local",
	}
	store := newFakeAnomalyStore(rule)
	// Já tem uma detection há 10min — dentro dos 3600s de silêncio.
	store.last["r1"] = domain.AnomalyDetection{
		RuleID: "r1", DetectedAt: time.Now().Add(-10 * time.Minute),
	}
	reader := &fakeReader{values: []float64{500, 10, 10, 10, 10}} // spike forte
	svc := &AnomalyService{
		store: store, reader: reader,
		dispatcher: NewAlertDispatcher(),
		logger:     silentAnomalyLogger(),
		now:        time.Now,
	}
	svc.evaluate(context.Background(), rule)
	if len(store.detections) != 0 {
		t.Fatalf("silence window deveria bloquear, mas gravou %d detections",
			len(store.detections))
	}
}
