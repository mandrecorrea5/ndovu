package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// fakeFunnelStore materializa funnels em memória.
type fakeFunnelStore struct {
	items map[string]domain.Funnel
	seq   int
}

func newFakeFunnelStore() *fakeFunnelStore {
	return &fakeFunnelStore{items: map[string]domain.Funnel{}}
}

func (f *fakeFunnelStore) CreateFunnel(_ context.Context, fn domain.Funnel) (domain.Funnel, error) {
	f.seq++
	fn.ID = string(rune('A' + f.seq))
	fn.CreatedAt = time.Now().UTC()
	f.items[fn.ID] = fn
	return fn, nil
}
func (f *fakeFunnelStore) UpdateFunnel(_ context.Context, id, name string, ws int, steps json.RawMessage) (domain.Funnel, error) {
	fn, ok := f.items[id]
	if !ok {
		return domain.Funnel{}, domain.ErrNotFound
	}
	fn.Name = name
	fn.WindowSeconds = ws
	fn.Steps = steps
	f.items[id] = fn
	return fn, nil
}
func (f *fakeFunnelStore) ListFunnels(_ context.Context, app string) ([]domain.Funnel, error) {
	out := []domain.Funnel{}
	for _, fn := range f.items {
		if app == "" || fn.App == app {
			out = append(out, fn)
		}
	}
	return out, nil
}
func (f *fakeFunnelStore) GetFunnel(_ context.Context, id string) (domain.Funnel, error) {
	if fn, ok := f.items[id]; ok {
		return fn, nil
	}
	return domain.Funnel{}, domain.ErrNotFound
}
func (f *fakeFunnelStore) DeleteFunnel(_ context.Context, id string) error {
	if _, ok := f.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.items, id)
	return nil
}

// fakeFunnelReader captura o último FunnelRun recebido, pra assert.
type fakeFunnelReader struct {
	nopEventReader
	lastRun domain.FunnelRun
	result  domain.FunnelResult
}

func (r *fakeFunnelReader) RunFunnel(_ context.Context, run domain.FunnelRun) (domain.FunnelResult, error) {
	r.lastRun = run
	return r.result, nil
}

func newFunnelSvc() (*FunnelService, *fakeFunnelStore, *fakeFunnelReader) {
	store := newFakeFunnelStore()
	reader := &fakeFunnelReader{
		result: domain.FunnelResult{TotalSessions: 10, Steps: []domain.FunnelStepResult{
			{Name: "s1", Sessions: 10, OverallRate: 1.0, StepConversion: 1.0},
			{Name: "s2", Sessions: 4, OverallRate: 0.4, StepConversion: 0.4},
		}},
	}
	return NewFunnelService(store, reader), store, reader
}

// -- validateFunnel ----------------------------------------------------

func TestFunnel_ValidacaoCriacao(t *testing.T) {
	svc, _, _ := newFunnelSvc()
	validSteps := []domain.FunnelStep{
		{Name: "s1", Match: domain.FunnelStepMatch{Type: "page_view"}},
		{Name: "s2", Match: domain.FunnelStepMatch{Type: "action"}},
	}
	cases := []struct {
		name string
		in   CreateFunnelInput
	}{
		{"app vazio", CreateFunnelInput{Name: "f", Steps: validSteps}},
		{"name vazio", CreateFunnelInput{App: "a", Steps: validSteps}},
		{"1 step só", CreateFunnelInput{App: "a", Name: "f", Steps: validSteps[:1]}},
		{"9 steps (máx 8)", CreateFunnelInput{
			App: "a", Name: "f", Steps: repeatStep("page_view", 9),
		}},
		{"step sem name", CreateFunnelInput{
			App: "a", Name: "f", Steps: []domain.FunnelStep{
				{Match: domain.FunnelStepMatch{Type: "page_view"}},
				{Name: "ok", Match: domain.FunnelStepMatch{Type: "action"}},
			},
		}},
		{"type inválido", CreateFunnelInput{
			App: "a", Name: "f", Steps: []domain.FunnelStep{
				{Name: "s1", Match: domain.FunnelStepMatch{Type: "click"}}, // inválido
				{Name: "s2", Match: domain.FunnelStepMatch{Type: "action"}},
			},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.Create(context.Background(), c.in)
			if err == nil {
				t.Fatal("esperava erro de validação")
			}
			var ve *domain.ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("esperava ValidationError, veio %T", err)
			}
		})
	}
}

func repeatStep(tp string, n int) []domain.FunnelStep {
	out := make([]domain.FunnelStep, n)
	for i := range out {
		out[i] = domain.FunnelStep{
			Name:  "step-" + string(rune('a'+i)),
			Match: domain.FunnelStepMatch{Type: tp},
		}
	}
	return out
}

func TestFunnel_CreateHappyPathComWindowDefault(t *testing.T) {
	svc, store, _ := newFunnelSvc()
	fn, err := svc.Create(context.Background(), CreateFunnelInput{
		App:  "portal", Name: "onboarding",
		Steps: repeatStep("action", 3),
		// WindowSeconds omitido → default 1800
	})
	if err != nil {
		t.Fatalf("create falhou: %v", err)
	}
	if fn.WindowSeconds != 1800 {
		t.Errorf("window default esperado 1800, veio %d", fn.WindowSeconds)
	}
	if store.items[fn.ID].App != "portal" {
		t.Errorf("app não persistiu")
	}
	// Steps serializados como JSON, deve ser válido.
	var steps []domain.FunnelStep
	if err := json.Unmarshal(fn.Steps, &steps); err != nil {
		t.Errorf("steps não serializou como JSON: %v", err)
	}
}

func TestFunnel_CreateTrimAppEName(t *testing.T) {
	svc, store, _ := newFunnelSvc()
	fn, _ := svc.Create(context.Background(), CreateFunnelInput{
		App: "  portal  ", Name: "  cadastro  ",
		Steps: repeatStep("action", 2),
	})
	if store.items[fn.ID].App != "portal" || store.items[fn.ID].Name != "cadastro" {
		t.Errorf("app/name não trimmed: app=%q name=%q", store.items[fn.ID].App, store.items[fn.ID].Name)
	}
}

func TestFunnel_UpdateSemStepsFalha(t *testing.T) {
	svc, _, _ := newFunnelSvc()
	// Cria válido primeiro.
	fn, _ := svc.Create(context.Background(), CreateFunnelInput{
		App: "a", Name: "f", Steps: repeatStep("action", 2),
	})
	// Update sem steps → falha (validate rejeita).
	_, err := svc.Update(context.Background(), fn.ID, UpdateFunnelInput{
		Name: "novo", Steps: nil,
	})
	if err == nil {
		t.Fatal("update sem steps deveria falhar")
	}
}

func TestFunnel_RunUsaConfigPersistida(t *testing.T) {
	svc, _, reader := newFunnelSvc()
	from := time.Now().Add(-time.Hour)
	to := time.Now()
	fn, _ := svc.Create(context.Background(), CreateFunnelInput{
		App: "portal", Name: "f", WindowSeconds: 900,
		Steps: repeatStep("action", 2),
	})

	res, err := svc.Run(context.Background(), fn.ID, &from, &to)
	if err != nil {
		t.Fatalf("run falhou: %v", err)
	}
	if res.TotalSessions != 10 {
		t.Errorf("result não veio do reader: %+v", res)
	}
	// Config passada pro reader deve refletir a persistida.
	if reader.lastRun.App != "portal" || reader.lastRun.WindowSeconds != 900 {
		t.Errorf("run config errada: %+v", reader.lastRun)
	}
	if len(reader.lastRun.Steps) != 2 {
		t.Errorf("steps não foram deserializados: %d", len(reader.lastRun.Steps))
	}
}

func TestFunnel_PreviewValidaAntesDeRodar(t *testing.T) {
	svc, _, _ := newFunnelSvc()
	// Steps inválidos (só 1).
	_, err := svc.Preview(context.Background(), FunnelRunInput{
		App: "a", Steps: repeatStep("action", 1),
	})
	if err == nil {
		t.Fatal("preview deveria validar")
	}
}

func TestFunnel_PreviewHappyPath(t *testing.T) {
	svc, _, reader := newFunnelSvc()
	_, err := svc.Preview(context.Background(), FunnelRunInput{
		App: "portal", WindowSeconds: 600, Steps: repeatStep("page_view", 2),
	})
	if err != nil {
		t.Fatalf("preview falhou: %v", err)
	}
	if reader.lastRun.WindowSeconds != 600 {
		t.Errorf("preview config errada: %+v", reader.lastRun)
	}
}

// -- IsValidType (contrato) --------------------------------------------

func TestFunnel_IsValidType(t *testing.T) {
	for _, tp := range []string{"page_view", "action", "http_request", "error", "custom"} {
		if !isValidType(tp) {
			t.Errorf("%q deveria ser válido", tp)
		}
	}
	for _, tp := range []string{"", "click", "PAGEVIEW", "pageview"} {
		if isValidType(tp) {
			t.Errorf("%q não deveria ser válido", tp)
		}
	}
}

// -- Guardas contra typos em type/name --------------------------------

func TestFunnel_ValidacaoMostraSteps(t *testing.T) {
	// Confirma que a mensagem menciona o step numerado.
	svc, _, _ := newFunnelSvc()
	_, err := svc.Create(context.Background(), CreateFunnelInput{
		App: "a", Name: "f",
		Steps: []domain.FunnelStep{
			{Name: "ok", Match: domain.FunnelStepMatch{Type: "page_view"}},
			{Name: "", Match: domain.FunnelStepMatch{Type: "action"}},
		},
	})
	var ve *domain.ValidationError
	_ = errors.As(err, &ve)
	joined := strings.Join(ve.Issues, "|")
	if !strings.Contains(joined, "step 2") {
		t.Errorf("issues deveriam mencionar 'step 2': %v", ve.Issues)
	}
}
