package domain

import (
	"context"
	"time"
)

// IngestBatch é a unidade de escrita: uma sessão + seus eventos.
type IngestBatch struct {
	Session Session
	Events  []TraceEvent
}

// IngestResult resume o resultado de um lote (eventos enfileirados/persistidos).
type IngestResult struct {
	Accepted int `json:"accepted"`
}

// EventFilter expressa tudo que o usuário da ferramenta pode combinar
// para consultar traces. Campos zero-value são ignorados.
type EventFilter struct {
	From       *time.Time
	To         *time.Time
	App        string
	AppScope   []string // whitelist de apps que o requester pode ver (multi-tenant). Vazio = sem restrição (super-admin).
	Release    string
	UserID     string
	SessionID  string
	Type       EventType
	Feature    string
	Name       string
	Screen     string
	HTTPURL    string // match parcial (ILIKE)
	StatusMin  *int
	StatusMax  *int
	OnlyErrors bool
	Search     string // busca textual em name/screen/url/error
	Limit      int
	Cursor     *Cursor
}

// Cursor implementa paginação keyset (occurred_at DESC, id DESC).
type Cursor struct {
	OccurredAt time.Time
	ID         string
}

// SessionFilter filtra a listagem de sessões.
type SessionFilter struct {
	From       *time.Time
	To         *time.Time
	App        string
	AppScope   []string
	UserID     string
	OnlyErrors bool
	Limit      int
	Offset     int
}

// EventPage é uma página de eventos com o cursor da próxima.
type EventPage struct {
	Events     []TraceEvent `json:"events"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

// SessionPage é uma página de sessões.
type SessionPage struct {
	Sessions []Session `json:"sessions"`
	Total    int       `json:"total"`
}

// TimeBucket é um ponto da série temporal de eventos.
type TimeBucket struct {
	Bucket time.Time `json:"bucket"`
	Total  int       `json:"total"`
	Errors int       `json:"errors"`
}

// RouteStat agrega uma rota HTTP.
type RouteStat struct {
	URL    string  `json:"url"`
	Count  int     `json:"count"`
	Errors int     `json:"errors"`
	AvgMs  float64 `json:"avgMs"`
	P95Ms  float64 `json:"p95Ms"`
}

// ErrorStat agrega um código de erro.
type ErrorStat struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

// Overview resume a janela consultada.
type Overview struct {
	TotalEvents   int          `json:"totalEvents"`
	TotalSessions int          `json:"totalSessions"`
	TotalUsers    int          `json:"totalUsers"`
	TotalErrors   int          `json:"totalErrors"`
	ErrorRate     float64      `json:"errorRate"`
	AvgDurationMs float64      `json:"avgDurationMs"`
	Series        []TimeBucket `json:"series"`
	TopRoutes     []RouteStat  `json:"topRoutes"`
	TopErrors     []ErrorStat  `json:"topErrors"`
}

// FilterOptions alimenta os dropdowns do dashboard com o que existe na base.
type FilterOptions struct {
	Apps     []string `json:"apps"`
	Types    []string `json:"types"`
	Features []string `json:"features"`
	Names    []string `json:"names"`
}

// EventStream é o port de publicação: a API de ingestão valida o contrato e
// publica o lote no stream durável — nunca espera o banco. Isso desacopla a
// ingestão da persistência (a API não é gargalo nem perde eventos).
type EventStream interface {
	Publish(ctx context.Context, batch IngestBatch) error
}

// EventWriter é o port de escrita no armazenamento analítico. Insert-only por
// design; recebe vários lotes de uma vez porque o ClickHouse é mais eficiente
// com inserts grandes.
type EventWriter interface {
	SaveBatches(ctx context.Context, batches []IngestBatch) (IngestResult, error)
}

// EventReader é o port de leitura. Nenhum método de mutação existe aqui.
type EventReader interface {
	FindEvents(ctx context.Context, f EventFilter) (EventPage, error)
	GetEvent(ctx context.Context, id string) (TraceEvent, error)
	FindSessions(ctx context.Context, f SessionFilter) (SessionPage, error)
	GetSession(ctx context.Context, sessionID string) (Session, error)
	SessionTimeline(ctx context.Context, sessionID string) ([]TraceEvent, error)
	GetOverview(ctx context.Context, from, to time.Time, app string) (Overview, error)
	GetFilterOptions(ctx context.Context) (FilterOptions, error)
	FindIssues(ctx context.Context, f IssueFilter) ([]Issue, error)
	FindWebVitals(ctx context.Context, f WebVitalFilter) ([]WebVitalStat, error)
	CountErrorsSince(ctx context.Context, app, code string, since time.Time) (int, error)
	// MetricInWindow calcula uma métrica escalar sobre a janela [from, to).
	// Usado pela detecção de anomalia (baseline móvel sazonal).
	MetricInWindow(ctx context.Context, metric string, app string, from, to time.Time) (float64, error)
	FindReleases(ctx context.Context, f ReleaseFilter) ([]Release, error)
	GetRelease(ctx context.Context, app, release string, from, to time.Time) (Release, error)
	RunFunnel(ctx context.Context, f FunnelRun) (FunnelResult, error)
	Retention(ctx context.Context, f RetentionFilter) (RetentionResult, error)
	TraceTimeline(ctx context.Context, traceID string) ([]TraceEvent, error)
	FindEventsByUser(ctx context.Context, userID string) ([]TraceEvent, error)
	DeleteEventsByUser(ctx context.Context, userID string) error
}

// FunnelStepMatch identifica quais eventos "fecham" o step. Todos os campos
// preenchidos precisam bater (AND). type é obrigatório.
type FunnelStepMatch struct {
	Type    string `json:"type"`              // page_view | action | http_request | error | custom
	Name    string `json:"name,omitempty"`
	Screen  string `json:"screen,omitempty"`
	HTTPURL string `json:"httpUrl,omitempty"` // match parcial (positionCaseInsensitive)
	Feature string `json:"feature,omitempty"`
}

// FunnelStep é um passo do funil.
type FunnelStep struct {
	Name  string          `json:"name"`
	Match FunnelStepMatch `json:"match"`
}

// FunnelRun é a query on-demand contra o ClickHouse.
type FunnelRun struct {
	App           string
	WindowSeconds int
	Steps         []FunnelStep
	From          *time.Time
	To            *time.Time
}

// FunnelResult tem a contagem por step e a taxa acumulada.
type FunnelResult struct {
	TotalSessions int                `json:"totalSessions"`
	Steps         []FunnelStepResult `json:"steps"`
}

// FunnelStepResult é um passo com contagem, conversão desde o step 0 e
// conversão desde o step anterior (drop-off nesse passo).
type FunnelStepResult struct {
	Name             string  `json:"name"`
	Sessions         int     `json:"sessions"`
	OverallRate      float64 `json:"overallRate"`      // fração vs step 0
	StepConversion   float64 `json:"stepConversion"`   // fração vs step anterior
	DropoffFromPrev  int     `json:"dropoffFromPrev"`  // sessões perdidas neste passo
}

// RetentionFilter define a análise de retenção.
type RetentionFilter struct {
	App       string
	From      *time.Time
	To        *time.Time
	CohortBy  string  // "week" (default) — futuramente "day", "month"
	OffsetsDays []int // ex.: [1, 7, 14, 30]
}

// RetentionCohort é uma linha da matriz de cohort.
type RetentionCohort struct {
	Cohort      time.Time         `json:"cohort"`      // início do período
	NewUsers    int               `json:"newUsers"`    // users que viram no cohort
	Retained    map[string]int    `json:"retained"`    // "D1" -> N, "D7" -> N, ...
	RetainedPct map[string]float64 `json:"retainedPct"`
}

// RetentionResult agrupa cohorts.
type RetentionResult struct {
	CohortBy    string            `json:"cohortBy"`
	OffsetsDays []int             `json:"offsetsDays"`
	Cohorts     []RetentionCohort `json:"cohorts"`
}

// IssueFilter filtra a listagem de issues agregadas.
type IssueFilter struct {
	From     *time.Time
	To       *time.Time
	App      string
	AppScope []string
	Release  string
	Limit    int
}

// Release resume a atividade de uma versão do app na janela consultada.
type Release struct {
	Release      string    `json:"release"`
	App          string    `json:"app"`
	FirstSeen    time.Time `json:"firstSeen"`
	LastSeen     time.Time `json:"lastSeen"`
	Events       int       `json:"events"`
	Errors       int       `json:"errors"`
	Sessions     int       `json:"sessions"`
	Users        int       `json:"users"`
	ErrorRate    float64   `json:"errorRate"`
	AvgDurationMs float64  `json:"avgDurationMs"`
}

// ReleaseFilter filtra a listagem de releases.
type ReleaseFilter struct {
	From     *time.Time
	To       *time.Time
	App      string
	AppScope []string
	Limit    int
}

// ReleaseComparison mostra o delta entre duas releases (Fase 2 — comparação
// por versão). Usado para detectar regressões após um deploy.
type ReleaseComparison struct {
	ReleaseA  Release      `json:"releaseA"`
	ReleaseB  Release      `json:"releaseB"`
	DeltaErrorRate  float64 `json:"deltaErrorRate"`  // B - A
	DeltaAvgMs      float64 `json:"deltaAvgMs"`      // B - A
	NewIssues       []Issue `json:"newIssues"`       // presentes em B mas não em A
}

// WebVitalFilter filtra as agregações de Web Vitals.
type WebVitalFilter struct {
	From     *time.Time
	To       *time.Time
	App      string
	AppScope []string
	Vital    string // LCP, CLS, INP, FID, TTFB, FCP (vazio = todos)
	Limit    int
}

// WebVitalStat agrega uma métrica Web Vital por rota (p75/p95).
type WebVitalStat struct {
	Vital  string  `json:"vital"`
	Screen string  `json:"screen"`
	Count  int     `json:"count"`
	P75    float64 `json:"p75"`
	P95    float64 `json:"p95"`
	Good   int     `json:"good"`
	Poor   int     `json:"poor"`
}

// IssueStore persiste o estado mutável das issues (status, assignee) e os
// comentários — o Postgres é o control plane; a contagem/first_seen/last_seen
// vêm do ClickHouse.
type IssueStore interface {
	GetIssueStates(ctx context.Context, fingerprints []string) (map[string]IssueState, error)
	UpsertIssueStatus(ctx context.Context, in IssueStatusInput) error
	ListIssueComments(ctx context.Context, fingerprint string) ([]IssueComment, error)
	CreateIssueComment(ctx context.Context, fingerprint, authorID, body string) (IssueComment, error)
	DeleteIssueComment(ctx context.Context, id, requesterID string) error
	ListFingerprintsByAssignee(ctx context.Context, userID string) ([]string, error)
}

// IssueState é o estado mutável de uma issue no control plane.
type IssueState struct {
	Status         IssueStatus
	Assignee       string // texto legado
	AssigneeUserID string
	AssigneeName   string
	AssigneeEmail  string
}

// IssueStatusInput é o payload do upsert de estado de uma issue.
type IssueStatusInput struct {
	Fingerprint    string
	App            string
	Status         IssueStatus
	Assignee       string // texto legado (mantido para compat)
	AssigneeUserID string // FK preferido — vazio = sem responsável
}
