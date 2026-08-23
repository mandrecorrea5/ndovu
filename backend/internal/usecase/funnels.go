package usecase

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// FunnelService faz CRUD de funis (Postgres) e delega a execução do funil
// ao EventReader (ClickHouse). CRUD e execução ficam separados: um funil é
// definido uma vez e rodado várias com janelas diferentes.
type FunnelService struct {
	store  domain.FunnelStore
	reader domain.EventReader
}

// NewFunnelService cria o serviço.
func NewFunnelService(store domain.FunnelStore, reader domain.EventReader) *FunnelService {
	return &FunnelService{store: store, reader: reader}
}

// CreateFunnelInput são os dados de criação.
type CreateFunnelInput struct {
	App           string
	Name          string
	WindowSeconds int
	Steps         []domain.FunnelStep
	CreatedBy     string
}

// Create valida e persiste um funil.
func (s *FunnelService) Create(ctx context.Context, in CreateFunnelInput) (domain.Funnel, error) {
	if err := validateFunnel(in.App, in.Name, in.Steps); err != nil {
		return domain.Funnel{}, err
	}
	if in.WindowSeconds <= 0 {
		in.WindowSeconds = 1800
	}
	stepsJSON, err := json.Marshal(in.Steps)
	if err != nil {
		return domain.Funnel{}, err
	}
	return s.store.CreateFunnel(ctx, domain.Funnel{
		App:           strings.TrimSpace(in.App),
		Name:          strings.TrimSpace(in.Name),
		WindowSeconds: in.WindowSeconds,
		Steps:         stepsJSON,
		CreatedBy:     in.CreatedBy,
	})
}

// UpdateFunnelInput são os campos editáveis.
type UpdateFunnelInput struct {
	Name          string
	WindowSeconds int
	Steps         []domain.FunnelStep
}

// Update altera nome/janela/steps.
func (s *FunnelService) Update(ctx context.Context, id string, in UpdateFunnelInput) (domain.Funnel, error) {
	if err := validateFunnel("_", in.Name, in.Steps); err != nil {
		return domain.Funnel{}, err
	}
	if in.WindowSeconds <= 0 {
		in.WindowSeconds = 1800
	}
	stepsJSON, err := json.Marshal(in.Steps)
	if err != nil {
		return domain.Funnel{}, err
	}
	return s.store.UpdateFunnel(ctx, id, strings.TrimSpace(in.Name), in.WindowSeconds, stepsJSON)
}

// List lista funis, opcional filtro por app.
func (s *FunnelService) List(ctx context.Context, app string) ([]domain.Funnel, error) {
	return s.store.ListFunnels(ctx, app)
}

// Get retorna um funil por id.
func (s *FunnelService) Get(ctx context.Context, id string) (domain.Funnel, error) {
	return s.store.GetFunnel(ctx, id)
}

// Delete remove por id.
func (s *FunnelService) Delete(ctx context.Context, id string) error {
	return s.store.DeleteFunnel(ctx, id)
}

// Run executa o funil na janela pedida.
func (s *FunnelService) Run(ctx context.Context, id string, from, to *time.Time) (domain.FunnelResult, error) {
	f, err := s.store.GetFunnel(ctx, id)
	if err != nil {
		return domain.FunnelResult{}, err
	}
	var steps []domain.FunnelStep
	if err := json.Unmarshal(f.Steps, &steps); err != nil {
		return domain.FunnelResult{}, err
	}
	return s.reader.RunFunnel(ctx, domain.FunnelRun{
		App:           f.App,
		WindowSeconds: f.WindowSeconds,
		Steps:         steps,
		From:          from,
		To:            to,
	})
}

// Preview executa um funil ad-hoc (sem persistir) — útil pra visualizar
// no editor antes de salvar.
func (s *FunnelService) Preview(ctx context.Context, in FunnelRunInput) (domain.FunnelResult, error) {
	if err := validateFunnel(in.App, "preview", in.Steps); err != nil {
		return domain.FunnelResult{}, err
	}
	return s.reader.RunFunnel(ctx, domain.FunnelRun{
		App:           in.App,
		WindowSeconds: in.WindowSeconds,
		Steps:         in.Steps,
		From:          in.From,
		To:            in.To,
	})
}

// FunnelRunInput é o payload da preview.
type FunnelRunInput struct {
	App           string
	WindowSeconds int
	Steps         []domain.FunnelStep
	From          *time.Time
	To            *time.Time
}

func validateFunnel(app, name string, steps []domain.FunnelStep) error {
	var issues []string
	if strings.TrimSpace(app) == "" {
		issues = append(issues, "app é obrigatório")
	}
	if strings.TrimSpace(name) == "" {
		issues = append(issues, "name é obrigatório")
	}
	if len(steps) < 2 {
		issues = append(issues, "funil precisa de pelo menos 2 steps")
	}
	if len(steps) > 8 {
		issues = append(issues, "funil aceita no máximo 8 steps")
	}
	for i, s := range steps {
		if strings.TrimSpace(s.Name) == "" {
			issues = append(issues, "step "+itoa(i+1)+": name é obrigatório")
		}
		if !isValidType(s.Match.Type) {
			issues = append(issues, "step "+itoa(i+1)+": type deve ser page_view|action|http_request|error|custom")
		}
	}
	if len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	return nil
}

func isValidType(t string) bool {
	switch t {
	case "page_view", "action", "http_request", "error", "custom":
		return true
	}
	return false
}

// itoa evita importar strconv só para uma formatação de número pequeno.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
